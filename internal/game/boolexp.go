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

// Name is unparse_object, already ported as unparse (used by @examine and
// wizard output): r's bare name, or "name(#dbref FLAGS)" when viewer
// controls r or may otherwise see its flags.
func (h *lockHost) Name(viewer, r ref.Ref) string { return unparse(h.w, viewer, r) }

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

// LockString implements muf.Host for GETLOCKSTR.
func (h *mufHost) LockString(obj ref.Ref) string {
	v, ok := h.w.GetProp(obj, propLock)
	if !ok || v.Type != props.Lock || v.Str == "" {
		return boolexp.Unlocked
	}
	return v.Str
}

// SetLockString implements muf.Host for SETLOCKSTR, which is upstream's
// _set_lock called with silent true: it still forwards a match failure's own
// message — that comes from inside parse_boolexp itself, not from
// _set_lock, so upstream's silent flag never suppresses it — but not "Lock
// set."/"Lock cleared."/"I don't understand that key.", which are
// _set_lock's own and do respect silent.
func (h *mufHost) SetLockString(descr int, matchPlayer, obj ref.Ref, raw string) bool {
	return h.s.setLock(h.w, descr, matchPlayer, obj, propLock, "Lock", raw, true)
}

// setLock is upstream's _set_lock: parse keyvalue with player's own matching
// context and store it as object's path property in its unparsed dbref
// form, or clear the property when keyvalue is empty. A match failure's own
// message (from inside boolexp.Parse, mirroring parse_boolexp's own embedded
// notify calls) always reaches player, regardless of silent; only _set_lock's
// own messages — "Lock set.", "Lock cleared." or "I don't understand that
// key." — are what silent gates, which is what distinguishes the
// @lock-family commands (silent false) from SETLOCKSTR (silent true). It
// reports whether the lock was set, which is false only when keyvalue failed
// to parse.
func (s *Server) setLock(w *world.World, descr int, player, object ref.Ref, path, label, keyvalue string, silent bool) bool {
	if keyvalue == "" {
		w.SetProp(object, path, props.Value{Type: props.Lock, Str: ""})
		if !silent {
			s.notify(w, player, "%s cleared.", label)
		}
		return true
	}

	lh := &lockHost{s: s, w: w}
	key, err := boolexp.Parse(lh, descr, player, keyvalue, false)
	if err != nil {
		if pe, ok := err.(*boolexp.ParseError); ok && pe.Notify {
			s.notify(w, player, "%s", pe.Msg)
		}
		if !silent {
			s.notify(w, player, "I don't understand that key.")
		}
		return false
	}

	w.SetProp(object, path, props.Value{Type: props.Lock, Str: boolexp.Unparse(lh, player, key, false)})
	if !silent {
		s.notify(w, player, "%s set.", label)
	}
	return true
}

// ParseLock implements muf.Host for PARSELOCK, which — unlike SETLOCKSTR —
// has no message of its own to add on failure: only a match failure's own
// message (boolexp.ParseError.Notify) ever reaches matchPlayer, matching
// parse_boolexp's own embedded notify calls exactly.
func (h *mufHost) ParseLock(descr int, matchPlayer ref.Ref, raw string) *boolexp.Expr {
	// A NULL string (as opposed to one merely empty) skips parse_boolexp
	// entirely upstream, going straight to TRUE_BOOLEXP with no match
	// attempt and no message — confirmed against the real server, since an
	// empty string here would otherwise try to match "" as an object name
	// and fail noisily. Go cannot distinguish a null PROG_STRING from an
	// empty one, so this treats every empty raw as upstream's null case,
	// which is what a MUF "" literal actually produces.
	if raw == "" {
		return nil
	}
	lh := &lockHost{s: h.s, w: h.w}
	lock, err := boolexp.Parse(lh, descr, matchPlayer, raw, false)
	if err != nil {
		if pe, ok := err.(*boolexp.ParseError); ok && pe.Notify {
			h.s.notify(h.w, matchPlayer, "%s", pe.Msg)
		}
		return nil
	}
	return lock
}

// UnparseLock implements muf.Host for UNPARSELOCK.
func (h *mufHost) UnparseLock(matchPlayer ref.Ref, lock *boolexp.Expr) string {
	if lock == nil {
		return ""
	}
	lh := &lockHost{s: h.s, w: h.w}
	return boolexp.Unparse(lh, matchPlayer, lock, false)
}

// PrettyLock implements muf.Host for PRETTYLOCK: unparse_boolexp with
// fullname true, so a CONST dbref renders the way a player would see it
// rather than as a bare "#123".
func (h *mufHost) PrettyLock(matchPlayer ref.Ref, lock *boolexp.Expr) string {
	lh := &lockHost{s: h.s, w: h.w}
	return boolexp.Unparse(lh, matchPlayer, lock, true)
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
