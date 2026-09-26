package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestResolveMatchesUpstreamsTrie pins the abbreviations whose answer
// is not obvious, each checked by hand against the C before being
// written down. The golden case drives the same words through a real
// Fuzzball; this is the fast version that runs without a container.
func TestResolveMatchesUpstreamsTrie(t *testing.T) {
	const huh = ""

	for _, tc := range []struct {
		typed, want, why string
	}{
		// Upstream's switch requires command[3] == 'n' and
		// then branches on command[4], so neither "@co" nor
		// "@con" reaches anything. Emerald used to answer
		// @conlock for "@co".
		{"@co", huh, "game.c:829 demands command[3] == 'n'"},
		{"@con", huh, "and then a fourth character"},
		{"@conl", "@conlock", ""},
		{"@cont", "@contents", ""},

		// strlen(command) < 7 splits the stem, game.c:795.
		{"@cho", "@chown", "shorter than 7 is the command"},
		{"@chown", "@chown", ""},
		{"@chown_", "@chown_lock", "7 long, so the other branch"},
		{"@fo", "@force", "same split, game.c:1002"},
		{"@force_l", "@force_lock", ""},

		// An exact-match sibling checked ahead of a prefix
		// one.
		{"@cr", "@create", "@credits is strcasecmp, so @cr is not it"},
		{"@credits", "@credits", ""},
		{"@de", "@describe", "@debug is strcasecmp"},
		{"@debug", "@debug", ""},

		// strcmp, so case matters. This looks like a bug and
		// is upstream's, on the commands that end a server.
		{"@toad", "@toad", ""},
		{"@TOAD", huh, "strcmp, game.c:1458"},
		{"@to", huh, "no abbreviation at all"},
		{"@shutdown", "@shutdown", ""},
		{"@SHUTDOWN", huh, "strcmp"},

		// string_prefix("@owned", command) is tried first, so
		// the shared abbreviation goes to @owned, not
		// @ownlock.
		{"@ow", "@owned", "game.c:1212 tries @owned first"},
		{"@ownl", "@ownlock", ""},

		// string_prefix(command, "@unb") has its arguments
		// the other way round: the typed word must be at
		// least that long. So "@un" reaches nothing.
		{"@un", huh, "game.c:1500 needs four characters"},
		{"@unb", "@unbless", ""},
		{"@unli", "@unlink", ""},
		{"@unlo", "@unlock", ""},
		{"@uncom", "@uncompile", ""},
		{"@unl", huh, "ambiguous between @unlink and @unlock"},

		// Bare commands are prefix-matched upstream.
		// Emerald's old table was exact-match, so "e" was Huh
		// here and examine there.
		{"e", "examine", "game.c:1587, Matched(\"examine\")"},
		{"i", "inventory", ""},
		{"l", "look", "look is tried before leave"},
		{"le", "leave", "and leave takes over once look cannot"},
		{"h", "help", "hand is strcasecmp, so it is not hand"},
		{"n", "news", ""},
		{"go", "goto", ""},

		// The one command reached with no Matched() at all,
		// so trailing text is accepted.
		{"move", "move", ""},
		{"movex", "move", "string_prefix(command, \"move\")"},

		// Two-character @-words that reach nothing, because
		// the switch has not committed far enough.
		{"@a", huh, "needs a third character"},
		{"@ac", "@action", ""},
		{"@at", "@attach", ""},

		// Emerald's own, exact-match so they widen nothing.
		{"home", "home", ""},
		{"@help", "@help", ""},
		{"@hel", huh, "exact-match, being an extension"},

		// Dropped with the old resolver: Fuzzball has no such
		// command, and the table is Fuzzball's.
		{"mpihelp", huh, "not a Fuzzball command"},
		{"mpi", "mpi", ""},
	} {
		cmd, ok := resolve(tc.typed)
		got := ""
		if ok {
			got = cmd.n
		}
		if got != tc.want {
			want := tc.want
			if want == "" {
				want = "nothing"
			}
			msg := ""
			if tc.why != "" {
				msg = " (" + tc.why + ")"
			}
			t.Errorf("resolve(%q) = %q, want %s%s",
				tc.typed, got, want, msg)
		}
	}
}

// TestEveryPrefixResolvesToItself is the property that makes the
// table trustworthy: a command's own full name must always reach it,
// whatever the minimums and the ordering do to shorter words.
func TestEveryPrefixResolvesToItself(t *testing.T) {
	for _, c := range commandTable {
		got, ok := resolve(c.n)
		if !ok {
			t.Errorf("%q reaches nothing", c.n)
			continue
		}
		if got.n != c.n {
			t.Errorf("%q reaches %q", c.n, got.n)
		}
	}
}

