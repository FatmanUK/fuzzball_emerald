package game

import (
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// internal/game/propqueue.go is timequeue.c's propqueue (:1912),
// envpropqueue (:2068) and listenqueue (:2240): the mechanism by
// which a *property* is a hook. A world writes `_arrive`, `_depart`,
// `_connect`, `_disconnect`, `_lookq`, `_listen` or one of their "o"
// halves, and the server runs whatever it names when the
// corresponding thing happens.
//
// This is the only work in the whole port that changes how an
// existing world behaves rather than what it says: properties that
// have sat inert in every imported `.db` start executing the moment
// the call sites are wired. The machinery lands first and the call
// sites follow one at a time, so a divergence can be attributed.
//
// **A property names a program four ways, and one of them is not a
// program.** A leading `&` means the rest is MPI; `#123` or a bare
// number is a dbref; `$name` is a registration looked up on the
// object carrying the property, not on the player. Anything else
// names nothing and the queue does nothing — silently, which is why
// a typo in a `_arrive` is so hard to notice.
//
// **The recursion counter is shared across every queue type.** One
// `propq_level`, capped at eight, so a `_depart` that triggers an
// `_arrive` that triggers a `_depart` runs out of depth rather than
// looping. The counter is checked *after* the program is resolved, so
// the "Propqueue stopped" message appears for a property that would
// have run.
//
// **`mt` is private, not public.** Upstream's parameter is documented
// as "this is a public message" and the code does the opposite: with
// it set, MPI runs as `MPI_ISPRIVATE` and the output goes to the
// triggering player alone; with it clear, MPI runs as `MPI_ISPUBLIC`
// and the output is broadcast to the room prefixed `>> `. Every
// non-"o" queue passes it set and every "o" queue clear, which is
// what makes `_arrive` yours and `_oarrive` everyone else's.
//
// **A propdir is walked.** `_arrive` with children runs each child as
// well, so a world can file several hooks under one name.

// propqLimit is upstream's cap on propq_level (timequeue.c:1969). It
// is eight, and it is cumulative across propqueue, envpropqueue and
// listenqueue together.
const propqLimit = 8

// propqMPI marks a property value as MPI rather than a program, and
// is stripped before evaluation.
const propqMPI = '&'

// propqRun carries one propqueue's parameters. Upstream passes ten
// positional arguments through three mutually recursive functions; a
// struct keeps the call sites readable and the recursion honest.
type propqRun struct {
	descr int
	// player is whoever triggered it, and is who output goes to
	// when private is set.
	player ref.Ref
	// where is the room it happened in, which is the room a
	// public message is broadcast to and the program's "loc".
	where ref.Ref
	// trigger is what caused it — an exit for a move, NOTHING
	// for a connect — and becomes the program's TRIGGER.
	trigger ref.Ref
	// what is the object the properties are read from, which
	// envpropqueue walks upwards from.
	what ref.Ref
	// exclude is a program not to run, which stops a listener
	// hearing its own output. Upstream's xclude.
	exclude ref.Ref
	// arg is upstream's toparg: the program's stack argument, and
	// MPI's {&cmd}.
	arg string
	// mlev is the floor a program and its owner must both meet.
	mlev int
	// private is upstream's mt, whose documented meaning is the
	// reverse of what it does. See the file comment.
	private bool
}

// propqueue runs the program or MPI a property names, then every
// property beneath it.
func (s *Server) propqueue(w *world.World, r propqRun,
	propname string) {

	s.propqOne(w, r, propname)

	// A propdir runs its children too, each by its full path, so
	// a world can file several hooks under one name. Upstream
	// rebuilds the path from next_prop_name, which returns it
	// whole.
	o := w.Get(r.what)
	if o == nil || !o.Props.IsDir(propname) {
		return
	}
	for _, child := range o.Props.Children(propname) {
		s.propqueue(w, r, propname+"/"+child)
	}
}

// propqOne is propqueue's own body, without the propdir walk.
func (s *Server) propqOne(w *world.World, r propqRun,
	propname string) {

	o := w.Get(r.what)
	if o == nil {
		return
	}
	v, ok := o.Props.Get(propname)
	if !ok {
		return
	}

	text, prog, isMPI := propqTarget(w, r.what, v)
	if !isMPI && prog == ref.Nothing {
		return
	}
	if !isMPI && !s.propqRunnable(w, r, prog) {
		return
	}

	if s.propqLevel >= propqLimit {
		// notify_nolisten, so this is the one message in the
		// mechanism that is not itself filtered.
		s.send(w, r.player,
			"Propqueue stopped to prevent infinite loop.")
		return
	}
	s.propqLevel++
	defer func() { s.propqLevel-- }()

	if isMPI {
		s.propqMPI(w, r, text, v.Blessed)
		return
	}
	s.propqProgram(w, r, prog)
}

// propqTarget reads a property value the way propqueue does: as MPI,
// as a dbref, as a registration, or as nothing at all.
//
// A Ref-typed property is taken directly — that is
// get_property_dbref, which upstream tries first — and a string is
// parsed. The AMBIGUOUS sentinel upstream uses to mean "this is MPI"
// is a bool here, because a dbref property holding AMBIGUOUS is
// upstream's one case of the sentinel arriving by accident, and it
// resolves to nothing.
func propqTarget(w *world.World, what ref.Ref,
	v props.Value) (text string, prog ref.Ref, isMPI bool) {

	if v.Type == props.Ref {
		if v.Ref == ref.Ambiguous {
			return "", ref.Nothing, false
		}
		return "", v.Ref, false
	}
	if v.Type != props.String || v.Str == "" {
		return "", ref.Nothing, false
	}
	s := v.Str
	switch {
	case s[0] == propqMPI:
		return s[1:], ref.Nothing, true
	case s[0] == '#' && isAllDigits(s[1:]):
		return "", ref.Ref(leadingInt(s[1:])), false
	case s[0] == '$':
		// find_registered_obj searches _reg/ on the object
		// carrying the property and then outwards through its
		// environment — not the triggering player's.
		return "", match.New(w, what, s).Registered().Result(),
			false
	case isAllDigits(s):
		return "", ref.Ref(leadingInt(s)), false
	}
	return "", ref.Nothing, false
}

// isAllDigits is fbstrings.c's number() without the sign, which is
// what propqueue uses to tell a dbref from a sentence.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// propqRunnable applies the six tests upstream makes before running a
// program a property named.
//
// The ownership test is the interesting one: a program only runs if
// the triggering player's owner owns it **or** it is LINK_OK, so a
// world cannot make somebody else's `_arrive` run somebody else's
// private code. The two mucker floors are separate — the program's
// own level and its owner's — because find_mlev takes the lower and
// either being too low disqualifies it.
func (s *Server) propqRunnable(w *world.World, r propqRun,
	prog ref.Ref) bool {

	o := w.Get(prog)
	if o == nil || o.Type() != ref.TypeProgram {
		return false
	}
	if o.Owner != ownerOf(w, r.player) &&
		o.Flags&ref.LinkOK == 0 {
		return false
	}
	if o.Flags.MLevel() < r.mlev {
		return false
	}
	if owner := w.Get(o.Owner); owner == nil ||
		owner.Flags.MLevel() < r.mlev {
		return false
	}
	return prog != r.exclude
}

// propqMPI evaluates a property whose value began with '&' and
// delivers whatever it produced.
//
// A private queue answers the triggering player; a public one is
// pronoun-substituted, prefixed ">> " and broadcast to every *player*
// in the room except the one who triggered it — not to things, and
// not through notifyRoom, because upstream walks CONTENTS itself and
// tests Typeof.
func (s *Server) propqMPI(w *world.World, r propqRun,
	text string, blessed bool) {

	kind := mpi.Private
	if !r.private {
		kind = 0
	}
	out := s.evalMPIAs(w, r.descr, r.player, r.what, text,
		"(MPIqueue)", r.arg, "", blessed, kind)
	if out == "" {
		return
	}

	if r.private {
		s.notifyFiltered(w, r.player, r.player, out)
		return
	}
	line := ">> " + muf.PronounSub(
		&mufHost{s: s, w: w, caller: r.player}, r.player, out)
	for _, p := range w.Contents(r.where) {
		o := w.Get(p)
		if o == nil || o.Type() != ref.TypePlayer ||
			p == r.player {
			continue
		}
		s.notifyFiltered(w, r.player, p, line)
	}
}

// notifyFiltered is interface.c:4778's notify_filtered: send unless
// the recipient is ignoring the sender.
func (s *Server) notifyFiltered(w *world.World, from, to ref.Ref,
	text string) {

	h := &mufHost{s: s, w: w, caller: from}
	if h.IsIgnoring(to, from) {
		return
	}
	s.send(w, to, text)
}

// propqProgram runs the program a property named.
//
// It runs **now** rather than through the timequeue — upstream
// calls interp and then interp_loop directly, so an `_arrive`
// finishes before the next line of the move prints — in BACKGROUND
// mode and HARDUID, so it acts as its own owner whatever triggered
// it.
//
// COMMAND is the fixed string "Queued event." and the pushed argument
// is the queue's own name, which is the pair match_cmdname and
// match_args again. Note the lower-case "event": the timequeue's own
// version says "Queued Event." and this one does not.
func (s *Server) propqProgram(w *world.World, r propqRun,
	prog ref.Ref) {

	p, err := s.compileProgram(w, prog)
	if err != nil {
		s.mufLog().Warn("propqueue program does not compile",
			"program", prog.String(), "error", err.Error())
		return
	}

	host := &mufHost{s: s, w: w, caller: r.player}
	f := muf.NewFrame(p, host)
	f.Perms = muf.HardUID
	f.SetReserved(r.player, r.where, r.trigger, "Queued event.")
	f.Stack[len(f.Stack)-1] = muf.Str(r.arg)
	f.Descr = r.descr
	f.Mode = muf.ModeBackground

	w.Used(prog)

	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			s.reportMUFErrorTo(w, r.player, f, prog, err)
			return
		}
		if res == muf.Done || res == muf.Blocked {
			return
		}
	}
}

