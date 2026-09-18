package game

import (
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
			s.procs.remove(p.pid)
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
		if p.descr != 0 {
			s.procs.reading[p.descr] = p.pid
		}

	case muf.BlockSleep:
		p.state = procSleeping
		p.wake = w.Now().Add(time.Duration(b.Seconds) * time.Second)

	case muf.BlockEvent:
		p.state = procWaiting
		p.events = b.Events

	default:
		// No reason given, which should not happen; treat it as done
		// rather than leaving a process that nothing will ever resume.
		s.procs.remove(p.pid)
	}
}

// failProcess reports a runtime error and removes the process.
func (s *Server) failProcess(w *world.World, p *process, err error) {
	s.procs.remove(p.pid)
	s.reportMUFErrorTo(w, p.player, p.frame, p.program, err)
}

// Input from a descriptor goes to a program waiting on a READ, when there is
// one, rather than to the command parser.
func (s *Server) readInput(w *world.World, descr int, line string) bool {
	p := s.procs.readerFor(descr)
	if p == nil {
		return false
	}
	// A lone "@Q" breaks out of a READ, which is how a player escapes a
	// program that is waiting on them.
	if line == breakCommand {
		s.procs.remove(p.pid)
		s.send(p.player, "Program aborted.")
		return true
	}
	v := muf.Str(line)
	s.resume(w, p, &v)
	return true
}
