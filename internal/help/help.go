// Package help holds the text the help system serves before a world
// has any of its own.
//
// The texts live in the database, not here: this is the seed, written
// once into a fresh world and thereafter editable from inside the
// game or from the configurator. Keeping the seed in the binary is
// what lets a world boot with working help before anyone has typed
// anything, without needing the game directory Emerald does not have.
//
// The content is Emerald's own rather than upstream's. Fuzzball's
// .raw files are GPLv3 and Emerald declares no licence, so vendoring
// them would settle that question by accident; and upstream's help
// documents about fifty commands Emerald does not implement, which
// would be worse than saying nothing.
package help

import (
	"embed"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Version identifies this release of the seed content. It is recorded
// in the database so a later release can tell a topic nobody has
// touched from one a wizard has edited.
//
// Bump it whenever the content under content/ changes.
const Version = "1"

//go:embed content
var content embed.FS

// corpusFile names the seed file for each corpus, and how to read it.
// A corpus with no entry here seeds empty.
var corpusFile = map[string]struct {
	file string
	// index is upstream's "~"-separated block format. Without it
	// the whole file is one body.
	index bool
	// header is whether the first block is the corpus header —
	// what a bare "help" shows. Upstream's info directory has no
	// header, because a bare "info" lists its files instead.
	header bool
}{
	world.CorpusHelp: {"content/help.txt", true, true},
	world.CorpusNews: {"content/news.txt", true, true},
	world.CorpusMan:  {"content/man.txt", true, true},
	world.CorpusMPI:  {"content/mpi.txt", true, true},
	world.CorpusInfo: {"content/info.txt", true, false},
	// Upstream ships a motd file holding just the rule that
	// closes an entry, which is also what "motd clear" leaves
	// behind. A fresh world starts in that same state.
	world.CorpusMOTD:    {"content/motd.txt", false, false},
	world.CorpusCredits: {"content/credits.txt", false, false},
	world.CorpusWelcome: {"content/welcome.txt", false, false},
	world.CorpusConnect: {"content/connect.txt", false, false},
}

// Seed returns the built-in topics for one corpus, in order. A corpus
// with no built-in content returns nothing, which is not an error.
func Seed(corpus string) ([]world.HelpTopic, error) {
	spec, ok := corpusFile[corpus]
	if !ok {
		return nil, nil
	}
	raw, err := content.ReadFile(spec.file)
	if err != nil {
		return nil, err
	}
	if !spec.index {
		// The file's terminating newline is not a blank line
		// at the end of the text; upstream reads these a line
		// at a time and never sees one.
		body := strings.TrimSuffix(normalise(string(raw)), "\n")
		return []world.HelpTopic{{Corpus: corpus, Body: body}}, nil
	}
	return ParseIndex(corpus, string(raw), spec.header), nil
}

// ParseIndex reads upstream's index format: blocks separated by lines
// beginning with '~', each block after the first headed by a line of
// '|'-separated aliases.
//
// With header set, the text before the first '~' is the corpus
// header, stored as the topic with no name. Without it, that text is
// discarded — an info file has no header, and a leading comment in
// one should not become a nameless topic nothing can reach.
func ParseIndex(corpus, src string, header bool) []world.HelpTopic {
	var out []world.HelpTopic
	blocks := splitBlocks(normalise(src))

	for i, b := range blocks {
		if i == 0 {
			if !header || strings.TrimSpace(b) == "" {
				continue
			}
			out = append(out, world.HelpTopic{
				Corpus: corpus,
				Ord:    len(out),
				Body:   trimBlock(b),
			})
			continue
		}
		names, body, ok := splitAliasLine(b)
		if !ok {
			continue
		}
		out = append(out, world.HelpTopic{
			Corpus:  corpus,
			Name:    names[0],
			Aliases: names[1:],
			Ord:     len(out),
			Body:    trimBlock(body),
		})
	}
	return out
}

// RenderIndex writes topics back in the index format, which is what
// an export and the configurator's editor want.
func RenderIndex(topics []world.HelpTopic) string {
	var b strings.Builder
	for i, t := range topics {
		if i > 0 {
			b.WriteString("~\n")
		}
		if t.Name != "" {
			b.WriteString(strings.Join(t.AllNames(), "|"))
			b.WriteByte('\n')
		}
		b.WriteString(t.Body)
		if !strings.HasSuffix(t.Body, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// splitBlocks cuts a source at every line starting with '~'.
// Consecutive separators produce no empty block, which is what
// upstream's "skip delimiters" loop amounts to.
func splitBlocks(src string) []string {
	var blocks []string
	var cur strings.Builder
	sep := false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "~") {
			if !sep {
				blocks = append(blocks, cur.String())
				cur.Reset()
				sep = true
			}
			continue
		}
		sep = false
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	return append(blocks, cur.String())
}

// splitAliasLine takes a block's leading alias line off it.
func splitAliasLine(b string) (names []string, body string, ok bool) {
	line, rest, _ := strings.Cut(b, "\n")
	for _, n := range strings.Split(line, "|") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, "", false
	}
	return names, rest, true
}

// normalise makes line endings uniform, so content written on another
// platform parses and displays the same way.
func normalise(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// trimBlock drops the blank lines a separator leaves at a block's
// edges while keeping the ones inside it, which are real blank lines
// in the text.
func trimBlock(s string) string {
	return strings.Trim(s, "\n")
}
