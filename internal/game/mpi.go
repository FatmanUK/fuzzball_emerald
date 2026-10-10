package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// mpiHost lets MPI reach the world. Like the MUF host, every method
// runs on the world goroutine.
type mpiHost struct {
	s *Server
	w *world.World
}

func (h *mpiHost) Name(obj mpi.Ref) string {
	return nameOf(h.w, ref.Ref(obj))
}

func (h *mpiHost) GetPropStr(obj mpi.Ref, path string) string {
	v, ok := h.w.GetProp(ref.Ref(obj), path)
	if !ok {
		return ""
	}
	return v.StringValue()
}

func (h *mpiHost) SetPropStr(obj mpi.Ref, path, val string) {
	h.w.SetProp(ref.Ref(obj), path, props.Value{Type: props.String, Str: val})
}

func (h *mpiHost) DelProp(obj mpi.Ref, path string) {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		o.Props.Delete(path)
		h.w.Modified(ref.Ref(obj))
	}
}

func (h *mpiHost) PropChildren(obj mpi.Ref, path string) []string {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return o.Props.Children(path)
	}
	return nil
}

func (h *mpiHost) BlessProp(obj mpi.Ref, path string, blessed bool) {
	h.w.SetBlessed(ref.Ref(obj), path, blessed)
}

func (h *mpiHost) Parent(obj mpi.Ref) mpi.Ref {
	return mpi.Ref(h.w.Parent(ref.Ref(obj)))
}

func (h *mpiHost) Location(obj mpi.Ref) mpi.Ref {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return mpi.Ref(o.Location)
	}
	return mpi.Ref(ref.Nothing)
}

func (h *mpiHost) Owner(obj mpi.Ref) mpi.Ref {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return mpi.Ref(o.Owner)
	}
	return mpi.Ref(ref.Nothing)
}

func (h *mpiHost) Contents(obj mpi.Ref) []mpi.Ref {
	got := h.w.Contents(ref.Ref(obj))
	out := make([]mpi.Ref, len(got))
	for i, r := range got {
		out[i] = mpi.Ref(r)
	}
	return out
}

func (h *mpiHost) Valid(obj mpi.Ref) bool {
	return h.w.Valid(ref.Ref(obj))
}

func (h *mpiHost) IsPlayer(obj mpi.Ref) bool {
	o := h.w.Get(ref.Ref(obj))
	return o != nil && o.Type() == ref.TypePlayer
}

func (h *mpiHost) Online(obj mpi.Ref) bool {
	return h.s.hub.Online(ref.Ref(obj))
}

// Match is `mesg_dbref_raw` (`msgparse.c:667`): four keyword names,
// then a five-stage search around the viewer, then **the same search
// again around the object carrying the message**.
//
// What stood here was `match_everything` plus an unconditional player
// search, which is a different function. So `{name:this}` answered
// "Match failed." -- `this` is the one name that is unique to MPI and
// it was not handled at all -- and a description on an object in
// another room could not name anything near itself, because the
// remote pass did not exist. `Matcher.Around` is `init_match_remote`
// and had been written with no caller.
func (h *mpiHost) Match(who, what mpi.Ref, name string) mpi.Ref {
	viewer, obj := ref.Ref(who), ref.Ref(what)
	switch {
	case ascii.EqualFold(name, "this"):
		return mpi.Ref(obj)
	case ascii.EqualFold(name, "me"):
		return mpi.Ref(viewer)
	case ascii.EqualFold(name, "here"):
		if o := h.w.Get(viewer); o != nil {
			return mpi.Ref(o.Location)
		}
		return mpi.Ref(ref.Nothing)
	case ascii.EqualFold(name, "home"):
		// Reproduced because upstream has it, and **dead**
		// because upstream's own closing `OkObj` check
		// rejects HOME: `{name:home}` is "Match failed."
		// there too.
		return mpi.Ref(ref.Home)
	}
	// Note what this list is not: no `match_me` or `match_here`,
	// which the keywords above have already answered, and
	// `match_absolute` without the wizard gate `match_everything`
	// puts on it.
	r := match.New(h.w, viewer, name).
		Absolute().Exits().Neighbor().Possession().
		Registered().Result()
	if r == ref.Nothing {
		r = match.New(h.w, viewer, name).Around(obj).
			Player().Exits().Neighbor().Possession().
			Registered().Result()
	}
	return mpi.Ref(r)
}

