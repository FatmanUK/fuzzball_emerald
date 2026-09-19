package game

import (
	"fmt"
	"sort"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// procState says what a suspended program is waiting for.
type procState int

const (
	// procRunnable is ready to continue now, which a yielded program is.
	procRunnable procState = iota
	// procSleeping waits for a time.
	procSleeping
	// procReading waits for a line from its player.
	procReading
	// procWaiting waits for a named event.
	procWaiting
)

func (s procState) String() string {
	switch s {
	case procRunnable:
		return "run"
	case procSleeping:
		return "sleep"
	case procReading:
		return "read"
	case procWaiting:
		return "event"
	}
	return "?"
}

// process is one suspended or running MUF program.
type process struct {
	pid   int
	state procState
	// wake is when a sleeping process becomes runnable.
	wake time.Time
	// events are what a waiting process is listening for.
	events []string

	// calledData is upstream's timequeue called_data: what the process is
	// doing, for GETPIDINFO's CALLED_DATA key. "READ", "SLEEPING" and
	// "EVENT_WAITFOR" for the three ways to block, "FOREGROUND" or
	// "BACKGROUND" for a runnable command-driven or forked process, or the
	// arg string a QUEUE was given.
	calledData string

	// waiters and waitees are upstream's fr->waiters/fr->waitees: WATCHPID
	// bookkeeping. waiters lists the pids watching this process, notified
	// with a PROC.EXIT.<pid> event when it ends; waitees lists the pids this
	// process is watching, so that ending early drops it from their waiters
	// too rather than leaving a stale entry.
	waiters []int
	waitees []int

	frame *muf.Frame

	player  ref.Ref
	program ref.Ref
	trigger ref.Ref
	descr   int
	command string
	started time.Time
}

// procQueue holds every suspended program.
//
// It is owned by the world goroutine, like everything else that touches the
// object graph. Nothing here locks, because nothing else may reach it.
type procQueue struct {
	next  int
	procs map[int]*process
	// reading indexes the process waiting on each descriptor's input, so a
	// line typed at a READ goes to the program rather than the parser.
	reading map[int]int
}

func newProcQueue() *procQueue {
	return &procQueue{procs: map[int]*process{}, reading: map[int]int{}}
}

// add registers a process and returns its id.
func (q *procQueue) add(p *process) int {
	q.next++
	p.pid = q.next
	q.procs[p.pid] = p
	if p.state == procReading && p.descr != 0 {
		q.reading[p.descr] = p.pid
	}
	return p.pid
}

// remove drops a process.
func (q *procQueue) remove(pid int) {
	p, ok := q.procs[pid]
	if !ok {
		return
	}
	if p.state == procReading {
		delete(q.reading, p.descr)
	}
	delete(q.procs, pid)
}

// get returns a process by id.
func (q *procQueue) get(pid int) *process { return q.procs[pid] }

// readerFor returns the process waiting on a descriptor's input, or nil.
func (q *procQueue) readerFor(descr int) *process {
	if pid, ok := q.reading[descr]; ok {
		return q.procs[pid]
	}
	return nil
}

// due lists the processes ready to run at a given time, in pid order so a
// program queued first runs first.
func (q *procQueue) due(now time.Time) []*process {
	var out []*process
	for _, p := range q.procs {
		switch p.state {
		case procRunnable:
			out = append(out, p)
		case procSleeping:
			if !p.wake.After(now) {
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

// all lists every process, in pid order.
func (q *procQueue) all() []*process {
	out := make([]*process, 0, len(q.procs))
	for _, p := range q.procs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

// forProgram lists the processes running one program.
func (q *procQueue) forProgram(prog ref.Ref) []*process {
	var out []*process
	for _, p := range q.all() {
		if p.program == prog {
			out = append(out, p)
		}
	}
	return out
}

// forPlayer lists the processes running for one player, for the
// max_plyr_processes check FORK and QUEUE both make before adding another.
func (q *procQueue) forPlayer(player ref.Ref) []*process {
	var out []*process
	for _, p := range q.all() {
		if p.player == player {
			out = append(out, p)
		}
	}
	return out
}

// Tick runs whatever is due. The engine calls it once per flush interval,
// which is also how often a sleeping program can wake.
func (s *Server) Tick(w *world.World) {
	now := w.Now()
	for _, p := range s.procs.due(now) {
		s.resume(w, p, nil)
	}
}

// resume continues a suspended program, optionally pushing a value first.
//
// A READ is resumed with the line the player typed; everything else resumes
// with nothing new on the stack.
func (s *Server) resume(w *world.World, p *process, push *muf.Value) {
	if p.state == procReading {
		delete(s.procs.reading, p.descr)
	}
	if push != nil {
		if err := p.frame.Push(*push); err != nil {
			s.failProcess(w, p, err)
			return
		}
	}
	p.state = procRunnable
	s.step(w, p)
}

// step runs a process until it finishes, blocks again, or fails.
func (s *Server) step(w *world.World, p *process) {
	for {
		res, err := p.frame.Run(muf.Limits{})
		if err != nil {
			s.failProcess(w, p, err)
			return
		}
		switch res {
		case muf.Done:
			s.finishProcess(w, p)
			return

		case muf.Yielded:
			// The slice ran out. Leave it runnable so the next tick
			// picks it up, which keeps one program from starving
			// the others.
			p.state = procRunnable
			return

		case muf.Blocked:
			s.blockProcess(w, p)
			return
		}
	}
}

// blockProcess files a process under whatever it is waiting for.
func (s *Server) blockProcess(w *world.World, p *process) {
	switch b := p.frame.Block; b.Kind {
	case muf.BlockRead:
		p.state = procReading
		p.calledData = "READ"
		if p.descr != 0 {
			s.procs.reading[p.descr] = p.pid
		}

	case muf.BlockSleep:
		p.state = procSleeping
		p.calledData = "SLEEPING"
		p.wake = w.Now().Add(time.Duration(b.Seconds) * time.Second)

	case muf.BlockEvent:
		p.state = procWaiting
		p.calledData = "EVENT_WAITFOR"
		p.events = b.Events

	default:
		// No reason given, which should not happen; treat it as done
		// rather than leaving a process that nothing will ever resume.
		s.finishProcess(w, p)
	}
}

// failProcess reports a runtime error and removes the process.
func (s *Server) failProcess(w *world.World, p *process, err error) {
	s.finishProcess(w, p)
	s.reportMUFErrorTo(w, p.player, p.frame, p.program, err)
}

// finishProcess is upstream's watchpid_process, called from every path that
// ends a process for good. It notifies whoever WATCHPID'd this pid with a
// PROC.EXIT.<pid> event, drops this pid from the waiters list of whatever it
// was itself watching so a dead process leaves no stale entries behind, and
// only then removes it from the queue.
func (s *Server) finishProcess(w *world.World, p *process) {
	for _, pid := range p.waitees {
		if target := s.procs.get(pid); target != nil {
			target.waiters = removePID(target.waiters, p.pid)
		}
	}
	for _, pid := range p.waiters {
		if waiter := s.procs.get(pid); waiter != nil {
			waiter.waitees = removePID(waiter.waitees, p.pid)
			s.deliverEvent(w, waiter, fmt.Sprintf("PROC.EXIT.%d", p.pid), muf.Int(int64(p.pid)))
		}
	}
	s.procs.remove(p.pid)
}

// deliverEvent is upstream's muf_event_add, plus the immediate-delivery half
// of muf_event_process: Emerald has no periodic scan to defer to, so a
// target already blocked in a matching EVENT_WAITFOR resumes right here
// instead of waiting for one. Otherwise the event is queued on its frame for
// whenever it next blocks on a matching EVENT_WAITFOR — see Frame.popEvent.
func (s *Server) deliverEvent(w *world.World, target *process, name string, data muf.Value) {
	if target.state == procWaiting && (len(target.events) == 0 || matchesEvent(name, target.events)) {
		target.state = procRunnable
		if err := target.frame.Push(data); err != nil {
			s.failProcess(w, target, err)
			return
		}
		if err := target.frame.Push(muf.Str(name)); err != nil {
			s.failProcess(w, target, err)
			return
		}
		s.step(w, target)
		return
	}
	target.frame.AddEvent(name, data)
}

func matchesEvent(name string, filters []string) bool {
	for _, f := range filters {
		if f == name {
			return true
		}
	}
	return false
}

func removePID(pids []int, pid int) []int {
	for i, x := range pids {
		if x == pid {
			return append(pids[:i], pids[i+1:]...)
		}
	}
	return pids
}

// Input from a descriptor goes to a program waiting on a READ, when there is
// one, rather than to the command parser.
func (s *Server) readInput(w *world.World, descr int, line string) bool {
	p := s.procs.readerFor(descr)
	if p == nil {
		return false
	}
	v := muf.Str(line)
	s.resume(w, p, &v)
	return true
}

// killProcessesFor stops everything a player is running, which deleting or
// disconnecting them has to do: a suspended program holds a frame naming an
// object that may be about to change hands.
func (s *Server) killProcessesFor(w *world.World, player ref.Ref) {
	for _, p := range s.procs.all() {
		if p.player == player {
			s.finishProcess(w, p)
		}
	}
}

// killProcessesOf stops every instance of one program.
func (s *Server) killProcessesOf(w *world.World, program ref.Ref) {
	for _, p := range s.procs.forProgram(program) {
		s.finishProcess(w, p)
	}
}