// TestRegisteredHandlersAreInTheTable guards the other direction:
// register panics on an unknown name, so this only has to check that
// every name the table carries is spelled the way the handlers
// expect.
func TestRegisteredHandlersAreInTheTable(t *testing.T) {
	for name := range handlers {
		if !knownCommand(name) {
			t.Errorf("%q has a handler and no table entry", name)
		}
	}
}

// TestUnimplementedCommandsSaySo checks the choice to keep upstream's
// names in the table without handlers. Dropping them would silently
// widen every abbreviation they constrain, and "Huh?" would be less
// true than saying what is going on.
func TestUnimplementedCommandsSaySo(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Any name in the table with no handler and no entry in
	// `declined` will do — the two say different things, and
	// this is the "not yet" half.
	h.send("@mcpedit")
	got := h.out()
	if !strings.Contains(got, "does not implement") {
		t.Errorf("@mcpedit said:\n%s", got)
	}
	if !strings.Contains(got, "@mcpedit") {
		t.Errorf("the message does not name the command:\n%s", got)
	}
}

// TestGuardsComeFromTheTable checks that the permission macros moved
// to the dispatch site, with upstream's wording.
func TestGuardsComeFromTheTable(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Strip the wizard bit and WIZARDONLY bites, with upstream's
	// wording rather than the "Permission denied." the inline
	// check used to give.
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("@tune muckname")
	if got := h.out(); !strings.Contains(got,
		"You are not allowed to @tune.") {
		t.Errorf("WIZARDONLY said:\n%s", got)
	}

	// And it applies to a command this server does not implement,
	// because upstream would have applied it too.
	h.send("@memory")
	if got := h.out(); !strings.Contains(got,
		"You are not allowed to @memory.") {
		t.Errorf("an unimplemented command skipped its guard:\n%s",
			got)
	}
}

// TestDeclinedCommandsSayWhy separates a decision from a gap. "Not
// yet" is a promise, and these are not going to be implemented.
func TestDeclinedCommandsSayWhy(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@memory")
	got := h.out()
	if !strings.Contains(got, "not available on this server") {
		t.Errorf("@memory said:\n%s", got)
	}
	if strings.Contains(got, "yet") {
		t.Errorf("@memory reads as a promise:\n%s", got)
	}

	// And every declined name is one the table actually carries,
	// so a rename upstream cannot leave a dangling excuse.
	for name := range declined {
		if !knownCommand(name) {
			t.Errorf("%q is declined and not in the table", name)
		}
		if _, ok := handlers[name]; ok {
			t.Errorf("%q is declined and implemented",
				name)
		}
	}
}

// TestMatchControlledWording pins upstream's refusal, which the
// golden harness cannot reach: its player is #1, who controls
// everything in the fixture.
//
// The four commands still on resolveControlled keep the older, bare
// message on purpose — see its doc comment for why each needs its
// own commit rather than a shared string.
func TestMatchControlledWording(t *testing.T) {
	h := newHarness(t)
	h.login()

	var theirs ref.Ref
	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		other := w.Create("Stranger", ref.TypePlayer,
			ref.Nothing)
		other.Owner = other.Ref
		here := w.Get(h.wizRef()).Location
		o := w.Create("locket", ref.TypeThing, other.Ref)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		theirs = o.Ref

		// Stop being a wizard, so control is really tested.
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	_ = theirs

	const upstream = "Permission denied. " +
		"(You don't control what was matched)"

	for _, cmd := range []string{
		"@name locket=brooch",
		"@describe locket=shiny",
		"@set locket=D",
		"@lock locket=me",
		"@unlock locket",
	} {
		h.send(cmd)
		if got := h.out(); !strings.Contains(got, upstream) {
			t.Errorf("%q said:\n%s", cmd, got)
		}
	}

	// And the four that are knowingly still divergent.
	for _, cmd := range []string{
		"@unlink locket",
		"@recycle locket",
	} {
		h.send(cmd)
		got := h.out()
		if !strings.Contains(got, "Permission denied.") {
			t.Errorf("%q said:\n%s", cmd, got)
		}
		if strings.Contains(got, "what was matched") {
			t.Errorf("%q claims match_controlled:\n%s",
				cmd, got)
		}
	}
}
