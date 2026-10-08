package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// cmdInit writes a minimal world of this server's own.
//
// Until this existed, the only way to get a bootable world was
// "fbemerald import" over a Fuzzball dump — and the only dump in
// the tree is a byte-identical copy of upstream's. Reading a foreign
// format is a feature; being unable to stand up without one is a
// dependency, and this is the command that removes it.
//
// There is deliberately no -password flag. A password on the command
// line lands in "ps" output and in shell history, so the only way to
// supply one is -password-stdin, which also makes the command
// scriptable without a terminal.
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: fbemerald init [flags]

Writes a minimal world: one room and one wizard who owns it. Use this
for a fresh install; use "fbemerald import" to load a Fuzzball dump
instead.

The password is read from stdin rather than taken as a flag, because a
flag would put it in "ps" output and shell history:

    printf 'secret\n' | fbemerald init -password-stdin

`)
		fs.PrintDefaults()
	}
	wizName := fs.String("wizard", "Wizard",
		"name of the first wizard")
	roomName := fs.String("room", "Nexus",
		"name of the first room")
	fromStdin := fs.Bool("password-stdin", false,
		"read the wizard's password from stdin's first line")
	force := fs.Bool("force", false,
		"replace an existing world instead of refusing")

	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("init takes no arguments")
	}
	if !*fromStdin {
		fs.Usage()
		return fmt.Errorf("a password is required: pass " +
			"-password-stdin and send it on stdin")
	}

	pw, err := readPasswordLine()
	if err != nil {
		return err
	}

	base, err := newLogger(c)
	if err != nil {
		return err
	}
	log := logging.On(base, logging.Status)

	ctx, stop := notifyContext()
	defer stop()

	// Build the world before opening the database, so a bad name
	// or an unhashable password cannot leave a half-written one.
	w, err := minimalWorld(*roomName, *wizName, pw)
	if err != nil {
		return err
	}

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}
	empty, err := st.IsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty && !*force {
		return fmt.Errorf("the database already holds a " +
			"world; pass -force to replace it")
	}
	if !empty {
		log.Warn("replacing the existing world")
		if err := st.Reset(ctx); err != nil {
			return err
		}
	}

	started := time.Now()
	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		return fmt.Errorf("writing the world: %w", err)
	}

	log.Info("world created",
		"room", *roomName,
		"wizard", *wizName,
		"took", time.Since(started).String(),
	)
	log.Info("the help texts are seeded on the first serve; " +
		"run \"fbemerald serve\" next")
	return nil
}

// readPasswordLine takes the first line of stdin.
func readPasswordLine() (string, error) {
	return readPasswordFrom(os.Stdin)
}

// readPasswordFrom takes the first line of r.
//
// Only the line ending is stripped, and nothing else: a password may
// legitimately begin or end with a space, and silently trimming one
// would lock somebody out of their own world. A read error with data
// already in hand is end-of-input, which is what a password sent
// without a trailing newline looks like.
func readPasswordFrom(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		if err != nil {
			return "", fmt.Errorf("no password on "+
				"stdin: %w", err)
		}
		return "", fmt.Errorf("the password on stdin " +
			"was empty")
	}
	return line, nil
}

// minimalWorld is the smallest world this server can serve: a room
// and a wizard who owns it.
//
// The shape is upstream's minimal database rather than an invention
// — #0 a room owned by #1, #1 a self-owned wizard standing in it
// — because the @tune defaults expect it: player_start,
// default_room_parent and lost_and_found all default to #0, so #0 has
// to exist and has to be a room.
//
// The wizard gets BUILDER and mucker 3, not mucker 4. A wizard with
// **no** mucker bits has level 0, which would cap every program they
// own at 0 (find_mlev takes the lower of the two) — so leaving the
// bits off would quietly make the world unable to run MUF. Level 4 is
// Wizard plus any mucker bit, which is what @set gives for "3" on top
// of the Wizard flag; 3 here is the raw level, and the Wizard flag
// lifts it where it matters.
func minimalWorld(roomName, wizName, pw string) (*world.World,
	error) {

	if err := checkWorldName(roomName); err != nil {
		return nil, fmt.Errorf("room name: %w", err)
	}
	if err := checkWorldName(wizName); err != nil {
		return nil, fmt.Errorf("wizard name: %w", err)
	}

	w := world.New()

	// #0 first, because every dbref default points at it.
	room := w.Create(roomName, ref.TypeRoom, ref.God)
	if room.Ref != ref.GlobalEnvironment {
		return nil, fmt.Errorf("the room came out as "+
			"%v, not #0", room.Ref)
	}
	w.SetProp(room.Ref, "_/de", props.Value{
		Type: props.String,
		Str: "A quiet place, and the first one. " +
			"Describe it with \"@describe here=...\".",
	})

	wiz := w.Create(wizName, ref.TypePlayer, ref.Nothing)
	if wiz.Ref != ref.God {
		return nil, fmt.Errorf("the wizard came out as %v, "+
			"not #1", wiz.Ref)
	}
	wiz.Owner = wiz.Ref
	wiz.Flags |= ref.Wizard | ref.Builder
	wiz.Flags = wiz.Flags.SetMLevel(3)
	wiz.Home = room.Ref

	hashed, err := password.Hash(pw)
	if err != nil {
		return nil, err
	}
	wiz.PasswordHash = hashed

	// No name registration is needed: World.Create indexes a
	// player by name as it makes one.
	if err := w.MoveTo(wiz.Ref, room.Ref); err != nil {
		return nil, err
	}
	return w, nil
}

// checkWorldName applies the same rule the game applies to anything a
// player creates, so a world cannot be initialised with a name no
// command could then refer to.
func checkWorldName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("must not be blank")
	}
	if !game.NameOK(name) {
		return fmt.Errorf("%q is not a usable name", name)
	}
	return nil
}