// envpropqueue runs a propqueue on an object and then on everything
// above it, which is what makes a hook on #0 fire for the whole
// world.
//
// It does **not** stop at the first hit: every level that has the
// property runs it. The walk is getparent's, so a VEHICLE thing
// continues from its home rather than its location.
func (s *Server) envpropqueue(w *world.World, r propqRun,
	propname string) {

	for what := r.what; what != ref.Nothing; what = getParent(w, what) {
		r.what = what
		s.propqueue(w, r, propname)
	}
}

// listenqueue is timequeue.c:2240, and is propqueue's near-twin with
// four differences that all matter.
//
// **It is gated on the LISTENER flag**, or on the object's owner
// being flagged ZOMBIE — the second being how a puppet's owner
// hears through it. Nothing else in the mechanism checks a flag.
//
// **A value may be conditional.** `Message=&MPI` runs the right-hand
// side only when the left matches the text being listened to, by
// `equalstr` — so a listener can wait for one phrase. The `=` may
// be escaped with a backslash. No other propqueue has this.
//
// **It queues rather than runs.** Upstream calls add_mpi_event and
// add_muf_queue_event, so a listener fires after the line that
// triggered it has been delivered — which is what stops a `_listen`
// that speaks from recursing through its own output. Emerald files it
// as a sleeping process due immediately, which the tick picks up.
//
// **MPI is opt-in.** `_listen` is called with it off and the two
// wizard-only propqueues with it on, so a mortal cannot make a
// listener evaluate MPI at all.
func (s *Server) listenqueue(w *world.World, r propqRun,
	propname string, allowMPI bool) {

	o := w.Get(r.what)
	if o == nil {
		return
	}
	if o.Flags&ref.Listener == 0 &&
		!hasFlag(w, ownerOf(w, r.what), ref.Zombie) {
		return
	}

	s.listenOne(w, r, propname, allowMPI)

	if !o.Props.IsDir(propname) {
		return
	}
	for _, child := range o.Props.Children(propname) {
		s.listenqueue(w, r, propname+"/"+child, allowMPI)
	}
}

