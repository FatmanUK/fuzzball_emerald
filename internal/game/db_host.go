package game

import (
	"context"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// NewPlayer implements muf.Host for NEWPLAYER, upstream's
// create_player.
func (h *mufHost) NewPlayer(name, pass string) (ref.Ref, error) {
	if err := validPlayerName(h.w, name); err != nil {
		return ref.Nothing, err
	}
	if _, taken := h.w.PlayerNamed(name); taken {
		return ref.Nothing, errMsg("That name is already taken.")
	}
	o, err := h.s.createPlayer(h.w, name, pass)
	if err != nil {
		return ref.Nothing, err
	}
	h.s.securityLog().Info("player created by a program",
		"player", o.Ref.String(), "name", name,
		"by", h.caller.String(), "byName", nameOf(h.w, h.caller))
	return o.Ref, nil
}

// CopyPlayer implements muf.Host for COPYPLAYER: a new player
// carrying src's flags, properties, home and pennies.
//
// Two of upstream's results here look like mistakes and are
// reproduced anyway, because a program written against the real
// server sees them. copy_properties_onto *replaces* the destination's
// whole property tree rather than merging into it, so the new player
// loses the created_as and starting pennies create_player had just
// given them and inherits src's instead; the value arithmetic that
// follows then reads back the value it just copied, so the copy ends
// up with twice src's pennies rather than src's plus its own.
func (h *mufHost) CopyPlayer(src ref.Ref, name, pass string) (ref.Ref, error) {
	r, err := h.NewPlayer(name, pass)
	if err != nil {
		return ref.Nothing, err
	}
	from, to := h.w.Get(src), h.w.Get(r)
	if from == nil || to == nil {
		return ref.Nothing, errMsg("That player could not be copied.")
	}
	to.Flags = from.Flags
	to.Props = from.Props.Clone()
	to.Home = from.Home
	h.w.SetProp(r, propValue, props.Value{
		Type: props.Int, Num: 2 * valueOf(h.w, src),
	})
	h.w.Modified(r)
	if err := h.w.MoveTo(r, from.Home); err != nil {
		h.s.statusLog().Error("could not place a copied player",
			"player", r.String(), "error", err)
	}
	return r, nil
}

// ToadPlayer implements muf.Host for TOADPLAYER. Every check the
// primitive makes is its own; this is only the deletion, the same
// split cmdToad uses.
func (h *mufHost) ToadPlayer(victim, recipient ref.Ref) {
	c := &ctx{w: h.w, who: h.caller, out: func(string) {}}
	h.s.securityLog().Warn("toaded by a program",
		"player", victim.String(), "name", nameOf(h.w, victim),
		"by", h.caller.String(), "byName", nameOf(h.w, h.caller),
		"recipient", recipient.String())
	h.s.toadPlayer(c, victim, recipient)
}

// TuneRefersTo implements muf.Host for TOADPLAYER's own refusal to
// delete a player some @tune parameter points at, upstream's own loop
// over tune_list.
func (h *mufHost) TuneRefersTo(obj ref.Ref) bool {
	_, named := tuneRefersTo(h.w, obj)
	return named
}

// CopyObject implements muf.Host for COPYOBJ, upstream's clone_thing.
func (h *mufHost) CopyObject(src ref.Ref, copyHidden bool) (ref.Ref, error) {
	from := h.w.Get(src)
	if from == nil {
		return ref.Nothing, errMsg("Invalid object.")
	}
	if !h.NameOK(from.Name, ref.TypeThing) {
		return ref.Nothing, errMsg("You cannot use that name for a thing.")
	}
	// A clone belongs to, and starts inside, the player the
	// program is running for — upstream's create_thing(player,
	// name, player, ...).
	r, err := h.Create(ref.TypeThing, from.Name, h.caller, h.caller)
	if err != nil {
		return ref.Nothing, err
	}
	to := h.w.Get(r)
	to.Flags = from.Flags
	copyProps(from, to, copyHidden)
	v := valueOf(h.w, src)
	if max := h.w.Tune.Int("max_object_endowment"); v > max {
		v = max
	}
	if v < 0 {
		v = 0
	}
	h.w.SetProp(r, propValue, props.Value{Type: props.Int, Num: v})
	h.w.Modified(r)
	return r, nil
}

// copyProps is upstream's copy_proplist: every property from one
// object's tree onto another's.
//
// Upstream skips a hidden property — one with a path segment
// starting with '@' — when copyHidden is false, and, because its
// trees are AVL nodes it abandons the whole subtree at, silently
// drops that node's siblings too. Emerald's tree is not an AVL, so
// only the hidden property itself is skipped; reproducing which
// siblings upstream happens to lose would mean reproducing its
// rebalancing.
func copyProps(from, to *world.Object, copyHidden bool) {
	from.Props.Walk(func(e props.Entry) bool {
		if !copyHidden && isHiddenProp(e.Path) {
			return true
		}
		to.Props.Set(e.Path, e.Value)
		return true
	})
}

// isHiddenProp is upstream's Prop_Hidden: any path segment starting
// with '@'.
func isHiddenProp(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "@") {
			return true
		}
	}
	return false
}

// SetProgramLines implements muf.Host for PROGRAM_SETLINES.
func (h *mufHost) SetProgramLines(prog ref.Ref, lines []string) {
	h.w.SaveSource(prog, strings.Join(lines, "\n"))
	h.s.InvalidateProgram(prog)
	h.s.statusLog().Info("program saved by a program",
		"program", prog.String(), "name", nameOf(h.w, prog),
		"by", h.caller.String(), "byName", nameOf(h.w, h.caller))
}

// DumpNow implements muf.Host for DUMP: asks the persister to write
// what is pending, off the world goroutine, the same way @dump does.
func (h *mufHost) DumpNow() {
	engine := h.s.engine
	log := h.s.statusLog()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := engine.Flush(ctx); err != nil {
			log.Error("a DUMP flush failed", "error", err)
		}
	}()
}
