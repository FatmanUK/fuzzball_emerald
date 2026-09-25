package game

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The half of mpi.Host that describes the world rather than reading
// properties from it — everything the object, connection and time
// functions need.

func (h *mpiHost) NotifyExcept(room mpi.Ref, except []mpi.Ref, msg string) {
	skip := make([]ref.Ref, len(except))
	for i, r := range except {
		skip[i] = ref.Ref(r)
	}
	h.s.notifyRoom(h.w, ref.Ref(room), skip, "%s", msg)
}

// TypeName is upstream's own words for a type, which {type} returns
// verbatim and {contents} matches its filter against.
func (h *mpiHost) TypeName(obj mpi.Ref) string {
	o := h.w.Get(ref.Ref(obj))
	if o == nil {
		return "Bad"
	}
	switch o.Type() {
	case ref.TypeRoom:
		return "Room"
	case ref.TypeExit:
		return "Exit"
	case ref.TypeThing:
		return "Thing"
	case ref.TypePlayer:
		return "Player"
	case ref.TypeProgram:
		return "Program"
	}
	return "Bad"
}

func (h *mpiHost) FlagString(obj mpi.Ref) string {
	o := h.w.Get(ref.Ref(obj))
	if o == nil {
		return ""
	}
	return o.Flags.Unparse()
}

func (h *mpiHost) HasFlag(obj mpi.Ref, flag string) bool {
	o := h.w.Get(ref.Ref(obj))
	if o == nil {
		return false
	}
	return o.Flags.HasNamed(flag)
}

func (h *mpiHost) Exits(obj mpi.Ref) []mpi.Ref {
	return toMPIRefs(h.w.Exits(ref.Ref(obj)))
}

func (h *mpiHost) Links(obj mpi.Ref) []mpi.Ref {
	return toMPIRefs((&mufHost{s: h.s, w: h.w}).Links(ref.Ref(obj)))
}

func (h *mpiHost) Value(obj mpi.Ref) int {
	return int(valueOf(h.w, ref.Ref(obj)))
}

func (h *mpiHost) Timestamps(obj mpi.Ref) (created, modified, used int64, count int) {
	o := h.w.Get(ref.Ref(obj))
	if o == nil {
		return 0, 0, 0, 0
	}
	return o.Created.Unix(), o.Modified.Unix(), o.LastUsed.Unix(), int(o.UseCount)
}

func (h *mpiHost) Controls(who, target mpi.Ref) bool {
	return h.s.controls(h.w, ref.Ref(who), ref.Ref(target))
}

func (h *mpiHost) Locked(descr int, player, thing mpi.Ref) bool {
	return !couldDoit(h.s, h.w, descr, 1, ref.Ref(player), ref.Ref(thing))
}

// TestLock parses a lock written as text and evaluates it, which is
// what {testlock} does with a property's contents.
func (h *mpiHost) TestLock(descr int, player, thing mpi.Ref, lock string) (bool, error) {
	lh := &lockHost{s: h.s, w: h.w, level: 1}
	expr, err := boolexp.Parse(lh, descr, ref.Ref(thing), lock, false)
	if err != nil {
		return false, err
	}
	return boolexp.Eval(lh, descr, ref.Ref(player), expr, ref.Ref(thing)), nil
}

func (h *mpiHost) OnlinePlayers() []mpi.Ref {
	return toMPIRefs((&mufHost{s: h.s, w: h.w}).Online())
}

// Idle and OnTime report on a player's least idle connection, which
// is what upstream's own least_idle_player_descr picks.
func (h *mpiHost) Idle(obj mpi.Ref) int {
	mh := &mufHost{s: h.s, w: h.w}
	d := mh.DescrLeastIdle(ref.Ref(obj))
	if d == -1 {
		return -1
	}
	return mh.DescrIdle(d)
}

func (h *mpiHost) OnTime(obj mpi.Ref) int {
	mh := &mufHost{s: h.s, w: h.w}
	d := mh.DescrLeastIdle(ref.Ref(obj))
	if d == -1 {
		return -1
	}
	return mh.DescrOnTime(d)
}

func (h *mpiHost) Width(obj mpi.Ref) int {
	w, _ := h.terminalSize(obj)
	return w
}
func (h *mpiHost) Height(obj mpi.Ref) int {
	_, ht := h.terminalSize(obj)
	return ht
}

