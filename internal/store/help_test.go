package store

import (
	"context"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestHelpRoundTrip checks a corpus survives the store: order,
// aliases, the nameless header, and the Modified stamp seeding
// depends on to tell an edited topic from a shipped one.
func TestHelpRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := world.New()
	w.SetHelpTopic(world.HelpTopic{Corpus: world.CorpusHelp,
		Body: "the header"})
	w.SetHelpTopic(world.HelpTopic{Corpus: world.CorpusHelp,
		Name: "look", Aliases: []string{"l", "read"},
		Body: "look here"})
	w.ReplaceHelp(world.CorpusInfo, []world.HelpTopic{
		{Name: "server", Body: "about this server"},
	})

	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	back := world.New()
	n, err := s.LoadHelp(ctx, back)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("loaded %d topics, want 3", n)
	}

	got, ok := back.LookupHelp(world.CorpusHelp, "read", false)
	if !ok {
		t.Fatal("the alias did not survive")
	}
	if got.Name != "look" || got.Body != "look here" {
		t.Errorf("topic = %+v", got)
	}
	if got.Modified == 0 {
		t.Error("an edited topic came back looking unedited")
	}
	if header, ok := back.HelpText(world.CorpusHelp); !ok ||
		header != "the header" {
		t.Errorf("header = %q, ok=%v", header, ok)
	}
	// A seeded topic is written with no timestamp, which is what
	// lets a later release refresh it.
	if info, _ := back.LookupHelp(world.CorpusInfo, "server", false); info.Modified != 0 {
		t.Errorf("a seeded topic was stamped: %+v", info)
	}
}

// TestHelpWriteReplacesOneCorpus pins that writing a corpus clears
// what it held and leaves every other corpus alone. Deleting a topic
// has to be visible, and a motd append must not touch the manual.
func TestHelpWriteReplacesOneCorpus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	w := world.New()
	w.ReplaceHelp(world.CorpusNews, []world.HelpTopic{
		{Name: "one", Body: "first"},
		{Name: "two", Body: "second"},
	})
	w.ReplaceHelp(world.CorpusMOTD, []world.HelpTopic{{Body: "quiet"}})
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	w.DeleteHelpTopic(world.CorpusNews, "two")
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	back := world.New()
	if _, err := s.LoadHelp(ctx, back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back.LookupHelp(world.CorpusNews, "two", false); ok {
		t.Error("the deleted topic is still stored")
	}
	if _, ok := back.LookupHelp(world.CorpusNews, "one", false); !ok {
		t.Error("rewriting the corpus lost the topic that stayed")
	}
	if body, ok := back.HelpText(world.CorpusMOTD); !ok ||
		body != "quiet" {
		t.Errorf("an untouched corpus changed: %q ok=%v", body, ok)
	}
}

func TestHelpSeedVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	v, err := s.HelpSeedVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Errorf("a fresh database reports seed version %q", v)
	}
	if err := s.SetHelpSeedVersion(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	if v, err = s.HelpSeedVersion(ctx); err != nil || v != "7" {
		t.Errorf("seed version = %q, err=%v", v, err)
	}
}
