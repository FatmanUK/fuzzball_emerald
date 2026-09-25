package muf

// Perms is upstream's fr->perms: which identity a running program
// acts with, chosen by whoever starts it.
//
// It is an argument to interp() upstream, not something the program
// can set, and it is half of what find_uid reads — the other half
// being the program's own STICKY and HAVEN flags. See Frame.ProgUID.
type Perms uint8

const (
	// RegUID is the ordinary case: the program acts as whoever
	// triggered it, subject to its mucker level. An exit that
	// runs a program gets this, and so does the zero value, which
	// is deliberate — a launch site that says nothing is saying
	// this.
	RegUID Perms = iota
	// SetUID makes the program act as its own owner. Upstream has
	// no caller that passes it: the STICKY flag is the only way
	// to reach that branch, since "setuid" is an alias for STICKY
	// in the flag table. It exists here because find_uid tests
	// for it and leaving it out would make that test unreadable.
	SetUID
	// HardUID makes the program act as the owner of whatever
	// triggered it. This is the one that matters: upstream passes
	// it for a lock, for a property that runs a program, for
	// MPI's {muf}, and for anything the timequeue fires — which
	// is most of the ways a program starts other than a command.
	HardUID
)
