package world

import (
	"sort"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// The corpora the help system serves. Upstream keeps each of these in
// a file or a directory under the game directory; Emerald has no game
// directory, so they are rows instead.
//
// The first four are upstream's "index files": one file of blocks
// separated by '~', each block headed by a '|'-separated list of
// aliases. Info is upstream's one directory whose file names are
// prefix-matched. The rest are single texts, stored as a corpus with
// one unnamed topic so there is exactly one mechanism to load, write
// and edit.
const (
	CorpusHelp    = "help"
	CorpusNews    = "news"
	CorpusMan     = "man"
	CorpusMPI     = "mpi"
	CorpusInfo    = "info"
	CorpusMOTD    = "motd"
	CorpusCredits = "credits"
	CorpusWelcome = "welcome"
	CorpusConnect = "connect"
)

// Corpora lists every corpus, in the order a listing should show
// them.
func Corpora() []string {
	return []string{
		CorpusHelp, CorpusNews, CorpusMan, CorpusMPI, CorpusInfo,
		CorpusMOTD, CorpusCredits, CorpusWelcome, CorpusConnect,
	}
}

// SingletonCorpus reports whether a corpus holds one text rather than
// a set of topics. Those are stored as a single topic with an empty
// name.
func SingletonCorpus(corpus string) bool {
	switch corpus {
	case CorpusMOTD, CorpusCredits, CorpusWelcome, CorpusConnect:
		return true
	}
	return false
}

// HelpTopic is one entry in one corpus.
//
// Aliases are the other names the topic answers to, which upstream
// writes as the '|'-separated first line of an index block. Name is
// the first of them, and is empty for the header block a corpus shows
// when no topic is asked for.
type HelpTopic struct {
	Corpus  string
	Name    string
	Aliases []string
	// Ord is the position in the corpus, which decides the order
	// a topic listing and an export show.
	Ord  int
	Body string
	// Modified is when the topic was last written, in Unix
	// seconds. Seeding uses it to tell a topic a wizard has
	// edited from one still as it shipped.
	Modified int64
}

// AllNames is the topic's name followed by its other aliases, which
// is the order upstream's index line is written in.
func (t HelpTopic) AllNames() []string {
	out := make([]string, 0, 1+len(t.Aliases))
	if t.Name != "" {
		out = append(out, t.Name)
	}
	out = append(out, t.Aliases...)
	return out
}

// helpCorpus is one corpus in memory: the topics in order, and a
// folded index from every alias to the topic that owns it.
type helpCorpus struct {
	topics []HelpTopic
	byName map[string]int
}

// reindex rebuilds the alias index. An alias claimed twice belongs to
// whichever topic comes first, which is what scanning an index file
// from the top does.
//
// A header topic has no aliases at all, and is indexed under the
// empty name so that looking one up, replacing it and deleting it all
// go through the same path as any other topic.
func (c *helpCorpus) reindex() {
	c.byName = make(map[string]int, len(c.topics))
	for i, t := range c.topics {
		names := t.AllNames()
		if len(names) == 0 {
			names = []string{""}
		}
		for _, n := range names {
			k := ascii.Fold(n)
			if _, taken := c.byName[k]; !taken {
				c.byName[k] = i
			}
		}
	}
}

// SetHelp replaces a corpus without marking it for writing, which is
// what loading from the store wants.
func (w *World) SetHelp(corpus string, topics []HelpTopic) {
	if w.help == nil {
		w.help = map[string]*helpCorpus{}
	}
	c := &helpCorpus{topics: append([]HelpTopic(nil), topics...)}
	sort.SliceStable(c.topics, func(i, j int) bool {
		return c.topics[i].Ord < c.topics[j].Ord
	})
	c.reindex()
	w.help[ascii.Fold(corpus)] = c
}

// ReplaceHelp writes a whole corpus and marks it for writing, keeping
// each topic's Modified as given. Seeding uses it: SetHelpTopic would
// stamp every topic as edited, which is exactly the distinction
// seeding depends on.
func (w *World) ReplaceHelp(corpus string, topics []HelpTopic) {
	for i := range topics {
		topics[i].Corpus = corpus
		topics[i].Ord = i
	}
	w.SetHelp(corpus, topics)
	w.markHelpDirty(ascii.Fold(corpus))
}

// HelpTopics returns a corpus in order.
func (w *World) HelpTopics(corpus string) []HelpTopic {
	c := w.help[ascii.Fold(corpus)]
	if c == nil {
		return nil
	}
	return append([]HelpTopic(nil), c.topics...)
}

// HelpCorpusEmpty reports whether a corpus holds nothing, which is
// what seeding checks.
func (w *World) HelpCorpusEmpty(corpus string) bool {
	c := w.help[ascii.Fold(corpus)]
	return c == nil || len(c.topics) == 0
}

// LookupHelp finds a topic by any of its aliases.
//
// An empty name asks for the corpus header — the block before the
// first '~' in an index file — which is what a bare "help" shows.
// With prefix set, a name that is not an exact alias also matches the
// first topic whose alias it begins, which is how upstream's info
// directory resolves a partial file name.
func (w *World) LookupHelp(corpus, name string, prefix bool) (HelpTopic, bool) {
	c := w.help[ascii.Fold(corpus)]
	if c == nil || len(c.topics) == 0 {
		return HelpTopic{}, false
	}
	// The empty name is the header, which is indexed like any
	// other; a corpus without one — every directory-style
	// corpus — simply has no entry for it.
	if i, ok := c.byName[ascii.Fold(name)]; ok {
		return c.topics[i], true
	}
	if name == "" || !prefix {
		return HelpTopic{}, false
	}
	for _, t := range c.topics {
		for _, n := range t.AllNames() {
			if ascii.HasPrefix(n, name) {
				return t, true
			}
		}
	}
	return HelpTopic{}, false
}

// SetHelpTopic writes one topic, replacing whatever held its name,
// and marks the corpus for writing.
//
// A new topic goes on the end, because a corpus is ordered and the
// order is what a listing and an export show.
func (w *World) SetHelpTopic(t HelpTopic) {
	if w.help == nil {
		w.help = map[string]*helpCorpus{}
	}
	key := ascii.Fold(t.Corpus)
	c := w.help[key]
	if c == nil {
		c = &helpCorpus{byName: map[string]int{}}
		w.help[key] = c
	}
	t.Modified = w.now().Unix()

	if i, ok := c.byName[ascii.Fold(t.Name)]; ok &&
		ascii.EqualFold(c.topics[i].Name, t.Name) {
		t.Ord = c.topics[i].Ord
		c.topics[i] = t
	} else {
		t.Ord = len(c.topics)
		c.topics = append(c.topics, t)
	}
	c.reindex()
	w.markHelpDirty(key)
}

// DeleteHelpTopic removes a topic by name, reporting whether there
// was one.
func (w *World) DeleteHelpTopic(corpus, name string) bool {
	key := ascii.Fold(corpus)
	c := w.help[key]
	if c == nil {
		return false
	}
	i, ok := c.byName[ascii.Fold(name)]
	if !ok || !ascii.EqualFold(c.topics[i].Name, name) {
		return false
	}
	c.topics = append(c.topics[:i], c.topics[i+1:]...)
	c.reindex()
	w.markHelpDirty(key)
	return true
}

// SetHelpText replaces a singleton corpus's only text.
func (w *World) SetHelpText(corpus, body string) {
	w.SetHelpTopic(HelpTopic{Corpus: corpus, Body: body})
}

// HelpText returns a singleton corpus's only text, and whether there
// is one. An empty body counts: "motd clear" leaves one deliberately.
func (w *World) HelpText(corpus string) (string, bool) {
	t, ok := w.LookupHelp(corpus, "", false)
	if !ok {
		return "", false
	}
	return t.Body, true
}

// AppendHelpText adds to a singleton corpus, which is what "motd
// <text>" does.
func (w *World) AppendHelpText(corpus, body string) {
	old, _ := w.HelpText(corpus)
	if old != "" && !strings.HasSuffix(old, "\n") {
		old += "\n"
	}
	w.SetHelpText(corpus, old+body)
}

// markHelpDirty queues one corpus for writing. Corpora are tracked
// separately so appending to the motd does not rewrite the manual.
func (w *World) markHelpDirty(key string) {
	if w.helpDirty == nil {
		w.helpDirty = map[string]struct{}{}
	}
	w.helpDirty[key] = struct{}{}
}

// KnownCorpus reports whether a name is one of the corpora.
func KnownCorpus(name string) bool {
	for _, c := range Corpora() {
		if ascii.EqualFold(c, name) {
			return true
		}
	}
	return false
}