// listenOne is listenqueue's body without the propdir walk.
func (s *Server) listenOne(w *world.World, r propqRun,
	propname string, allowMPI bool) {

	o := w.Get(r.what)
	v, ok := o.Props.Get(propname)
	if !ok {
		return
	}

	// The conditional form, which only listen props have.
	if v.Type == props.String {
		pattern, rest, conditional := cutUnescaped(v.Str, '=')
		if conditional {
			if !ascii.SMatch(r.arg, pattern) {
				return
			}
			v.Str = rest
		}
	}

	text, prog, isMPI := propqTarget(w, r.what, v)
	if !isMPI && prog == ref.Nothing {
		return
	}
	if !isMPI && !s.propqRunnable(w, r, prog) {
		return
	}

	if isMPI {
		if !allowMPI {
			return
		}
		s.queueListenMPI(w, r, text, v.Blessed)
		return
	}
	s.queueListenProgram(w, r, prog)
}

// cutUnescaped splits at the first unescaped occurrence of sep, which
// is how listenqueue finds the `=` in "Message=action" without
// tripping over a literal one.
func cutUnescaped(s string, sep byte) (before, after string,
	found bool) {

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// queueListenMPI files a listener's MPI for the next tick, which is
// add_mpi_event (timequeue.c:900).
//
// The caller context is "Listen" for a private queue and "Olisten"
// for a public one, which is what an MPI {&how} reads back — and is
// the one place the two halves are told apart by name rather than by
// what they do with the result.
func (s *Server) queueListenMPI(w *world.World, r propqRun,
	text string, blessed bool) {

	how := "Olisten"
	if r.private {
		how = "Listen"
	}
	// A listen MPI event is queued with a delay of one second,
	// where a listen MUF event is queued with none — so a
	// listener that answers in MPI answers a beat later than one
	// that answers in MUF. That is upstream's add_mpi_event(1,
	// ...) against add_muf_queue_event's zero, and it is
	// reproduced because both are observable in a transcript.
	s.deferTo(w, time.Second, func(w *world.World) {
		s.propqMPIAs(w, r, text, how, blessed)
	})
}

// propqMPIAs is propqMPI with the caller context spelled out, which
// the listen queues need and the others do not.
func (s *Server) propqMPIAs(w *world.World, r propqRun,
	text, how string, blessed bool) {

	kind := mpi.Private | mpi.Listener
	if !r.private {
		kind = mpi.Listener
	}
	out := s.evalMPIAs(w, r.descr, r.player, r.what, text, how,
		r.arg, "", blessed, kind)
	if out == "" {
		return
	}
	if r.private {
		s.notifyFiltered(w, r.player, r.player, out)
		return
	}
	line := ">> " + muf.PronounSub(
		&mufHost{s: s, w: w, caller: r.player}, r.player, out)
	for _, p := range w.Contents(r.where) {
		o := w.Get(p)
		if o == nil || o.Type() != ref.TypePlayer ||
			p == r.player {
			continue
		}
		s.notifyFiltered(w, r.player, p, line)
	}
}

// queueListenProgram files a listener's program for the next tick,
// which is add_muf_queue_event (timequeue.c:1050). COMMAND is
// "(_Listen)" here rather than "Queued event.", which is how a
// program can tell what woke it.
func (s *Server) queueListenProgram(w *world.World, r propqRun,
	prog ref.Ref) {

	s.deferTo(w, 0, func(w *world.World) {
		s.propqProgramAs(w, r, prog, "(_Listen)")
	})
}

// propqProgramAs is propqProgram with COMMAND spelled out.
func (s *Server) propqProgramAs(w *world.World, r propqRun,
	prog ref.Ref, command string) {

	p, err := s.compileProgram(w, prog)
	if err != nil {
		s.mufLog().Warn("listener program does not compile",
			"program", prog.String(), "error", err.Error())
		return
	}
	host := &mufHost{s: s, w: w, caller: r.player}
	f := muf.NewFrame(p, host)
	f.Perms = muf.HardUID
	f.SetReserved(r.player, r.where, r.trigger, command)
	f.Stack[len(f.Stack)-1] = muf.Str(r.arg)
	f.Descr = r.descr
	f.Mode = muf.ModeBackground

	w.Used(prog)
	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			s.reportMUFErrorTo(w, r.player, f, prog, err)
			return
		}
		if res == muf.Done || res == muf.Blocked {
			return
		}
	}
}