func (h *mpiHost) terminalSize(obj mpi.Ref) (width, height int) {
	mh := &mufHost{s: h.s, w: h.w}
	d := mh.DescrLeastIdle(ref.Ref(obj))
	if d == -1 {
		return 0, 0
	}
	return mh.DescrSize(d)
}

func (h *mpiHost) TuneGet(name string) (string, bool) {
	return (&mufHost{s: h.s, w: h.w}).TuneGet(name)
}

func (h *mpiHost) MuckName() string {
	return h.w.Tune.String("muckname")
}

func (h *mpiHost) PronounSub(obj mpi.Ref, text string) string {
	return muf.PronounSub(&mufHost{s: h.s, w: h.w, caller: ref.Ref(obj)},
		ref.Ref(obj), text)
}

func (h *mpiHost) Force(descr int, who mpi.Ref, command string) {
	(&mufHost{s: h.s, w: h.w, caller: ref.Ref(who)}).Force(
		descr, ref.Ref(who), ref.Nothing, ref.Ref(who), command)
}

func (h *mpiHost) Kill(pid int) bool {
	return (&mufHost{s: h.s, w: h.w}).KillPID(pid)
}

// RunMUF is {muf}: run a program to completion and take back what it
// left on its stack, rendered as text. It is INTERP's own
// nested-frame path, since the two do the same thing from different
// languages.
func (h *mpiHost) RunMUF(descr int, player, prog, perms mpi.Ref,
	how, arg string) (string, error) {

	mh := &mufHost{s: h.s, w: h.w, caller: ref.Ref(player)}
	// The trigger is the evaluation's *permissions* object rather
	// than the player: mfuns2.c:2680 passes perms as interp's
	// source. And COMMAND is the calling context with "(MPI)"
	// after it, which is how a program can tell it was reached
	// from a property rather than typed.
	v, ok := mh.Interp(descr, 1, ref.Ref(prog), ref.Ref(perms),
		how+"(MPI)", arg)
	if !ok {
		return "", nil
	}
	if v.Type == muf.TypeObject {
		return v.Ref.String(), nil
	}
	return v.String(), nil
}

// Delay is {delay}: evaluate a message later rather than now.
func (h *mpiHost) Delay(descr int, player, what, perms mpi.Ref, seconds int, text string, blessed bool) {
	h.s.mpiEvents = append(h.s.mpiEvents, mpiEvent{
		fires:   h.w.Now().Add(time.Duration(seconds) * time.Second),
		descr:   descr,
		player:  ref.Ref(player),
		what:    ref.Ref(what),
		perms:   ref.Ref(perms),
		text:    text,
		blessed: blessed,
	})
}

// mpiEvent is one pending {delay}.
type mpiEvent struct {
	fires   time.Time
	descr   int
	player  ref.Ref
	what    ref.Ref
	perms   ref.Ref
	text    string
	blessed bool
}

// fireMPIEvents evaluates whatever {delay} has scheduled and is now
// due. The result is discarded: a delayed message acts through {tell}
// and {otell} rather than by producing text, since there is nothing
// left to return it to.
func (s *Server) fireMPIEvents(w *world.World, now time.Time) {
	if len(s.mpiEvents) == 0 {
		return
	}
	var kept []mpiEvent
	var due []mpiEvent
	for _, e := range s.mpiEvents {
		if e.fires.After(now) {
			kept = append(kept, e)
			continue
		}
		due = append(due, e)
	}
	s.mpiEvents = kept
	for _, e := range due {
		env := &mpi.Env{
			Who:     mpi.Ref(e.player),
			What:    mpi.Ref(e.what),
			Perms:   mpi.Ref(e.perms),
			Blessed: e.blessed,
			Descr:   e.descr,
			// a delayed message keeps the kind it was
			// queued with; nothing queues a public one
			// yet.
			Type: mpi.Private,
			Host: &mpiHost{s: s, w: w},
		}
		mpi.Eval(env, e.text)
	}
}

func toMPIRefs(rs []ref.Ref) []mpi.Ref {
	out := make([]mpi.Ref, len(rs))
	for i, r := range rs {
		out[i] = mpi.Ref(r)
	}
	return out
}
