package game

import (
	"math/rand"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// moveto and the two loop checks around it, from move.c:275 and
// predicates.c:342 and :408.
//
// The difference from World.MoveTo is what happens when a move would
// put something inside itself. The store refuses and returns an
// error; upstream *redirects*, down a per-type ladder that always
// ends somewhere valid — a player to their home, a thing to its
// home and then its owner's home and then player_start, a room to #0,
// a program to its owner. Nothing in upstream's movement path can
// fail, which is why none of its callers check.

// maxParentDepth is MAX_PARENT_DEPTH (config.h:118).
const maxParentDepth = 256

// getParentLogic is db.c's getparent_logic: the environment parent,
// which for a VEHICLE thing is its home rather than its location —
// and a step further when that home is a player, since a vehicle
// parented to somebody means parented to where they live.
func getParentLogic(w *world.World, obj ref.Ref) ref.Ref {
	o := w.Get(obj)
	if obj == ref.Nothing || o == nil {
		return ref.Nothing
	}
	if o.Type() == ref.TypeThing && o.Flags&ref.Vehicle != 0 {
		home := o.Home
		if h := w.Get(home); h != nil &&
			h.Type() == ref.TypePlayer {
			return h.Home
		}
		return home
	}
	return o.Location
}

// getParent is db.c's getparent.
//
// With secure_thing_movement set it is simply the location. Without
// it, upstream walks the parent chain with a tortoise and a hare and
// collapses a detected cycle to the global environment, because a
// vehicle inside a vehicle inside itself would otherwise loop for
// ever. Reproduced rather than simplified: which answer it gives
// decides whether a move is refused.
func getParent(w *world.World, obj ref.Ref) ref.Ref {
	if w.Tune.Bool("secure_thing_movement") {
		if o := w.Get(obj); o != nil {
			return o.Location
		}
		return ref.Nothing
	}

	ptr := getParentLogic(w, obj)
	var oldptr ref.Ref
	for {
		obj = getParentLogic(w, obj)
		oldptr = getParentLogic(w, ptr)
		ptr = oldptr
		if obj == oldptr {
			break
		}
		ptr = getParentLogic(w, ptr)
		if obj == ptr {
			break
		}
		if obj == ref.Nothing {
			break
		}
		if o := w.Get(obj); o == nil ||
			o.Type() != ref.TypeThing {
			break
		}
	}
	if obj != ref.Nothing && (obj == oldptr || obj == ptr) {
		return ref.GlobalEnvironment
	}
	return obj
}

// locationLoopCheck is predicates.c:342: whether dest is inside
// source, walking locations.
func locationLoopCheck(w *world.World, source, dest ref.Ref) bool {
	if source == dest {
		return true
	}
	seen := []ref.Ref{source, dest}
	for level := 0; level < maxParentDepth; level++ {
		o := w.Get(dest)
		if o == nil {
			return false
		}
		dest = o.Location
		switch dest {
		case ref.Nothing:
			return false
		case ref.Home:
			return true
		case ref.GlobalEnvironment:
			// The top of the chain, reached without
			// meeting source.
			return false
		}
		for _, r := range seen {
			if r == dest {
				return true
			}
		}
		seen = append(seen, dest)
	}
	return true
}

// parentLoopCheck is predicates.c:408: whether moving source into
// dest would make something contain itself, by location *or* by
// environment parentage.
//
// HOME is resolved first, and a type with no home at all — garbage
// — counts as a loop, which is how upstream refuses to move it.
func parentLoopCheck(w *world.World, source, dest ref.Ref) bool {
	if dest == ref.Home {
		o := w.Get(source)
		if o == nil {
			return true
		}
		switch o.Type() {
		case ref.TypePlayer, ref.TypeThing:
			dest = o.Home
		case ref.TypeRoom:
			dest = ref.GlobalEnvironment
		case ref.TypeProgram:
			dest = o.Owner
		default:
			return true
		}
	}
	if locationLoopCheck(w, source, dest) || source == dest {
		return true
	}

	seen := []ref.Ref{source, dest}
	for level := 0; level < maxParentDepth; level++ {
		dest = getParent(w, dest)
		switch dest {
		case ref.Nothing:
			return false
		case ref.Home:
			return true
		case ref.GlobalEnvironment:
			return false
		}
		for _, r := range seen {
			if r == dest {
				return true
			}
		}
		seen = append(seen, dest)
	}
	return true
}

// fallbackHome is the ladder moveto takes when a destination would
// make a loop. Every rung is a place the object can always go, which
// is what lets upstream's movement never fail.
func fallbackHome(w *world.World, what ref.Ref) ref.Ref {
	o := w.Get(what)
	if o == nil {
		return ref.Nothing
	}
	switch o.Type() {
	case ref.TypePlayer:
		return o.Home
	case ref.TypeThing:
		where := o.Home
		if parentLoopCheck(w, what, where) {
			owner := w.Get(o.Owner)
			where = ref.Nothing
			if owner != nil {
				where = owner.Home
			}
			if parentLoopCheck(w, what, where) {
				where = w.Tune.Ref("player_start")
			}
		}
		return where
	case ref.TypeRoom:
		return ref.GlobalEnvironment
	case ref.TypeProgram:
		return o.Owner
	}
	return ref.Nothing
}

// moveObject is move.c's moveto: put what inside where, resolving
// HOME and redirecting rather than refusing when that would make a
// loop.
//
// Garbage is not moved at all, which upstream checks first and which
// matters because a recycled object still has a location.
func moveObject(w *world.World, what, where ref.Ref) {
	o := w.Get(what)
	if o == nil || o.Type() == ref.TypeGarbage {
		return
	}

	switch {
	case where == ref.Nothing:
		// NOTHING has no contents to push onto, so this is a
		// removal rather than a move.
	case where == ref.Home:
		where = fallbackHome(w, what)
	default:
		if parentLoopCheck(w, what, where) {
			where = fallbackHome(w, what)
		}
	}

	// The store's own check is the same question asked again, and
	// by here the answer is no; a failure would mean the ladder
	// above ran out, which only a damaged world can do. Taking
	// the object out of the world is the one outcome that cannot
	// itself fail, and @sanity reports it.
	if err := w.MoveTo(what, where); err != nil {
		_ = w.MoveTo(what, ref.Nothing)
	}
}

// enterRoom is move.c:123's enter_room: move a player, and narrate
// it.
//
// Upstream's order is load-bearing and is kept. The destination is
// resolved and loop-checked *before* the self-loop test, so a move
// redirected back to where the player already is prints nothing; the
// departure messages come before the move is announced anywhere else;
// and the autolook happens before the penny check, which is why
// finding one reads as a remark on the room you have just seen.
//
// The six propqueue calls are named and not made. They are tranche
// three, and they are the only thing here that would change how an
// existing world behaves.
func (s *Server) enterRoom(w *world.World, descr int,
	who, loc, exit ref.Ref) {

	o := w.Get(who)
	if o == nil {
		return
	}
	if loc == ref.Home {
		loc = o.Home
	}
	if parentLoopCheck(w, who, loc) {
		loc = fallbackHome(w, who)
	}
	old := o.Location

	if loc != old {
		moveObject(w, who, loc)

		if old != ref.Nothing {
			// propqueue: _depart, _odepart.
			s.announceMove(w, who, old, exit, "%s has left.")

			// A room whose drop-to is STICKY holds its
			// contents until the last person leaves.
			if r := w.Get(old); r != nil &&
				r.Type() == ref.TypeRoom &&
				r.Dropto != ref.Nothing &&
				r.Flags&ref.Sticky != 0 {
				s.maybeDropto(w, descr, old, r.Dropto)
			}
		}
		s.announceMove(w, who, loc, exit, "%s has arrived.")
	}

	s.autolook(w, descr, who)

	if loc != old {
		s.maybeFindPenny(w, who, loc)
		// propqueue: _arrive, _oarrive. After the autolook,
		// which upstream comments on: a message from them
		// would otherwise be lost in the spam of the move.
	}
}

// announceMove tells a room that somebody came or went, subject to
// upstream's five-part gate.
//
// Emerald announced every move unconditionally. Upstream stays quiet
// when quiet_moves is set, when either the room or the mover is DARK,
// when the mover is a THING that is neither a ZOMBIE nor a VEHICLE
// — which is most of what keeps a puppet-heavy world readable —
// and when the exit taken is itself DARK.
func (s *Server) announceMove(w *world.World, who, room, exit ref.Ref,
	format string) {

	o, r := w.Get(who), w.Get(room)
	if o == nil || r == nil {
		return
	}
	if w.Tune.Bool("quiet_moves") {
		return
	}
	if r.Flags&ref.Dark != 0 || o.Flags&ref.Dark != 0 {
		return
	}
	if o.Type() == ref.TypeThing &&
		o.Flags&(ref.Zombie|ref.Vehicle) == 0 {
		return
	}
	if e := w.Get(exit); e != nil &&
		e.Type() == ref.TypeExit && e.Flags&ref.Dark != 0 {
		return
	}
	s.notifyRoom(w, room, []ref.Ref{who}, format, o.Name)
}

// maybeDropto is move.c:51: when the last player leaves a room whose
// drop-to is STICKY, everything left behind goes through it.
//
// A zombie counts as a player for this, but only while zombies are
// allowed at all — so turning allow_zombies off empties rooms that
// were being held open by one.
func (s *Server) maybeDropto(w *world.World, descr int,
	loc, dropto ref.Ref) {

	if loc == dropto {
		// Upstream calls this the bizarre special case, and
		// it would otherwise sweep a room into itself.
		return
	}
	zombies := w.Tune.Bool("allow_zombies")
	for _, r := range w.Contents(loc) {
		o := w.Get(r)
		if o == nil {
			continue
		}
		if o.Type() == ref.TypePlayer {
			return
		}
		if zombies && o.Type() == ref.TypeThing &&
			o.Flags&ref.Zombie != 0 {
			return
		}
	}
	s.sendContents(w, descr, loc, dropto)
}

// sendContents is move.c:370: move everything in loc to dest.
//
// Three rules inside it are upstream's. Only things and programs
// travel — a room or an exit that somehow ended up in the contents
// chain is put back where it was. A STICKY thing goes home instead of
// to the drop-to. And anything that cannot go where it was sent stays
// put rather than being redirected, which is the one place upstream
// does *not* take the fallback ladder.
func (s *Server) sendContents(w *world.World, descr int,
	loc, dest ref.Ref) {

	// The list is taken first, because moving out of a chain
	// rewrites it.
	contents := w.Contents(loc)
	for _, r := range contents {
		o := w.Get(r)
		if o == nil {
			continue
		}
		if t := o.Type(); t != ref.TypeThing &&
			t != ref.TypeProgram {
			continue
		}
		where := dest
		if o.Flags&ref.Sticky != 0 {
			where = ref.Home
		}
		if parentLoopCheck(w, r, where) {
			continue
		}
		if w.Tune.Bool("secure_thing_movement") &&
			o.Type() == ref.TypeThing {
			s.enterRoom(w, descr, r, where, o.Location)
			continue
		}
		moveObject(w, r, where)
	}
}

// autolook is what enter_room does after a move: run the command
// named by autolook_cmd, falling back to look_room when it does not
// resolve to anything.
//
// The recursion guard is upstream's own: a room whose autolook
// command moves the player again would otherwise not stop, and the
// message it gives when it does stop is part of the interface.
func (s *Server) autolook(w *world.World, descr int, who ref.Ref) {
	o := w.Get(who)
	if o == nil {
		return
	}
	// A plain thing does not look around; a puppet or a vehicle
	// does, because somebody is reading its output.
	if o.Type() == ref.TypeThing &&
		o.Flags&(ref.Zombie|ref.Vehicle) == 0 {
		return
	}

	if s.lookDepth >= 8 {
		s.notify(w, who, "Look aborted because of look action loop.")
		return
	}
	s.lookDepth++
	defer func() { s.lookDepth-- }()

	cmd := w.Tune.String("autolook_cmd")
	if cmd != "" {
		// can_move then do_move: the autolook command is an
		// *exit* if the world defines one, and only otherwise
		// the built-in.
		if r := match.New(w, who, cmd).Exits().Result(); r !=
			ref.Nothing && r != ref.Ambiguous {
			d := s.hub.Get(descr)
			if d != nil {
				c := &ctx{w: w, d: d, who: who,
					out: d.Send, verb: cmd}
				s.useExit(c, r)
				return
			}
		}
	}
	s.lookRoom(w, descr, who, o.Location)
}

// maybeFindPenny is enter_room's tail: moving about occasionally
// pays.
//
// penny_rate is the reciprocal of the chance, and zero turns it off
// entirely — which is what the golden fixture needs, since two
// servers cannot agree about a coin flip. Controlling the room
// exempts it, so a builder cannot farm their own house.
func (s *Server) maybeFindPenny(w *world.World, who, loc ref.Ref) {
	rate := int(w.Tune.Int("penny_rate"))
	if rate == 0 {
		return
	}
	owner := ownerOf(w, who)
	if s.controls(w, who, loc) {
		return
	}
	if valueOf(w, owner) > w.Tune.Int("max_pennies") {
		return
	}
	if rand.Intn(rate) != 0 {
		return
	}
	s.notify(w, who, "You found one %s!",
		w.Tune.String("penny"))
	w.SetProp(owner, propValue, props.Value{Type: props.Int,
		Num: valueOf(w, owner) + 1})
}
