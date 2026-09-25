package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Interp implements muf.Host for INTERP: run prog to completion in
// the foreground and hand back what it left on its stack.
//
// This is RunLock's shape — a nested, synchronous frame the caller
// waits on — with the same two limits, for the same reasons. A
// program that blocks on READ or SLEEP cannot be resumed from inside
// a caller that is itself mid-instruction, so it counts as producing
// nothing; and a program that aborts reports its own error and yields
// nothing, rather than taking the calling program down with it, which
// is upstream's own interp_loop returning NULL.
func (h *mufHost) Interp(descr, level int, prog, trig ref.Ref,
	cmd, arg string) (muf.Value, bool) {

	p, err := h.s.compileProgram(h.w, prog)
	if err != nil {
		return muf.Value{}, false
	}

	player := h.caller
	host := &mufHost{s: h.s, w: h.w, caller: player}
	f := muf.NewFrame(p, host)
	// INTERP runs its target HARDUID. p_stack.c:1763.
	f.Perms = muf.HardUID
	// COMMAND and the pushed argument are two different strings,
	// upstream's match_cmdname and match_args; SetReserved's own
	// convention makes them one.
	f.SetReserved(player, h.Location(player), trig, cmd)
	f.Stack[len(f.Stack)-1] = muf.Str(arg)
	f.Descr = descr
	f.Level = level + 1
	// Upstream runs this frame PREEMPT: it gets the world to
	// itself until it finishes, which is what lets the caller
	// treat it as one instruction.
	f.Mode = muf.ModePreempt

	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			return muf.Value{}, false
		}
		if res == muf.Done {
			break
		}
		if res == muf.Blocked {
			return muf.Value{}, false
		}
	}
	if len(f.Stack) == 0 {
		return muf.Value{}, false
	}
	return f.Stack[len(f.Stack)-1], true
}