// notifyListeners is interface.c:4884's notify_listeners, minus the
// delivery it shares with send: the three listen propqueues on one
// object, gated by the three allow_listeners parameters.
//
// allow_listeners turns the whole mechanism off; allow_listeners_obj
// decides whether anything but a room may listen. The third,
// allow_listeners_env, belongs to the caller: it decides whether the
// environment chain is walked at all, which is why this takes one
// object and the caller loops.
func (s *Server) notifyListeners(w *world.World, from, prog,
	obj, room ref.Ref, text string) {

	if !w.Tune.Bool("allow_listeners") {
		return
	}
	o := w.Get(obj)
	if o == nil {
		return
	}
	if !w.Tune.Bool("allow_listeners_obj") &&
		o.Type() != ref.TypeRoom {
		return
	}

	mlev := int(w.Tune.Int("listen_mlev"))
	r := propqRun{
		descr:   -1,
		player:  from,
		where:   room,
		trigger: obj,
		what:    obj,
		exclude: prog,
		arg:     text,
		mlev:    mlev,
		private: true,
	}
	s.listenqueue(w, r, world.ListenProp, false)
	s.listenqueue(w, r, world.WListenProp, true)
	r.private = false
	s.listenqueue(w, r, world.WOListenProp, true)
}

// propqArg builds the "#123" a LOOK propqueue passes as its argument.
func propqArg(r ref.Ref) string {
	return "#" + strings.TrimPrefix(r.String(), "#")
}

