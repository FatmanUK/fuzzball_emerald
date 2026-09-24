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

func (h *mufHost) IsPID(pid int) bool {
	return h.s.procs.get(pid) != nil
}

func (h *mufHost) Instances(prog ref.Ref) int {
	return len(h.s.procs.forProgram(prog))
}

// CanCall implements muf.Host for CANCALL?.
func (h *mufHost) CanCall(callerLevel int, callerUID ref.Ref, prog ref.Ref, name string) bool {
	p, err := h.s.compileProgram(h.w, prog)
	if err != nil || p.MLevel <= 0 {
		return false
	}

	if callerLevel < 4 && h.Owner(prog) != callerUID &&
		!linkable(h.Flags(prog), h.ObjType(prog)) {
		return false
	}

	pub, ok := p.Publics[ascii.Fold(name)]
	if !ok {
		return false
	}
	return callerLevel >= pub.MLevel
}

// linkable is upstream's Linkable macro: a room or thing needs ABODE,
// anything else — a player, exit or program — needs LINK_OK. The
// two flags share no bit, so the check genuinely differs by type
// rather than being the same test worded twice.
func linkable(f ref.Flags, t ref.ObjType) bool {
	if t == ref.TypeRoom || t == ref.TypeThing {
		return f&ref.Abode != 0
	}
	return f&ref.LinkOK != 0
}

// ControlsProcess implements muf.Host for KILL, upstream's
// control_process: whether callerUID controls the process's program,
// controls its trigger, or is the player it is running for. A pid
// procQueue does not know about answers false the same way upstream's
// own "not in the timequeue, not in the event queue either"
// fallthrough does.
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
	p := h.s.procs.get(pid)
	if p == nil {
		return false
	}
	h.s.finishProcess(h.w, p)
	return true
}

// Fork implements muf.Host for FORK, upstream's
// add_muf_delay_event(0, ...) call: register child as a new
// background process, subject to the same process-count limits every
// other way onto the queue respects.
func (h *mufHost) Fork(child *muf.Frame) int {
	if !h.s.processLimitOK(h.w, h.caller) {
		// Two spaces after the period is upstream's own
		// literal wording.
		h.s.notify(h.w, h.caller, "Event killed.  Timequeue table full.")
		return 0
	}

	proc := &process{
		frame:      child,
		player:     h.caller,
		program:    child.Prog.Ref,
		trigger:    child.Trig,
		descr:      child.Descr,
		command:    "Forked Process.",
		started:    h.w.Now(),
		calledData: "BACKGROUND",
	}
	pid := h.s.procs.add(proc)
	child.PID = pid
	child.Started = proc.started
	return pid
}

// Queue implements muf.Host for QUEUE, upstream's
// add_muf_delayq_event.
//
// Unlike upstream, which defers compiling and building a frame until
// the event actually fires, this builds both eagerly, at QUEUE-call
// time — the same simplification runProgram already makes for a
// command-driven program, just with the process starting procSleeping
// instead of running immediately. The one observable gap: an edit to
// prog between now and when it fires is not picked up, since the
// frame already holds a compiled *muf.Program rather than recompiling
// at fire time the way upstream's lazy interp() call would.
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
	// COMMAND is "Queued Event." and the pushed stack argument is
	// arg — upstream's match_cmdname and match_args, two
	// different strings, unlike SetReserved's own command-driven
	// convention where they are the same one. Overwriting the
	// pushed value after the fact is simpler than a second
	// SetReserved variant for what only QUEUE needs.
	f.SetReserved(h.caller, loc, ref.Nothing, "Queued Event.")
	f.Stack[len(f.Stack)-1] = muf.Str(arg)
	f.Descr = descr
	f.Mode = muf.ModeBackground

	proc := &process{
		frame:      f,
		player:     h.caller,
		program:    prog,
		trigger:    ref.Nothing,
		descr:      descr,
		command:    "Queued Event.",
		started:    h.w.Now(),
		state:      procSleeping,
		wake:       h.w.Now().Add(time.Duration(seconds) * time.Second),
		calledData: arg,
	}
	pid := h.s.procs.add(proc)
	f.PID = pid
	f.Started = proc.started
	return pid
}

// Force implements muf.Host for FORCE, upstream's process_command
// call wrapped in the forcelist bookkeeping — see cmdForce's own
// identical pattern in wiz.go, which this mirrors except for pushing
// program as well as player, matching prim_force's own "if (player !=
// program)" second push. If descr no longer names a live connection
// — the calling frame's player disconnected since a background
// process holding it was forked or queued — this quietly does
// nothing, since there is no descriptor left to force the command
// through; upstream has no equivalent failure mode, as
// dbref_first_descr and process_command work from a plain int
// throughout.
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
// FORCEDBY_ARRAY, reading Server.forcelist — see its own doc
// comment.
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

// processLimitOK is upstream's add_event process-count gate: the
// system-wide max_process_limit applies to everyone, wizards
// included, but max_plyr_processes exempts a wizard. Both counts are
// taken before the new process would be added, matching upstream's
// own pre-increment check.
//
// Unlike upstream's tqhead, procQueue also holds the process
// currently running synchronously in the foreground — the one
// making this very check — so both counts run one process higher
// here than the equivalent upstream count would for the same state.
// Documented rather than corrected: matching it exactly would mean
// threading "which process is this check being made from" through
// Host, for a one-off difference at the very edge of a tune
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

