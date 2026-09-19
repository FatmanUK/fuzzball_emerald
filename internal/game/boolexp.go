package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// lockHost implements boolexp.Host against the live world, the way mufHost
// and mpiHost do for their own packages.
type lockHost struct {
	s *Server
	w *world.World
	// level is this evaluation's own interpreter nesting depth — see
	// muf.Frame.Level. A program-type lock constant that RunLock starts runs
	// one level deeper, so a chain of TESTLOCK-triggered lock checks cannot
	// recurse forever. Zero behaves as 1, a fresh top-level check.
	level int
}

var _ boolexp.Host = (*lockHost)(nil)

func (h *lockHost) Match(player ref.Ref, name string) ref.Ref {
	return match.New(h.w, player, name).
		Neighbor().Possession().Me().Here().Absolute().Registered().Player().
		Result()
}

func (h *lockHost) Wizard(player ref.Ref) bool {
	if o := h.w.Get(player); o != nil {
		return o.Flags.IsWizard()
	}
	return false
}

func (h *lockHost) Name(r ref.Ref) string { return nameOf(h.w, r) }

func (h *lockHost) Valid(r ref.Ref) bool { return h.w.Valid(r) }

func (h *lockHost) Type(r ref.Ref) ref.ObjType {
	if o := h.w.Get(r); o != nil {
		return o.Type()
	}
	return ref.NoType
}

func (h *lockHost) Owner(r ref.Ref) ref.Ref {
	if o := h.w.Get(r); o != nil {
		return o.Owner
	}
	return ref.Nothing
}

func (h *lockHost) Location(r ref.Ref) ref.Ref {
	if o := h.w.Get(r); o != nil {
		return o.Location
	}
	return ref.Nothing
}

func (h *lockHost) Contents(r ref.Ref) []ref.Ref { return h.w.Contents(r) }

func (h *lockHost) Flags(r ref.Ref) ref.Flags {
	if o := h.w.Get(r); o != nil {
		return o.Flags
	}
	return 0
}

func (h *lockHost) Parent(r ref.Ref) ref.Ref { return h.w.Parent(r) }

func (h *lockHost) LockEnvCheck() bool { return h.w.Tune.Bool("lock_envcheck") }

func (h *lockHost) Prop(r ref.Ref, path string) (props.Value, bool) {
	return h.w.GetProp(r, path)
}

// EvalLockProp is has_property_strict's do_parse_mesg call: the property's
// text is run through MPI with what as both the message's object and the
// permissions it runs with, and its Blessed flag granting wizard permissions
// the way a blessed property does everywhere else.
func (h *lockHost) EvalLockProp(descr int, player, what ref.Ref, raw string, blessed bool) string {
	env := &mpi.Env{
		Who:     mpi.Ref(player),
		What:    mpi.Ref(what),
		Perms:   mpi.Ref(what),
		Blessed: blessed,
		Descr:   descr,
		Host:    &mpiHost{s: h.s, w: h.w},
	}
	return mpi.Eval(env, raw)
}

// RunLock runs a TYPE_PROGRAM lock constant to completion in the foreground,
// as eval_boolexp_rec's interp()/interp_loop() call does, and reports whether
// it finished rather than aborting.
//
// Two things upstream does are simplified here. interp() also checks that
// thing's owner may link to prog the way linking an exit to it would — that
// permission gate is not reproduced, so any compiling program is allowed to
// run as a lock. And a program that blocks on READ or SLEEP mid-evaluation
// cannot be resumed from inside a synchronous lock check, so it is treated as
// a failure rather than suspended and later retried.
func (h *lockHost) RunLock(descr int, player, prog, thing ref.Ref) bool {
	p, err := h.s.compileProgram(h.w, prog)
	if err != nil {
		return false
	}

	realPlayer := player
	if t := h.Type(player); t != ref.TypePlayer && t != ref.TypeThing {
		realPlayer = h.Owner(player)
	}

	host := &mufHost{s: h.s, w: h.w, caller: realPlayer}
	f := muf.NewFrame(p, host)
	f.SetReserved(realPlayer, h.Location(player), thing, "")
	f.Descr = descr

	callerLevel := h.level
	if callerLevel == 0 {
		callerLevel = 1
	}
	f.Level = callerLevel + 1

	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			return false
		}
		switch res {
		case muf.Done:
			return true
		case muf.Blocked:
			return false
		}
	}
}

