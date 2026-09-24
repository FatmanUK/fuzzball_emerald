package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/help"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// seedHelp fills the harness world's corpora from the built-in
// content, which is what the serve path does at boot.
func (h *harness) seedHelp() {
	h.t.Helper()
	err := h.engine.Do(context.Background(), func(w *world.World) {
		help.Apply(w, "", false)
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

func TestHelpShowsTheHeaderAndTopics(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("help")
	if got := h.out(); !strings.Contains(got, "player command help") {
		t.Errorf("bare help did not show the header:\n%s", got)
	}

	h.send("help examine")
	got := h.out()
	if !strings.Contains(got, "examine <thing>") {
		t.Errorf("help examine:\n%s", got)
	}

	// "ex" is an alias of the same block, so it must find it.
	h.send("help ex")
	if alias := h.out(); alias != got {
		t.Errorf("help ex differed from help examine:\n%s", alias)
	}
}

// TestHelpMissUsesUpstreamWording pins the message, which is what a
// player and any program wrapping help both read.
func TestHelpMissUsesUpstreamWording(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("help nosuchtopic")
	want := `Sorry, no help available on topic "nosuchtopic"`
	if got := h.out(); !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant it to contain %q", got, want)
	}
}

// TestHelpIsNotPrefixMatched separates help from info: an index
// corpus matches a topic exactly, and only info prefix-matches.
func TestHelpIsNotPrefixMatched(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("help exam")
	if got := h.out(); !strings.Contains(got, "no help available") {
		t.Errorf("help should not prefix-match:\n%s", got)
	}
}

// TestInfoSegment covers the one corpus whose segment is honoured.
func TestInfoSegment(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("info newbie")
	full := strings.Split(h.out(), "\n")
	if len(full) < 3 {
		t.Fatalf("info newbie is too short to slice: %q", full)
	}

	h.send("info newbie=2")
	if got := h.out(); got != full[1] {
		t.Errorf("segment 2 = %q, want %q", got, full[1])
	}

	h.send("info newbie=1-2")
	if got := h.out(); got != strings.Join(full[:2], "\n") {
		t.Errorf("segment 1-2 = %q", got)
	}
}

// TestHelpIgnoresItsSegment pins what looks like a bug and is not:
// upstream's index_file never sees the segment, so only the
// directory-backed "info" can cut a text down.
func TestHelpIgnoresItsSegment(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("help examine")
	full := h.out()
	h.send("help examine=2")
	if got := h.out(); got != full {
		t.Errorf("a segment changed an index corpus:\n%s", got)
	}
}

func TestInfoListsAndPrefixMatches(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("info")
	got := h.out()
	if !strings.Contains(got, "Available information files are:") {
		t.Errorf("bare info did not list:\n%s", got)
	}
	if !strings.Contains(got, "server") {
		t.Errorf("the listing is missing a topic:\n%s", got)
	}

	// Upstream's info directory matches a partial file name.
	h.send("info newb")
	if got := h.out(); !strings.Contains(got, "If you have just arrived") {
		t.Errorf("info did not prefix-match:\n%s", got)
	}

	h.send("info nosuchfile")
	if got := h.out(); !strings.Contains(got, noInfoMsg) {
		t.Errorf("info miss:\n%s", got)
	}
}

// TestInfoListingIsInColumns pins the layout, which upstream builds
// by padding each name to the next twenty-column boundary from four.
func TestInfoListingIsInColumns(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("info")
	for _, line := range strings.Split(h.out(), "\n") {
		if !strings.HasPrefix(line, "    ") {
			continue
		}
		if len(line)%20 != 4 {
			t.Errorf("listing line is %d columns: %q",
				len(line), line)
		}
	}
}

func TestMOTDAppendAndClear(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("motd The hall is closed for repairs.")
	if got := h.out(); !strings.Contains(got, "MOTD updated.") {
		t.Fatalf("motd update:\n%s", got)
	}

	h.send("motd")
	got := h.out()
	if !strings.Contains(got, "The hall is closed for repairs.") {
		t.Errorf("the motd did not keep the text:\n%s", got)
	}
	if !strings.Contains(got, motdRule) {
		t.Errorf("the motd is missing its rule:\n%s", got)
	}

	h.send("motd clear")
	if got := h.out(); !strings.Contains(got, "MOTD cleared.") {
		t.Fatalf("motd clear:\n%s", got)
	}
	h.send("motd")
	if got := h.out(); strings.Contains(got, "repairs") {
		t.Errorf("clearing left the old text:\n%s", got)
	}
}

// TestMOTDIsShownOnConnect pins upstream's interface.c behaviour: the
// motd arrives with the connection, not only when asked for.
func TestMOTDIsShownOnConnect(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetHelpText(world.CorpusMOTD, "Mind the step.")
	}); err != nil {
		t.Fatal(err)
	}

	h.send("connect Wizard secret")
	if got := h.out(); !strings.Contains(got, "Mind the step.") {
		t.Errorf("connecting did not show the motd:\n%s", got)
	}
}