// GetPIDs implements muf.Host for GETPIDS, upstream's get_pids.
// procQueue holds every process — foreground, background and
// blocked alike — unlike upstream's own separate timequeue and
// mufevent-queue, which never contain a still-running foreground
// process at all; selfPID is excluded here to match that, leaving
// GETPIDS's own primFunc to add it back only for the one case
// upstream itself does.
func (h *mufHost) GetPIDs(obj ref.Ref, selfPID int) []int {
	var out []int
	for _, p := range h.s.procs.all() {
		if p.pid == selfPID {
			continue
		}
		if p.program == obj || p.player == obj || obj < 0 {
			out = append(out, p.pid)
		}
	}
	return out
}

// PIDInfo implements muf.Host for GETPIDINFO's other-pid branch,
// upstream's get_pidinfo. Unlike get_pidinfo, there is no separate
// MUF-event queue to fall back to when pid is not found in the main
// one — procQueue already holds an EVENT_WAITFOR-blocked process
// the same way it holds every other kind — so a missing pid simply
// reports ok=false.
//
// SUBTYPE mirrors upstream's own TQ_MUF_* mapping as closely as
// procState allows: "READ" for a blocked READ, "QUEUE" for a
// QUEUE-created process (recognised by its own reserved COMMAND,
// since both SLEEP and QUEUE share procSleeping), and "DELAY" for
// everything add_muf_delay_event covers upstream — SLEEP, and a
// runnable foreground or forked process — which upstream itself
// gives the same subtype regardless of BACKGROUND/FOREGROUND mode. An
// EVENT_WAITFOR-blocked process gets "", matching
// get_mufevent_pidinfo's own hardcoded SUBTYPE.
func (h *mufHost) PIDInfo(pid int) (muf.PIDInfo, bool) {
	p := h.s.procs.get(pid)
	if p == nil {
		return muf.PIDInfo{}, false
	}

	subtype := "DELAY"
	switch p.state {
	case procReading:
		subtype = "READ"
	case procSleeping:
		if p.command == "Queued Event." {
			subtype = "QUEUE"
		}
	case procWaiting:
		subtype = ""
	}

	return muf.PIDInfo{
		CalledProg: p.program,
		CalledData: p.calledData,
		Descr:      p.descr,
		InstCnt:    p.frame.Instructions,
		NextRun:    h.nextRun(p),
		Player:     p.player,
		Started:    p.started,
		Subtype:    subtype,
		Trig:       p.trigger,
	}, true
}

// WatchPID implements muf.Host for WATCHPID's "target exists" branch,
// upstream's frame-found half of prim_watchpid. Unlike upstream's own
// dedup check — which compares a stored caller pid against the
// target pid it is searching for, so it can only ever match by
// coincidence — this dedups on whether callerPID already appears in
// targetPID's waiters, the check upstream's own comment describes
// wanting.
func (h *mufHost) WatchPID(callerPID, targetPID int) bool {
	target := h.s.procs.get(targetPID)
	if target == nil {
		return false
	}
	for _, pid := range target.waiters {
		if pid == callerPID {
			return true
		}
	}
	target.waiters = append(target.waiters, callerPID)
	if caller := h.s.procs.get(callerPID); caller != nil {
		caller.waitees = append(caller.waitees, targetPID)
	}
	return true
}

// nextRun approximates upstream's own ptr->when: a real wake time for
// a sleeping process (SLEEP or QUEUE alike), and "now" for everything
// else — upstream's own dtime is 0 for a runnable process and -1
// for a READ, both of which land within a second of "now" too.
func (h *mufHost) nextRun(p *process) int64 {
	if p.state == procSleeping {
		return p.wake.Unix()
	}
	return h.w.Now().Unix()
}

// timerEventName is the event a fired timer delivers, upstream's
// "TIMER.%.32s". The id is truncated, so two timers whose names
// differ only past the 32nd character are the same timer.
func timerEventName(id string) string {
	const maxTimerID = 32
	if len(id) > maxTimerID {
		id = id[:maxTimerID]
	}
	return "TIMER." + id
}

// TimerCount implements muf.Host for TIMER_START's own limit check.
func (h *mufHost) TimerCount(pid int) int {
	p := h.s.procs.get(pid)
	if p == nil {
		return 0
	}
	return len(p.timers)
}

// TimerStart implements muf.Host for TIMER_START. Restarting a timer
// that is already running replaces it rather than adding a second,
// which is upstream's own dequeue-then-add.
func (h *mufHost) TimerStart(pid int, id string, seconds int64) {
	p := h.s.procs.get(pid)
	if p == nil {
		return
	}
	h.TimerStop(pid, id)
	p.timers = append(p.timers, mufTimer{
		name:  id,
		fires: h.w.Now().Add(time.Duration(seconds) * time.Second),
	})
}

// TimerStop implements muf.Host for TIMER_STOP.
func (h *mufHost) TimerStop(pid int, id string) {
	p := h.s.procs.get(pid)
	if p == nil {
		return
	}
	want := timerEventName(id)
	kept := p.timers[:0]
	for _, t := range p.timers {
		if timerEventName(t.name) != want {
			kept = append(kept, t)
		}
	}
	p.timers = kept
}

// SendEvent implements muf.Host for EVENT_SEND: queue a USER.<id>
// event on another live process, resuming it if it is already waiting
// for one.
//
// It reports whether pid named a live process. Upstream silently does
// nothing when it does not, which EVENT_SEND reproduces — a process
// that has already finished is not an error to send to.
func (h *mufHost) SendEvent(pid int, name string, data muf.Value) bool {
	p := h.s.procs.get(pid)
	if p == nil {
		return false
	}
	h.s.deliverEvent(h.w, p, name, data)
	return true
}
