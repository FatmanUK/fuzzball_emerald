package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/timefmt"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `dump_users` (`interface.c:744`), which Emerald had as a table of
// its own invention: one format for everybody, its own column widths,
// its own header, no wizard mode, and `who_hides_dark` unread — so
// **DARK wizards were listed**, which the parameter defaults to
// preventing.
//
// It is two tables rather than one, chosen by a leading `*` on the
// typed argument, and the wizard one has two variants of its own.

// whoWizardHeader and whoHeader are upstream's two, spaced as the C
// spaces them.
const (
	whoWizardHeader = "Player Name                Location  " +
		"   On For Idle   Host"
	whoHeader = "Player Name           On For Idle   Doing..."
)

// cmdWho lists who is online.
func (s *Server) cmdWho(c *ctx) {
	s.writeWho(c.w, c.d, c.arg)
}

// writeWho renders the table.
//
// The argument is scanned for leading spaces and `*`s, and a `*` asks
// for the wizard table — which is granted only to a connected,
// unquelled wizard, and quietly ignored otherwise. Whatever is left
// after the scan is a name prefix, and an empty remainder is no
// filter at all.
func (s *Server) writeWho(w *world.World, d *session.Descriptor,
	arg string) {

	user, wizard := whoArg(w, d, arg)
	if wizard {
		d.Send(whoWizardHeader)
	} else {
		d.Send(whoHeader)
	}

	now := w.Now().Unix()
	limit := int(w.Tune.Int("player_name_limit"))
	hide := w.Tune.Bool("who_hides_dark")
	god := d.Connected && d.Player == ref.God

	players := 0
	for _, other := range s.hub.Connected() {
		o := w.Get(other.Player)
		if o == nil {
			continue
		}
		// who_hides_dark keeps a DARK player out of a
		// mortal's listing -- and out of the **count**,
		// because `++players` sits between this test and the
		// name filter. A name that is filtered out is still
		// counted.
		if hide && !wizard && o.Flags&ref.Dark != 0 {
			continue
		}
		players++
		if user != "" && !ascii.HasPrefix(o.Name, user) {
			continue
		}
		if wizard {
			d.Send(whoWizardRow(w, other, o, now, limit,
				god))
			continue
		}
		d.Send(whoRow(w, other, o, now, limit))
	}

	// Upstream raises its high-water mark here too, and its own
	// comment calls this "an odd place to update this variable".
	w.RecordMaxConnects(players)
	max := 0
	if v, ok := w.GetProp(ref.GlobalEnvironment,
		world.SysMaxConnects); ok {

		max = int(v.Num)
	}
	are := "are"
	if players == 1 {
		are = "is"
	}
	d.Send(sprintf("%d player%s %s connected.  (Max was %d)",
		players, plural(players), are, max))
}

// whoArg is the leading-`*` scan. Upstream also *logs* a wizard WHO,
// which is why the scan is where it is rather than folded into the
// caller.
func whoArg(w *world.World, d *session.Descriptor,
	arg string) (string, bool) {

	wizard := false
	i := 0
	for ; i < len(arg); i++ {
		if arg[i] == '*' {
			if d.Connected && isWizard(w, d.Player) {
				wizard = true
			}
			continue
		}
		if !isSpaceByte(arg[i]) {
			break
		}
	}
	return arg[i:], wizard
}

// whoRow is the mortal line. The name column is cut to
// player_name_limit+1, and the Doing column to whatever is left of 79
// once the name and the fixed columns have had theirs.
func whoRow(w *world.World, d *session.Descriptor, o *world.Object,
	now int64, limit int) string {

	return sprintf("%-*s %10s %4s%c%c %.*s",
		limit+1, o.Name,
		timefmt.Format1(now-d.ConnectedAt.Unix()),
		timefmt.Format2(now-d.LastActive.Unix()),
		interactiveMark(o), secureMark(),
		79-(limit+20), getMesg(w, d.Player, propDoing))
}

// whoWizardRow is the wizard line, which has a God variant: only God
// is shown the connection's username beside its host, because
// `GOD_PRIV` is defined in upstream's default build and the test is
// `!God(e->player)`.
func whoWizardRow(w *world.World, d *session.Descriptor,
	o *world.Object, now int64, limit int, god bool) string {

	name := sprintf("%.*s(%s)", limit+1, o.Name,
		o.Ref.String())
	host := d.Hostname
	if god {
		host = sprintf("%s(%s)", d.Hostname, d.Port)
	}
	return sprintf("%-*s [%6d] %10s %4s%c%c %s",
		limit+10, name, int32(o.Location),
		timefmt.Format1(now-d.ConnectedAt.Unix()),
		timefmt.Format2(now-d.LastActive.Unix()),
		interactiveMark(o), secureMark(), host)
}

// interactiveMark is the `*` beside somebody in the editor or
// answering a READ.
func interactiveMark(o *world.Object) byte {
	if o.Flags&ref.Interactive != 0 {
		return '*'
	}
	return ' '
}

// secureMark is upstream's `@` for an SSL, local or forwarded
// connection. Every connection here is TLS, so it is always set —
// the same answer `DESCRSECURE?` gives for the same reason.
func secureMark() byte { return '@' }

// plural is the "s" of "N players".
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
