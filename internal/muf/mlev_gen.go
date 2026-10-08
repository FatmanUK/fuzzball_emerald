// Code generated from the mlev checks in Fuzzball 7's p_*.c.
// DO NOT EDIT. Regenerate with: go generate ./internal/muf

package muf

// primMLevel is the mucker level a primitive requires.
//
// Upstream guards privileged primitives with "if (mlev < N)" inside
// each implementation. Without these a program at mucker level 1 could
// read passwords, change ownership and boot connections, so this is a
// security surface rather than only a compatibility one.
//
// Only an *unconditional* floor is recorded. A check written
// "(mlev < 4) && !permissions(...)" means "a wizard or the
// owner", and so does one nested inside a type or flag test, or
// one whose branch picks a message rather than refusing; a
// finer-grained gate would need the arguments, which the
// dispatcher does not have.
//
// One entry is not from the C at all: see HELD_FLOOR in
// gen_mlev.py, which keeps MOVETO gated because this server's
// implementation of it is not faithful enough to ungate.
//
// Where a primitive has several, the *loosest* is recorded: that
// is the level at which it is definitely callable, and refusing
// at the strictest would block a path that works before the
// primitive could check for itself.
var primMLevel = map[string]int{
	"ARRAY_GET_IGNORELIST": 3,
	"ARRAY_GET_PROPDIRS":   3,
	"ARRAY_GET_PROPVALS":   3,
	"BLESSED?":             2,
	"CHECKPASSWORD":        4,
	"COMPILE":              4,
	"COPYPLAYER":           4,
	"DUMP":                 4,
	"ENTRANCES_ARRAY":      3,
	"FORK":                 3,
	"MOVETO":               3,
	"NEWEXIT":              3,
	"NEWPASSWORD":          4,
	"NEWPLAYER":            4,
	"NEWPROGRAM":           4,
	"NEXTOWNED":            2,
	"NEXTPROP":             3,
	"NOTIFY_SECURE":        3,
	"PARSEMPI":             3,
	"PART_PMATCH":          3,
	"PNAME_HISTORY":        4,
	"PROGRAM_SETLINES":     4,
	"PROPDIR?":             2,
	"QUEUE":                3,
	"RECYCLE":              3,
	"TOADPLAYER":           4,
	"UNCOMPILE":            4,
}
