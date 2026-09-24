package golden

import (
	"os"
	"path/filepath"

	"github.com/FatmanUK/fuzzball_emerald/internal/help"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The help system's two servers read the same texts from different
// places: the C from files under its game directory, Emerald from
// Postgres. So that they can be compared, both are given the content
// below — written as files for the oracle, parsed into corpora for
// this server.
//
// It is deliberately small. What is being compared is the machinery
// — how a topic is found, what a miss says, how a listing is laid
// out — and not the prose, which the two projects do not share.

// helpIndex is one index corpus in upstream's format. The blank line
// in the "look" block is there to check that both servers render an
// empty line as two spaces.
const helpIndex = `Test help.
Type "help <topic>".
~
look|l|read
Look at something.

It does not list exits.
~
go|move
Leave through an exit.
`

const newsIndex = `No news.
~
opening
The east wing is open.
`

const manIndex = `Test manual.
~
stack
MUF is postfix.
`

const mpiIndex = `Test MPI.
~
syntax
Calls look like {func:arg}.
`

// infoFiles are upstream's info directory: one file per topic,
// prefix-matched. The names are different lengths on purpose, because
// the listing pads each to a twenty-column field and that padding is
// what the golden case checks.
var infoFiles = map[string]string{
	"server":      "A test server.",
	"newbie":      "Type look.",
	"building":    "It costs pennies.",
	"programming": "Use MUF.",
}

// writeHelpData lays the texts out as the C server expects to find
// them, under the game directory's data/.
func writeHelpData(dataDir string) error {
	files := map[string]string{
		"help.txt":         helpIndex,
		"news.txt":         newsIndex,
		"man.txt":          manIndex,
		"mpihelp.txt":      mpiIndex,
		"motd.txt":         "",
		"connect-help.txt": "Type connect <name> <password>.\n",
		"credits.txt":      "Test credits.\n",
	}
	for name, body := range files {
		path := filepath.Join(dataDir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}

	infoDir := filepath.Join(dataDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return err
	}
	for name, body := range infoFiles {
		path := filepath.Join(infoDir, name)
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// loadHelpData puts the same texts into a world, which is where this
// server reads them from.
func loadHelpData(w *world.World) {
	w.ReplaceHelp(world.CorpusHelp,
		help.ParseIndex(world.CorpusHelp, helpIndex, true))
	w.ReplaceHelp(world.CorpusNews,
		help.ParseIndex(world.CorpusNews, newsIndex, true))
	w.ReplaceHelp(world.CorpusMan,
		help.ParseIndex(world.CorpusMan, manIndex, true))
	w.ReplaceHelp(world.CorpusMPI,
		help.ParseIndex(world.CorpusMPI, mpiIndex, true))
	w.SetHelpText(world.CorpusMOTD, "")
	w.SetHelpText(world.CorpusCredits, "Test credits.")
	w.SetHelpText(world.CorpusConnect,
		"Type connect <name> <password>.")

	topics := make([]world.HelpTopic, 0, len(infoFiles))
	for _, name := range infoNames {
		topics = append(topics, world.HelpTopic{
			Name: name, Body: infoFiles[name]})
	}
	w.ReplaceHelp(world.CorpusInfo, topics)
}

// infoNames fixes the order this server lists its info topics in.
//
// The bare "info" listing is not compared against the oracle and
// cannot be: the C builds it from readdir, which hands back whatever
// order the filesystem happens to hold, so the two transcripts would
// differ by nothing but luck. The column arithmetic that listing uses
// is pinned by TestInfoListingIsInColumns instead, against the
// padding rule ported from do_info.
var infoNames = []string{"building", "newbie", "programming", "server"}