// TestMOTDIsReadOnlyForMortals checks that a non-wizard typing text
// reads the motd instead of appending to it.
func TestMOTDIsReadOnlyForMortals(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetHelpText(world.CorpusMOTD, "Quiet, please.")
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("motd Everybody shout.")
	got := h.out()
	if strings.Contains(got, "MOTD updated.") {
		t.Errorf("a mortal changed the motd:\n%s", got)
	}
	if !strings.Contains(got, "Quiet, please.") {
		t.Errorf("a mortal was not shown the motd:\n%s", got)
	}
}

func TestCreditsIsExactAndDoesNotShadowCreate(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("@credits")
	if got := h.out(); !strings.Contains(got, "Fuzzball Emerald") {
		t.Errorf("@credits:\n%s", got)
	}

	// The reason @credits is exact-only: without that, "@cre"
	// becomes ambiguous and lookupAtCommand answers nothing, so
	// abbreviating @create would stop working.
	h.send("@cre widget")
	if got := h.out(); !strings.Contains(got, "created.") {
		t.Errorf("@cre no longer reaches @create:\n%s", got)
	}
}

func TestPreLoginHelpIsTheConnectionText(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.out()

	h.send("help")
	got := h.out()
	if !strings.Contains(got, "connect <name> <password>") {
		t.Errorf("pre-login help:\n%s", got)
	}
	// The banner is what they have already read; the point of
	// this text is that it is a different one.
	if strings.Contains(got, "Fuzzball Emerald") {
		t.Errorf("pre-login help re-sent the banner:\n%s", got)
	}
}

// TestWelcomeComesFromTheProplist checks the first of the banner's
// three sources, which is the one MUF can write.
func TestWelcomeComesFromTheProplist(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		root := w.Get(ref.Ref(0))
		if root == nil {
			t.Skip("this world has no #0")
		}
		root.Props.Set("welcome#", props.Value{
			Type: props.Int, Num: 2})
		root.Props.SetString("welcome#/1", "The Hollow Tree")
		root.Props.SetString("welcome#/2", "connect or create")
	}); err != nil {
		t.Fatal(err)
	}

	d2, err := h.s.Connect(h.d.Transport, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	h.sync()

	got := drainDescriptor(d2)
	if !strings.Contains(got, "The Hollow Tree") {
		t.Errorf("the banner ignored the proplist:\n%s", got)
	}
	if strings.Contains(got, "Fuzzball Emerald") {
		t.Errorf("the banner used the corpus as well:\n%s", got)
	}
}