// TestLock implements muf.Host for the TESTLOCK primitive.
func (h *mufHost) TestLock(descr, level int, testPlayer ref.Ref, lock *boolexp.Expr, trig, caller ref.Ref) (bool, error) {
	thing := caller
	if h.w.Tune.Bool("consistent_lock_source") {
		thing = trig
	}
	lh := &lockHost{s: h.s, w: h.w, level: level}
	return boolexp.Eval(lh, descr, testPlayer, lock, thing), nil
}

// Locked implements muf.Host for the LOCKED? primitive: LOCKED? is could_doit
// negated.
func (h *mufHost) Locked(descr, level int, player, thing ref.Ref) (bool, error) {
	return !couldDoit(h.s, h.w, descr, level, player, thing), nil
}

// MaxInterpRecursion implements muf.Host, reading the max_interp_recursion
// @tune parameter LOCKED?'s own recursion guard is bounded by.
func (h *mufHost) MaxInterpRecursion() int {
	return int(h.w.Tune.Int("max_interp_recursion"))
}

// couldDoit is upstream's could_doit: if thing is an exit, the destination it
// would move player to must itself be reachable (JUMP_OK, GUEST rooms,
// BUILDER-restricted sources, secure_teleport); then, exit or not, thing's
// own @lock must pass. This is what LOCKED? negates, and — once exit
// traversal starts checking locks — what a "go" command will also need.
func couldDoit(s *Server, w *world.World, descr, level int, player, thing ref.Ref) bool {
	o := w.Get(thing)
	if o != nil && o.Type() == ref.TypeExit {
		if len(o.Dest) == 0 {
			return false
		}

		owner := o.Owner
		playerObj := w.Get(player)
		source := ref.Nothing
		if playerObj != nil {
			source = playerObj.Location
		}
		dest := o.Dest[0]

		if dest == ref.Nil {
			return s.lockPasses(w, descr, level, player, thing, propLock, true)
		}

		if dp := w.Get(dest); dp != nil && dp.Type() == ref.TypePlayer {
			dest = dp.Location
			destRoom := w.Get(dest)
			if dp.Flags&ref.JumpOK == 0 || (destRoom != nil && destRoom.Flags&ref.Builder != 0) {
				return false
			}
		}

		if destObj := w.Get(dest); dest != ref.Home && destObj != nil && destObj.Type() == ref.TypeRoom &&
			destObj.Flags&ref.Guest != 0 && isGuest(w, player) {
			return false
		}

		exitLoc := w.Get(o.Location)
		if o.Location != ref.Nothing && exitLoc != nil && exitLoc.Type() != ref.TypeRoom {
			destObj := w.Get(dest)
			sourceObj := w.Get(source)
			if destObj != nil && (destObj.Type() == ref.TypeRoom || destObj.Type() == ref.TypePlayer) &&
				sourceObj != nil && sourceObj.Flags&ref.Builder != 0 {
				return false
			}

			if w.Tune.Bool("secure_teleport") && destObj != nil && destObj.Type() == ref.TypeRoom {
				if dest != ref.Home && !s.controls(w, owner, source) &&
					sourceObj != nil && sourceObj.Flags&ref.JumpOK == 0 {
					return false
				}
			}
		}
	}

	return s.lockPasses(w, descr, level, player, thing, propLock, true)
}

// isGuest is upstream's ISGUEST, under GOD_PRIV: a guest-flagged object that
// is not God.
func isGuest(w *world.World, r ref.Ref) bool {
	o := w.Get(r)
	return o != nil && o.Flags&ref.Guest != 0 && r != ref.God
}
