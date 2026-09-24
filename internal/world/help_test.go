package world

import (
	"testing"
)

func newHelpWorld(t *testing.T) *World {
	t.Helper()
	w := New()
	w.SetHelp(CorpusHelp, []HelpTopic{
		{Corpus: CorpusHelp, Ord: 0, Body: "the header"},
		{Corpus: CorpusHelp, Ord: 1, Name: "look",
			Aliases: []string{"l", "read"},
			Body:    "look here"},
		{Corpus: CorpusHelp, Ord: 2, Name: "lock", Body: "lock it"},
	})
	return w
}

func TestLookupHelpByAlias(t *testing.T) {
	w := newHelpWorld(t)
	for _, name := range []string{"look", "l", "READ", "Look"} {
		got, ok := w.LookupHelp(CorpusHelp, name, false)
		if !ok || got.Body != "look here" {
			t.Errorf("%q resolved to %+v, ok=%v", name, got, ok)
		}
	}
}

// TestLookupHelpHeaderIsTheEmptyName pins the convention the whole
// design rests on: the block a bare command shows is a topic like any
// other, stored under no name.
func TestLookupHelpHeaderIsTheEmptyName(t *testing.T) {
	w := newHelpWorld(t)
	got, ok := w.LookupHelp(CorpusHelp, "", false)
	if !ok || got.Body != "the header" {
		t.Fatalf("header = %+v, ok=%v", got, ok)
	}

	// A corpus with no header — every directory-style one —
	// has nothing to answer with.
	w.SetHelp(CorpusInfo, []HelpTopic{
		{Corpus: CorpusInfo, Name: "server", Body: "about"},
	})
	if _, ok := w.LookupHelp(CorpusInfo, "", false); ok {
		t.Error("a corpus with no header answered a bare lookup")
	}
}

func TestLookupHelpPrefixIsOptIn(t *testing.T) {
	w := newHelpWorld(t)
	if _, ok := w.LookupHelp(CorpusHelp, "loo", false); ok {
		t.Error("an index corpus must not prefix-match")
	}
	got, ok := w.LookupHelp(CorpusHelp, "loo", true)
	if !ok || got.Name != "look" {
		t.Errorf("prefix lookup = %+v, ok=%v", got, ok)
	}
	// "lo" is a prefix of both; the first in order wins, which is
	// what scanning a directory from the top does.
	got, _ = w.LookupHelp(CorpusHelp, "lo", true)
	if got.Name != "look" {
		t.Errorf("ambiguous prefix chose %q, want the first", got.Name)
	}
}

// TestSetHelpTopicReplacesRatherThanAppends covers the bug the header
// convention hid: a topic set twice must not become two topics, and
// that includes the nameless one.
func TestSetHelpTopicReplacesRatherThanAppends(t *testing.T) {
	w := newHelpWorld(t)
	w.SetHelpTopic(HelpTopic{Corpus: CorpusHelp, Name: "look",
		Body: "replaced"})
	w.SetHelpText(CorpusHelp, "new header")

	topics := w.HelpTopics(CorpusHelp)
	if len(topics) != 3 {
		t.Fatalf("corpus has %d topics, want 3: %+v", len(topics), topics)
	}
	if body, _ := w.HelpText(CorpusHelp); body != "new header" {
		t.Errorf("header = %q", body)
	}
	got, _ := w.LookupHelp(CorpusHelp, "look", false)
	if got.Body != "replaced" {
		t.Errorf("look = %q", got.Body)
	}
	// Replacing a topic keeps its position, so the corpus does
	// not reshuffle every time somebody edits it.
	if got.Ord != 1 {
		t.Errorf("look moved to position %d", got.Ord)
	}
}

func TestDeleteHelpTopic(t *testing.T) {
	w := newHelpWorld(t)
	if !w.DeleteHelpTopic(CorpusHelp, "look") {
		t.Fatal("delete reported nothing to delete")
	}
	if _, ok := w.LookupHelp(CorpusHelp, "l", false); ok {
		t.Error("an alias outlived the topic it belonged to")
	}
	if w.DeleteHelpTopic(CorpusHelp, "look") {
		t.Error("deleting twice reported a second removal")
	}
}

// TestHelpDirtyIsPerCorpus is the reason the snapshot is a map: a
// motd append must not rewrite the manual, which a wizard may be
// half-way through editing from somewhere else.
func TestHelpDirtyIsPerCorpus(t *testing.T) {
	w := newHelpWorld(t)
	w.SetHelpText(CorpusMOTD, "closed for repairs")

	s := w.TakeSnapshot()
	if _, ok := s.Help[CorpusMOTD]; !ok {
		t.Error("the changed corpus was not written")
	}
	if _, ok := s.Help[CorpusHelp]; ok {
		t.Error("an untouched corpus was rewritten")
	}
	if s.Empty() {
		t.Error("a snapshot carrying help is not empty")
	}

	// The dirty set is drained, so nothing is written twice.
	if s := w.TakeSnapshot(); s.Help != nil {
		t.Errorf("help was still pending: %+v", s.Help)
	}
}

// TestSetHelpDoesNotMark separates the two entry points: loading from
// the store must not queue what it just read for writing back.
func TestSetHelpDoesNotMark(t *testing.T) {
	w := New()
	w.SetHelp(CorpusNews, []HelpTopic{{Corpus: CorpusNews, Body: "x"}})
	if s := w.TakeSnapshot(); s.Help != nil {
		t.Errorf("loading queued a write: %+v", s.Help)
	}
}

func TestAppendHelpText(t *testing.T) {
	w := New()
	w.AppendHelpText(CorpusMOTD, "first")
	w.AppendHelpText(CorpusMOTD, "second")
	if got, _ := w.HelpText(CorpusMOTD); got != "first\nsecond" {
		t.Errorf("motd = %q", got)
	}
}