func (h *mpiHost) Notify(obj mpi.Ref, msg string) {
	h.s.send(h.w, ref.Ref(obj), msg)
}

func (h *mpiHost) Now() int64 { return h.w.Now().Unix() }

// evalMPI evaluates a property's text for a viewer.
//
// Only text that actually contains a call is parsed, so an ordinary
// description costs nothing and cannot be changed by a stray brace.
func (s *Server) evalMPI(w *world.World, descr int,
	viewer, what ref.Ref, text, how string, blessed bool,
	kind mpi.MesgType) string {

	return s.evalMPIAs(w, descr, viewer, what, text, how, "", "",
		blessed, kind)
}

// evalMPIWith is evalMPI carrying upstream's match_cmdname and
// match_args, which MPI reads back as {&cmd} and {&arg}. Every
// message property goes through it; only an exit's pass anything.
func (s *Server) evalMPIWith(w *world.World, descr int,
	viewer, what ref.Ref, text, how string, blessed bool,
	kind mpi.MesgType, ma mesgArgs) string {

	return s.evalMPIAs(w, descr, viewer, what, text, how,
		ma.cmd, ma.arg, blessed, kind)
}

// evalMPIAs is evalMPI with {&cmd} and {&arg} spelled out, which the
// propqueues need: propqueue sets match_cmdname to the queue's own
// name — "Depart", "Arrive", "#123" — and match_args to the empty
// string, so a description's "both empty" is not the only answer.
func (s *Server) evalMPIAs(w *world.World, descr int,
	viewer, what ref.Ref, text, how, cmd, arg string,
	blessed bool, kind mpi.MesgType) string {

	// There is deliberately no "does it contain a brace?"
	// short-circuit. One used to stand here, and it was invented:
	// `do_parse_mesg` always runs the scanner, which does three
	// things besides evaluating calls -- it consumes the
	// **backtick** that toggles literal mode (`MFUN_LITCHAR`,
	// `mpi.h:31`), turns `\r` into a carriage return and `\[`
	// into an escape, and passes any other `\x` through as `x`.
	// So a description with no brace in it still changes, and
	// every such description diverged.
	if !w.Tune.Bool("do_mpi_parsing") {
		return text
	}

	env := &mpi.Env{
		Who:     mpi.Ref(viewer),
		What:    mpi.Ref(what),
		Perms:   mpi.Ref(what),
		Blessed: blessed,
		Type:    kind,
		Descr:   descr,
		Host:    &mpiHost{s: s, w: w},
	}
	// do_parse_mesg_2 (msgparse.c:1919) allocates three variables
	// for every evaluation, and all three have to exist or
	// referring to one is an MPI *error* rather than an empty
	// string.
	//
	// {&how} is the caller context — "(@Desc)", "(@Succ)" —
	// the same whatcalled string exec_or_notify passes. MPI's
	// {muf} reads it back to build the COMMAND variable it hands
	// a program, so leaving it out was visible from inside MUF.
	//
	// {&cmd} and {&arg} are match_cmdname and match_args, and
	// both read **empty** here. That was measured against the
	// oracle rather than derived: a description or a @succ
	// reached by looking reports them empty on every command
	// tried, and the golden case pins it. If a world ever finds a
	// path where upstream's are not empty, this is the line to
	// revisit — Emerald has no ambient equivalent of those two
	// globals to fill them from.
	_ = env.BindVar("", "how", how)
	_ = env.BindVar("", "cmd", cmd)
	_ = env.BindVar("", "arg", arg)

	// Eval reports a failure to the viewer and yields empty text
	// rather than propagating, so a broken description cannot
	// break the look.
	return mpi.Eval(env, text)
}
