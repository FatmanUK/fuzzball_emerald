package game

import (
	"time"

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

// Queue implements muf.Host for QUEUE, upstream's add_muf_delayq_event.
//
// Unlike upstream, which defers compiling and building a frame until the
// event actually fires, this builds both eagerly, at QUEUE-call time — the
// same simplification runProgram already makes for a command-driven program,
// just with the process starting procSleeping instead of running
// immediately. The one observable gap: an edit to prog between now and when
// it fires is not picked up, since the frame already holds a compiled
// *muf.Program rather than recompiling at fire time the way upstream's
// lazy interp() call would.
func (h *mufHost) Queue(descr int, prog ref.Ref, seconds int64, arg string) int {
	if !h.s.processLimitOK(h.w, h.caller) {
		h.s.notify(h.w, h.caller, "Event killed.  Timequeue table full.")
		return 0
	}

	p, err := h.s.compileProgram(h.w, prog)
	if err != nil {
		return 0
	}

	host := &mufHost{s: h.s, w: h.w, caller: h.caller}
	f := muf.NewFrame(p, host)
	loc := ref.Nothing
	if me := h.w.Get(h.caller); me != nil {
		loc = me.Location
	}
	// COMMAND is "Queued Event." and the pushed stack argument is arg —
	// upstream's match_cmdname and match_args, two different strings, unlike
	// SetReserved's own command-driven convention where they are the same
	// one. Overwriting the pushed value after the fact is simpler than a
	// second SetReserved variant for what only QUEUE needs.
	f.SetReserved(h.caller, loc, ref.Nothing, "Queued Event.")
	f.Stack[len(f.Stack)-1] = muf.Str(arg)
	f.Descr = descr
	f.Mode = muf.ModeBackground

	proc := &process{
		frame:   f,
		player:  h.caller,
		program: prog,
		trigger: ref.Nothing,
		descr:   descr,
		command: "Queued Event.",
		started: h.w.Now(),
		state:   procSleeping,
		wake:    h.w.Now().Add(time.Duration(seconds) * time.Second),
	}
	pid := h.s.procs.add(proc)
	f.PID = pid
	return pid
}

// Force implements muf.Host for FORCE, upstream's process_command call
// wrapped in the forcelist bookkeeping — see cmdForce's own identical
// pattern in wiz.go, which this mirrors except for pushing program as well
// as player, matching prim_force's own "if (player != program)" second
// push. If descr no longer names a live connection — the calling frame's
// player disconnected since a background process holding it was forked or
// queued — this quietly does nothing, since there is no descriptor left to
// force the command through; upstream has no equivalent failure mode, as
// dbref_first_descr and process_command work from a plain int throughout.
func (h *mufHost) Force(descr int, player, program, victim ref.Ref, command string) {
	d := h.s.hub.Get(descr)
	if d == nil {
		return
	}

	h.s.forcelist = append(h.s.forcelist, player)
	n := 1
	if program != player {
		h.s.forcelist = append(h.s.forcelist, program)
		n = 2
	}
	defer func() { h.s.forcelist = h.s.forcelist[:len(h.s.forcelist)-n] }()

	h.s.force(h.w, d, victim, command)
}

// ForcedBy and ForcedByArray implement muf.Host for FORCEDBY and
// FORCEDBY_ARRAY, reading Server.forcelist — see its own doc comment.
func (h *mufHost) ForcedBy() ref.Ref {
	if n := len(h.s.forcelist); n > 0 {
		return h.s.forcelist[n-1]
	}
	return ref.Nothing
}

func (h *mufHost) ForcedByArray() []ref.Ref {
	n := len(h.s.forcelist)
	out := make([]ref.Ref, n)
	for i := range out {
		out[i] = h.s.forcelist[n-1-i]
	}
	return out
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
