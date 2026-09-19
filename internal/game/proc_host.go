package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ForceLevel, IsPID and Instances implement muf.Host for FORCE_LEVEL,
// ISPID? and INSTANCES.

func (h *mufHost) ForceLevel() int { return h.s.forceDepth }

func (h *mufHost) IsPID(pid int) bool { return h.s.procs.get(pid) != nil }

func (h *mufHost) Instances(prog ref.Ref) int {
	return len(h.s.procs.forProgram(prog))
}

// CanCall implements muf.Host for CANCALL?.
func (h *mufHost) CanCall(callerLevel int, callerUID ref.Ref, prog ref.Ref, name string) bool {
	p, err := h.s.compileProgram(h.w, prog)
	if err != nil || p.MLevel <= 0 {
		return false
	}

	if callerLevel < 4 && h.Owner(prog) != callerUID && !linkable(h.Flags(prog), h.ObjType(prog)) {
		return false
	}

	pub, ok := p.Publics[ascii.Fold(name)]
	if !ok {
		return false
	}
	return callerLevel >= pub.MLevel
}

// linkable is upstream's Linkable macro: a room or thing needs ABODE, anything
// else — a player, exit or program — needs LINK_OK. The two flags share no
// bit, so the check genuinely differs by type rather than being the same test
// worded twice.
func linkable(f ref.Flags, t ref.ObjType) bool {
	if t == ref.TypeRoom || t == ref.TypeThing {
		return f&ref.Abode != 0
	}
	return f&ref.LinkOK != 0
}

// ControlsProcess implements muf.Host for KILL, upstream's control_process:
// whether callerUID controls the process's program, controls its trigger, or
// is the player it is running for. A pid procQueue does not know about
// answers false the same way upstream's own "not in the timequeue, not in
// the event queue either" fallthrough does.
func (h *mufHost) ControlsProcess(callerUID ref.Ref, pid int) bool {
	p := h.s.procs.get(pid)
	if p == nil {
		return false
	}
	return h.s.controls(h.w, callerUID, p.program) ||
		h.s.controls(h.w, callerUID, p.trigger) ||
		callerUID == p.player
}

// KillPID implements muf.Host for KILL, upstream's dequeue_process.
func (h *mufHost) KillPID(pid int) bool {
	if h.s.procs.get(pid) == nil {
		return false
	}
	h.s.procs.remove(pid)
	return true
}

// Fork implements muf.Host for FORK, upstream's add_muf_delay_event(0, ...)
// call: register child as a new background process, subject to the same
// process-count limits every other way onto the queue respects.
func (h *mufHost) Fork(child *muf.Frame) int {
	if !h.s.processLimitOK(h.w, h.caller) {
		// Two spaces after the period is upstream's own literal wording.
		h.s.notify(h.w, h.caller, "Event killed.  Timequeue table full.")
		return 0
	}

	proc := &process{
		frame:   child,
		player:  h.caller,
		program: child.Prog.Ref,
		trigger: child.Trig,
		descr:   child.Descr,
		command: "Forked Process.",
		started: h.w.Now(),
	}
	pid := h.s.procs.add(proc)
	child.PID = pid
	return pid
}

// processLimitOK is upstream's add_event process-count gate: the system-wide
// max_process_limit applies to everyone, wizards included, but
// max_plyr_processes exempts a wizard. Both counts are taken before the new
// process would be added, matching upstream's own pre-increment check.
//
// Unlike upstream's tqhead, procQueue also holds the process currently
// running synchronously in the foreground — the one making this very check —
// so both counts run one process higher here than the equivalent upstream
// count would for the same state. Documented rather than corrected: matching
// it exactly would mean threading "which process is this check being made
// from" through Host, for a one-off difference at the very edge of a tune
// parameter's default.
func (s *Server) processLimitOK(w *world.World, player ref.Ref) bool {
	if int64(len(s.procs.all())) > w.Tune.Int("max_process_limit") {
		return false
	}
	if int64(len(s.procs.forPlayer(player))) <= w.Tune.Int("max_plyr_processes") {
		return true
	}
	if o := w.Get(player); o != nil && o.Flags.IsWizard() {
		return true
	}
	return false
}