// --- the deferred half of the timequeue
// ---------------------------------------

// deferredEvent is one entry in the small timequeue the listen
// propqueues file into. Upstream's is one ordered list carrying MUF
// events, MPI events and delayed commands together; Emerald already
// had the process table for suspended programs and mpiEvents for
// {delay}, so this holds only what neither of those fits: a
// listener's program or MPI, which has to run *after* the line that
// woke it.
type deferredEvent struct {
	fires time.Time
	run   func(*world.World)
}

// deferTo files work for a later tick. A delay of zero means the next
// one, which is add_muf_queue_event's dtime; a listen MPI event uses
// a second, which is add_mpi_event's.
func (s *Server) deferTo(w *world.World, delay time.Duration,
	fn func(*world.World)) {

	s.deferred = append(s.deferred, deferredEvent{
		fires: w.Now().Add(delay),
		run:   fn,
	})
}

// fireDeferred runs whatever the listen propqueues have filed and is
// now due, in the order it was filed — upstream's timequeue is
// ordered by due time and FIFO within one, and a listener firing out
// of order is visible in a transcript.
func (s *Server) fireDeferred(w *world.World, now time.Time) {
	if len(s.deferred) == 0 {
		return
	}
	var kept, due []deferredEvent
	for _, e := range s.deferred {
		if e.fires.After(now) {
			kept = append(kept, e)
			continue
		}
		due = append(due, e)
	}
	s.deferred = kept
	for _, e := range due {
		e.run(w)
	}
}

// The propqueue property names, from include/game.h:119.
//
// The two beginning '~' are wizard-only props, which is what the
// character means to the property system; a mortal cannot set them,
// so a world can listen without the object advertising it.
const (
	propArrive      = "_arrive"
	propOArrive     = "_oarrive"
	propDepart      = "_depart"
	propODepart     = "_odepart"
	propConnect     = "_connect"
	propOConnect    = "_oconnect"
	propDisconnect  = "_disconnect"
	propODisconnect = "_odisconnect"
	propLookQueue   = "_lookq"
)