// TestWelcomeFallsBackToTheCorpus checks the second source, which is
// where upstream reads file_welcome_screen.
func TestWelcomeFallsBackToTheCorpus(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetHelpText(world.CorpusWelcome, "Welcome to the Vault.")
	}); err != nil {
		t.Fatal(err)
	}

	d2, err := h.s.Connect(h.d.Transport, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	h.sync()

	if got := drainDescriptor(d2); !strings.Contains(got,
		"Welcome to the Vault.") {
		t.Errorf("the banner ignored the corpus:\n%s", got)
	}
}

// TestWelcomeFallsBackToTheDefault covers a world with neither, which
// is what an imported dump and a fresh database both are until
// somebody writes one.
func TestWelcomeFallsBackToTheDefault(t *testing.T) {
	h := newHarness(t)

	d2, err := h.s.Connect(h.d.Transport, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	h.sync()

	if got := drainDescriptor(d2); !strings.Contains(got,
		"Fuzzball Emerald") {
		t.Errorf("the banner has no fallback:\n%s", got)
	}
}

func TestEditHelpTopic(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()

	h.send("@help #set news welcome=We have opened the east wing.")
	if got := h.out(); !strings.Contains(got, "Topic saved.") {
		t.Fatalf("@help #set:\n%s", got)
	}
	h.send("news welcome")
	if got := h.out(); !strings.Contains(got, "east wing") {
		t.Errorf("the edited topic did not come back:\n%s", got)
	}

	h.send("@help #alias news welcome=eastwing|east")
	h.out()
	h.send("news east")
	if got := h.out(); !strings.Contains(got, "east wing") {
		t.Errorf("the alias does not resolve:\n%s", got)
	}

	h.send("@help #del news welcome")
	if got := h.out(); !strings.Contains(got, "Topic deleted.") {
		t.Fatalf("@help #del:\n%s", got)
	}
	h.send("news welcome")
	if got := h.out(); !strings.Contains(got, "no help available") {
		t.Errorf("the deleted topic is still there:\n%s", got)
	}
}

func TestEditHelpNeedsAWizard(t *testing.T) {
	h := newHarness(t)
	h.seedHelp()
	h.login()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.Get(h.wizRef()).Flags &^= ref.Wizard
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	// The refusal is the dispatch table's WIZARDONLY, worded as
	// upstream's macro words it rather than in @help's own voice.
	h.send("@help #set news welcome=mine now")
	if got := h.out(); !strings.Contains(got,
		"You are not allowed to @help.") {
		t.Errorf("a mortal edited the help:\n%s", got)
	}
}

// TestBlankLinesBecomeTwoSpaces pins the one formatting rule the
// whole of help.c shares. A client that trims trailing whitespace
// sees a blank line either way; one that collapses empty output does
// not, which is why upstream does this.
func TestBlankLinesBecomeTwoSpaces(t *testing.T) {
	h := newHarness(t)
	h.login()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetHelp(world.CorpusNews, []world.HelpTopic{{
			Corpus: world.CorpusNews,
			Body:   "one\n\ntwo",
		}})
	}); err != nil {
		t.Fatal(err)
	}

	h.send("news")
	want := "one\n  \ntwo"
	if got := h.out(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapMOTD(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for _, line := range strings.Split(wrapMOTD(long), "\n") {
		if line == "" {
			continue
		}
		if len(line) > 76 {
			t.Errorf("line is %d columns: %q", len(line), line)
		}
	}
	if got := wrapMOTD("short"); got != "    short\n" {
		t.Errorf("wrapMOTD(short) = %q", got)
	}
}

func TestParseSegment(t *testing.T) {
	for _, tc := range []struct {
		in         string
		start, end int
	}{
		{"", 0, 0},
		{"5", 5, 5},
		{"3-7", 3, 7},
		{"3-", 3, 0},
		{"-7", 0, 7},
		{"rubbish", 0, 0},
	} {
		start, end := parseSegment(tc.in)
		if start != tc.start || end != tc.end {
			t.Errorf("parseSegment(%q) = %d,%d want %d,%d",
				tc.in, start, end, tc.start, tc.end)
		}
	}
}
