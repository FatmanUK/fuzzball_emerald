package world

import (
	"strconv"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Gripe is one complaint, upstream's line in the file named by
// file_log_gripes.
//
// Upstream writes it to a log file and reads the whole file back when
// a wizard types "gripe" with no message. Emerald has no game
// directory, so it goes where the help texts went: rows in Postgres,
// loaded at boot and written behind like everything else. That keeps
// the reading in memory — nothing in this server reads the database
// after boot — and gives the configurator something to show.
type Gripe struct {
	// ID is assigned by the store, and is zero for a gripe made
	// this session and not yet written.
	ID int64
	// When is Unix seconds, the timestamp upstream's log line
	// carries.
	When int64
	// Who and Where are recorded as refs *and* as the names they
	// had at the time, because upstream's log line is text: a
	// player renamed or recycled afterwards would otherwise
	// rewrite history.
	Who       ref.Ref
	WhoName   string
	Where     ref.Ref
	WhereName string
	Message   string
}

// GripeLimit is how many gripes are kept in memory, newest last.
//
// Upstream's log file grows without bound and a wizard reading it
// gets the lot; a world that has been up for years would flood
// somebody's client. The rows are all still in the database for the
// configurator to page through — this bounds only what "gripe"
// shows.
const GripeLimit = 200

// Gripes returns the complaints on record, oldest first.
func (w *World) Gripes() []Gripe {
	out := make([]Gripe, len(w.gripes))
	copy(out, w.gripes)
	return out
}

// SetGripes replaces the list without marking anything for writing,
// which is what loading from the store wants.
func (w *World) SetGripes(list []Gripe) {
	if len(list) > GripeLimit {
		list = list[len(list)-GripeLimit:]
	}
	w.gripes = append(w.gripes[:0], list...)
}

// AddGripe records a complaint and marks it for writing.
func (w *World) AddGripe(g Gripe) {
	if g.When == 0 {
		g.When = w.Now().Unix()
	}
	w.gripes = append(w.gripes, g)
	w.newGripes = append(w.newGripes, g)
	if len(w.gripes) > GripeLimit {
		w.gripes = w.gripes[len(w.gripes)-GripeLimit:]
	}
}

// GripeLine renders a gripe the way upstream's log file holds it, so
// a wizard reading them back sees what they would have read in the
// file.
//
// The dbrefs carry no '#'. That is not a slip: log_gripe's format is
// "GRIPE from %s(%d) in %s(%d): %s", which is how every log line
// upstream writes spells a ref, and the golden case compares it.
func (g Gripe) GripeLine() string {
	when := time.Unix(g.When, 0).Format("2006-01-02T15:04:05")
	return when + ": GRIPE from " + g.WhoName + "(" +
		strconv.Itoa(int(g.Who)) + ") in " + g.WhereName + "(" +
		strconv.Itoa(int(g.Where)) + "): " + g.Message
}
