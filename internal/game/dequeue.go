package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `dequeue_prog(player, 2)` (`timequeue.c:1455`), which is "kill only
// foreground" and is reached from exactly two places: `@Q`
// (`interface.c:1845`) and a **disconnect** (`interface.c:2339`). The
// second had no port at all, so a player who dropped their connection
// left their foreground programs running, and
// `mpi_continue_after_logout` — the parameter that exists to decide
// what *survives* that sweep — had no reader.

// abortForegroundFor is the sweep. It reports whether anything was
// killed, which is what decides the "Foreground program aborted."
// line.
//
// Two of its rules are upstream's and easy to get backwards. A
// **background** process is spared, because killmode 2 is about the
// program somebody is sitting in front of. And a queued **MPI** event
// is spared only when `mpi_continue_after_logout` is set, which it is
// not by default — so an MPI `{delay}` normally dies with the
// connection.
func (s *Server) abortForegroundFor(w *world.World,
	player ref.Ref) bool {

	killed := false
	for _, p := range s.procs.all() {
		if p.player != player {
			continue
		}
		if p.frame != nil &&
			p.frame.Mode == muf.ModeBackground {
			continue
		}
		s.finishProcess(w, p)
		killed = true
	}
	if w.Tune.Bool("mpi_continue_after_logout") {
		return killed
	}
	kept := s.mpiEvents[:0]
	for _, e := range s.mpiEvents {
		if e.player == player {
			killed = true
			continue
		}
		kept = append(kept, e)
	}
	s.mpiEvents = kept
	return killed
}
