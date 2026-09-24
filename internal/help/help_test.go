package help

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

const sample = `the header
~
look|l|read
look at something
~
go
leave through an exit
`

func TestParseIndex(t *testing.T) {
	got := ParseIndex("help", sample, true)
	if len(got) != 3 {
		t.Fatalf("parsed %d topics: %+v", len(got), got)
	}
	if got[0].Name != "" || got[0].Body != "the header" {
		t.Errorf("header = %+v", got[0])
	}
	if got[1].Name != "look" ||
		strings.Join(got[1].Aliases, ",") != "l,read" {
		t.Errorf("aliases = %+v", got[1])
	}
	if got[2].Body != "leave through an exit" {
		t.Errorf("trailing block = %q", got[2].Body)
	}
	for i, tp := range got {
		if tp.Ord != i {
			t.Errorf("topic %d has Ord %d", i, tp.Ord)
		}
	}
}

// TestParseIndexWithoutHeader is how the info corpus is read: the
// text before the first separator is not a topic, so a leading
// comment does not become a nameless entry nothing can reach.
func TestParseIndexWithoutHeader(t *testing.T) {
	got := ParseIndex("info", sample, false)
	if len(got) != 2 {
		t.Fatalf("parsed %d topics: %+v", len(got), got)
	}
	if got[0].Name != "look" {
		t.Errorf("first topic = %+v", got[0])
	}
}

func TestRenderIndexRoundTrips(t *testing.T) {
	first := ParseIndex("help", sample, true)
	second := ParseIndex("help", RenderIndex(first), true)
	if len(first) != len(second) {
		t.Fatalf("round trip changed the count: %d then %d",
			len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name ||
			first[i].Body != second[i].Body ||
			strings.Join(first[i].Aliases, "|") !=
				strings.Join(second[i].Aliases, "|") {
			t.Errorf("topic %d changed:\n%+v\n%+v",
				i, first[i], second[i])
		}
	}
}

// TestSeedParses is a guard on the embedded content itself: a
// malformed file would otherwise only show up as a corpus that is
// quietly short at boot.
func TestSeedParses(t *testing.T) {
	for _, corpus := range world.Corpora() {
		got, err := Seed(corpus)
		if err != nil {
			t.Fatalf("%s: %v", corpus, err)
		}
		if len(got) == 0 {
			t.Errorf("%s seeds nothing", corpus)
		}
		for _, tp := range got {
			if strings.TrimSpace(tp.Body) == "" {
				t.Errorf("%s/%q has an empty body",
					corpus, tp.Name)
			}
			if tp.Modified != 0 {
				t.Errorf("%s/%q is stamped as edited",
					corpus, tp.Name)
			}
		}
	}
}

// TestSeedInfoHasNoHeader checks the one corpus whose first block is
// deliberately discarded, since "info" lists its topics rather than
// showing a header.
func TestSeedInfoHasNoHeader(t *testing.T) {
	got, err := Seed(world.CorpusInfo)
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range got {
		if tp.Name == "" {
			t.Errorf("info has a nameless topic: %q", tp.Body)
		}
	}
}

func TestApplyFillsAnEmptyWorld(t *testing.T) {
	w := world.New()
	changed := Apply(w, "", false)
	if len(changed) == 0 {
		t.Fatal("seeding an empty world changed nothing")
	}
	if _, ok := w.LookupHelp(world.CorpusHelp, "look", false); !ok {
		t.Error("the help corpus was not filled")
	}
	// Seeding twice at the same version must be a no-op, or every
	// boot would rewrite every corpus.
	if again := Apply(w, Version, false); len(again) != 0 {
		t.Errorf("reseeding changed %v", again)
	}
}

// TestApplyKeepsEditedTopics is the point of the Modified stamp: a
// new release refreshes what nobody has touched and leaves the rest
// exactly as the wizard left it.
func TestApplyKeepsEditedTopics(t *testing.T) {
	w := world.New()
	Apply(w, "", false)

	w.SetHelpTopic(world.HelpTopic{Corpus: world.CorpusHelp,
		Name: "look", Body: "our own version"})
	w.SetHelpTopic(world.HelpTopic{Corpus: world.CorpusHelp,
		Name: "teapot", Body: "local addition"})

	// Delete a shipped topic, so the merge has something real to
	// put back and the corpus genuinely differs.
	w.DeleteHelpTopic(world.CorpusHelp, "drop")

	// Pretend the built-in content moved on.
	if changed := Apply(w, "0", false); len(changed) == 0 {
		t.Fatal("a version change seeded nothing")
	}
	if _, ok := w.LookupHelp(world.CorpusHelp, "drop", false); !ok {
		t.Error("the merge did not restore a shipped topic")
	}

	got, _ := w.LookupHelp(world.CorpusHelp, "look", false)
	if got.Body != "our own version" {
		t.Errorf("an edited topic was overwritten: %q", got.Body)
	}
	if got, ok := w.LookupHelp(world.CorpusHelp, "teapot", false); !ok ||
		got.Body != "local addition" {
		t.Errorf("a local topic was dropped: %+v ok=%v", got, ok)
	}
	// An untouched topic takes the new content.
	if got, _ := w.LookupHelp(world.CorpusHelp, "drop", false); got.Modified != 0 {
		t.Errorf("an untouched topic was stamped: %+v", got)
	}
}

// TestApplyForceReplacesEverything covers "fbemerald help-seed
// -force", which is how a wizard who has made a mess of the manual
// gets it back.
func TestApplyForceReplacesEverything(t *testing.T) {
	w := world.New()
	Apply(w, "", false)
	w.SetHelpTopic(world.HelpTopic{Corpus: world.CorpusHelp,
		Name: "look", Body: "our own version"})

	if changed := Apply(w, Version, true); len(changed) == 0 {
		t.Fatal("forcing changed nothing")
	}
	got, _ := w.LookupHelp(world.CorpusHelp, "look", false)
	if got.Body == "our own version" {
		t.Error("forcing left an edited topic in place")
	}
}
