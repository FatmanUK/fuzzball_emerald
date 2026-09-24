// Package golden runs the same session against this server and
// against a Fuzzball 7 built from the C sources, and compares what
// each says.
//
// It exists because porting 400 primitives by reading C is guesswork
// without an oracle. A case here states a MUF snippet; the harness
// builds a database holding it, drives both servers through the same
// script, and diffs the transcripts.
package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Fixture is a database both servers can load.
//
// Fuzzball keeps program source in muf/<dbref>.m beside the dump
// rather than inside it, so a fixture is a directory rather than a
// file.
type Fixture struct {
	Dir      string
	DumpPath string
	MufDir   string
}

// object is one record in the dump under construction.
type object struct {
	ref      int
	name     string
	location int
	contents int
	next     int
	flags    uint32
	props    []string
	// tail is the type-specific part, already rendered.
	tail []string
}

// Object types and the flag bits the fixtures need, from
// include/db.h.
const (
	typeRoom    = 0x0
	typeThing   = 0x1
	typeExit    = 0x2
	typePlayer  = 0x3
	typeProgram = 0x4

	flagWizard = 0x10
	flagLinkOK = 0x20
	flagMucker = 0x40000
	flagSMuck  = 0x100000
	flagHaven  = 0x10000
)

// The password Fuzzball's own minimal database uses for #1, which its
// README documents. Using it keeps the fixture and the oracle's
// default in step.
const godPassword = "potrzebie"

// md5OfGodPassword is the base64 MD5 of that password, the form a
// dump stores.
const md5OfGodPassword = "CuG4ZtGvyRbfJubgNISTcg=="

// WriteFixture builds a database holding one MUF program, an exit
// that runs it, a room and a wizard player.
func WriteFixture(dir, source string) (*Fixture, error) {
	return WriteMultiFixture(dir, []Program{{Name: "test", Source: source}})
}

// Program is one test program in a fixture, reachable through an exit
// of the same name.
type Program struct {
	Name   string
	Source string
}

// WriteMultiFixture builds a database holding several programs, each
// with its own exit.
//
// Several rather than one because starting a container costs about
// two seconds: putting every case in one database turns a per-case
// cost into a per-run one.
//
// The layout mirrors the minimal database Fuzzball ships: #0 is the
// room, #1 is the wizard, and the programs and their exits follow in
// pairs.
func WriteMultiFixture(dir string, programs []Program) (*Fixture, error) {
	const (
		room = 0
		god  = 1
	)

	mufDir := filepath.Join(dir, "muf")
	dataDir := filepath.Join(dir, "data")
	for _, d := range []string{mufDir, dataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	if len(programs) == 0 {
		return nil, fmt.Errorf("a fixture needs at least one program")
	}

	// Programs go in the wizard's inventory and exits on the
	// room, each chain threaded through the next field.
	firstProg := 2
	firstExit := firstProg + len(programs)

	objs := []object{
		{
			ref: room, name: "Room Zero",
			location: -1, contents: god, next: -1,
			flags: typeRoom,
			props: []string{"_/de:2:A featureless test room."},
			// drop-to, exits, owner
			tail: []string{"-1", itoa(firstExit), itoa(god)},
		},
		{
			ref: god, name: "One",
			location: room, contents: firstProg, next: -1,
			// A wizard at mucker level 3, so the programs
			// may do whatever they like.
			flags: typePlayer | flagWizard | flagMucker | flagSMuck,
			props: []string{"_/de:2:The test wizard."},
			// home, exits, password
			tail: []string{itoa(room), "-1", md5OfGodPassword},
		},
	}

	for i, prog := range programs {
		progRef := firstProg + i
		exitRef := firstExit + i

		nextProg := -1
		if i+1 < len(programs) {
			nextProg = progRef + 1
		}
		nextExit := -1
		if i+1 < len(programs) {
			nextExit = exitRef + 1
		}

		objs = append(objs,
			object{
				ref:      progRef,
				name:     prog.Name + ".muf",
				location: god,
				contents: -1,
				next:     nextProg,
				flags:    typeProgram | flagMucker | flagSMuck | flagLinkOK,
				tail:     []string{itoa(god)},
			},
			object{
				ref: exitRef, name: prog.Name,
				location: room,
				contents: -1,
				next:     nextExit,
				flags:    typeExit,
				// destination count, destinations,
				// owner
				tail: []string{"1", itoa(progRef), itoa(god)},
			})
	}

	dump := filepath.Join(dataDir, "test.db")
	if err := writeDump(dump, objs); err != nil {
		return nil, err
	}
	for i, prog := range programs {
		srcPath := filepath.Join(mufDir, fmt.Sprintf("%d.m", firstProg+i))
		if err := os.WriteFile(srcPath, []byte(prog.Source), 0o644); err != nil {
			return nil, err
		}
	}
	// An empty macro table, so neither server falls back to a
	// stale one.
	if err := os.WriteFile(filepath.Join(mufDir, "macros"), nil, 0o644); err != nil {
		return nil, err
	}

	return &Fixture{Dir: dir, DumpPath: dump, MufDir: mufDir}, nil
}

// itoa renders a dbref for the dump.
func itoa(n int) string { return strconv.Itoa(n) }

// writeDump renders objects in the Foxen9 format both servers read.
//
// Objects are written highest ref first, as db_write does. The
// parameter block is empty, which leaves every @tune setting at its
// default.
func writeDump(path string, objs []object) error {
	var b strings.Builder

	b.WriteString("***Foxen9 TinyMUCK DUMP Format***\n")
	// The object count, an ignored flags word, and the number of
	// parameters.
	fmt.Fprintf(&b, "%d\n0\n0\n", len(objs))

	for i := len(objs) - 1; i >= 0; i-- {
		o := objs[i]
		fmt.Fprintf(&b, "#%d\n", o.ref)
		b.WriteString(o.name + "\n")
		fmt.Fprintf(&b, "%d\n%d\n%d\n%d\n", o.location, o.contents, o.next, o.flags)
		// Created, last used, use count, modified. Fixed
		// values keep the two servers' output identical.
		b.WriteString("1000000000\n1000000000\n0\n1000000000\n")

		if len(o.props) > 0 {
			b.WriteString("*Props*\n")
			for _, p := range o.props {
				b.WriteString(p + "\n")
			}
			b.WriteString("*End*\n")
		} else {
			// With no property block the type-specific
			// fields follow the timestamps directly, and
			// the first of them takes the slot the block
			// would have used.
			b.WriteString(o.tail[0] + "\n")
			for _, t := range o.tail[1:] {
				b.WriteString(t + "\n")
			}
			continue
		}
		for _, t := range o.tail {
			b.WriteString(t + "\n")
		}
	}

	b.WriteString("***END OF DUMP***\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
