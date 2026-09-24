package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The help system: src/help.c.
//
// Upstream reads these out of files under the game directory, one
// "index file" per corpus with '~'-separated blocks, plus one
// directory whose file names "info" prefix-matches. Emerald has no
// game directory, so a corpus is rows in Postgres and a topic is a
// row — which is also what makes it editable from inside the game
// and from the configurator.
//
// What a player sees is upstream's, down to the wording of a miss and
// the two spaces a blank line becomes.

// noInfoMsg is config.h's NO_INFO_MSG.
const noInfoMsg = "That file does not exist.  " +
	"Type 'info' to get a list of the info files available."

// motdRule is the separator do_motd writes after every entry.
const motdRule = "- - - - - - - - - - - - - - - - - - - " +
	"- - - - - - - - - - - - - - - - - - -"

func init() {
	commands["help"] = func(s *Server, c *ctx) {
		s.showHelp(c, world.CorpusHelp)
	}
	commands["news"] = func(s *Server, c *ctx) {
		s.showHelp(c, world.CorpusNews)
	}
	commands["man"] = func(s *Server, c *ctx) {
		s.showHelp(c, world.CorpusMan)
	}
	// Upstream's command is "mpi"; only the tune parameters are
	// called file_mpihelp. "mpihelp" is registered too, because
	// the parameter names make it an easy thing to type.
	commands["mpi"] = func(s *Server, c *ctx) {
		s.showHelp(c, world.CorpusMPI)
	}
	commands["mpihelp"] = commands["mpi"]
	commands["info"] = (*Server).cmdInfo
	commands["motd"] = (*Server).cmdMOTD

	// Upstream prefix-matches "help" and "news", so "h" and "n"
	// reach them. This table is exact-match only, so the prefixes
	// are registered by hand. They cost nothing: an exit of the
	// same name still wins, because exits are matched first.
	for _, p := range []string{"h", "he", "hel"} {
		commands[p] = commands["help"]
	}
	for _, p := range []string{"n", "ne", "new"} {
		commands[p] = commands["news"]
	}

	atCommands["@credits"] = (*Server).cmdCredits
	atCommands["@help"] = (*Server).cmdEditHelp

	// @credits would otherwise make "@cr" and "@cre" ambiguous
	// against @create, and lookupAtCommand answers an ambiguous
	// prefix with nothing at all. Upstream exact-matches
	// "@credits" ahead of "@create" for the same reason.
	exactOnlyCommands["@credits"] = true
}

// showHelp is do_helpfile: look a topic up in one index corpus and
// print it.
//
// The segment after "=" is parsed and then ignored, which is
// upstream's behaviour and looks like an oversight until you read
// index_file: it never sees the segment at all. Only show_subfile
// honours one, and that is the per-file form — the separate
// data/help directory, which is empty in every deployment this has
// been compared against. "info" is the corpus that really does use
// its segment.
func (s *Server) showHelp(c *ctx, corpus string) {
	topic, _ := splitTopic(c.arg)
	t, ok := c.w.LookupHelp(corpus, topic, false)
	if !ok {
		if topic == "" {
			// No header and no topic asked for. Upstream
			// would have failed to open the file, which
			// is what this corresponds to.
			c.tell("Sorry, %s is missing.  "+
				"Management has been notified.", corpus)
			return
		}
		c.tell("Sorry, no help available on topic \"%s\"", topic)
		return
	}
	spitSegment(c, t.Body, "")
}

// cmdInfo is do_info. Unlike the others it prefix-matches, and a bare
// "info" lists what there is rather than showing a header.
func (s *Server) cmdInfo(c *ctx) {
	topic, seg := splitTopic(c.arg)
	if topic == "" {
		s.listInfo(c)
		return
	}
	t, ok := c.w.LookupHelp(world.CorpusInfo, topic, true)
	if !ok {
		c.send(noInfoMsg)
		return
	}
	spitSegment(c, t.Body, seg)
}

// listInfo prints the available topics in upstream's layout: three to
// a line, each padded to a twenty-column field starting at column
// four.
func (s *Server) listInfo(c *ctx) {
	topics := c.w.HelpTopics(world.CorpusInfo)
	if len(topics) == 0 {
		c.send("No information files are available.")
		return
	}
	c.send("Available information files are:")

	line := "    "
	cols := 0
	for _, t := range topics {
		if t.Name == "" {
			continue
		}
		if cols > 2 || len(line)+len(t.Name) > 63 {
			c.send(line)
			line = "    "
			cols = 0
		}
		cols++
		line += t.Name + " "
		// Upstream pads to the next multiple of twenty offset
		// by four, so the names line up in columns however
		// long they are.
		for len(line)%20 != 4 {
			line += " "
		}
	}
	if strings.TrimSpace(line) != "" {
		c.send(line)
	}
}

// cmdMOTD is do_motd: everyone may read it, a wizard may add to it or
// clear it.
//
// The text is the whole rest of the line rather than an "=" argument,
// which is upstream's full_command.
func (s *Server) cmdMOTD(c *ctx) {
	text := strings.TrimSpace(c.arg)
	if text == "" || !isWizard(c.w, ownerOf(c.w, c.who)) {
		body, _ := c.w.HelpText(world.CorpusMOTD)
		spitSegment(c, body, "")
		return
	}

	if ascii.EqualFold(text, "clear") {
		c.w.SetHelpText(world.CorpusMOTD, motdRule)
		c.tell("MOTD cleared.")
		return
	}

	stamp := c.w.Now().Format("Mon Jan 02 15:04:05 MST 2006")
	entry := stamp + "\n" + wrapMOTD(text) + motdRule
	c.w.AppendHelpText(world.CorpusMOTD, entry)
	c.tell("MOTD updated.")
}

// wrapMOTD is add_motd_text_fmt: indent four, break at sixty-eight,
// and run on to seventy-six rather than split a word.
func wrapMOTD(text string) string {
	var out strings.Builder
	p := []byte(text)
	i, count := 0, 4
	line := []byte("    ")

	for i < len(p) {
		for i < len(p) && count < 68 {
			line = append(line, p[i])
			i++
			count++
		}
		for i < len(p) && !isMOTDSpace(p[i]) && count < 76 {
			line = append(line, p[i])
			i++
			count++
		}
		out.Write(line)
		out.WriteByte('\n')
		for i < len(p) && isMOTDSpace(p[i]) {
			i++
		}
		line = line[:0]
		count = 0
	}
	return out.String()
}

func isMOTDSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
		ch == '\v' || ch == '\f'
}

// cmdCredits is do_credits.
func (s *Server) cmdCredits(c *ctx) {
	body, ok := c.w.HelpText(world.CorpusCredits)
	if !ok {
		c.tell("Sorry, the credits are missing.  " +
			"Management has been notified.")
		return
	}
	spitSegment(c, body, "")
}

// splitTopic divides "topic=segment" the way the command parser
// divides every other line on '='.
func splitTopic(arg string) (topic, seg string) {
	topic, seg, _ = strings.Cut(arg, "=")
	return strings.TrimSpace(topic), strings.TrimSpace(seg)
}

// spitSegment is spit_file_segment: print a body, optionally cut to a
// line or a range of lines.
//
// A blank line prints as two spaces, which is upstream's and is
// load-bearing for clients that collapse empty output.
func spitSegment(c *ctx, body, seg string) {
	start, end := parseSegment(seg)
	for i, line := range strings.Split(body, "\n") {
		n := i + 1
		if start != 0 && n < start {
			continue
		}
		if end != 0 && n > end {
			break
		}
		if line == "" {
			c.send("  ")
			continue
		}
		c.send(line)
	}
}

// parseSegment reads "5" as a single line and "3-7" as a range. A
// zero bound is unbounded, so "3-" starts at three and runs to the
// end. Anything unparseable reads as zero, which is atoi's answer and
// so upstream's.
func parseSegment(seg string) (start, end int) {
	if seg == "" {
		return 0, 0
	}
	i := 0
	for i < len(seg) && seg[i] >= '0' && seg[i] <= '9' {
		i++
	}
	start = atoiPrefix(seg[:i])
	if i == len(seg) {
		return start, start
	}
	for i < len(seg) && (seg[i] < '0' || seg[i] > '9') {
		i++
	}
	return start, atoiPrefix(seg[i:])
}

// atoiPrefix reads the leading digits of a string, as atoi does.
func atoiPrefix(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0
	}
	return n
}

// cmdEditHelp is @help, which edits the corpora from inside the game.
//
// Upstream has no such command: its texts are files, edited with a
// text editor beside the server. Emerald's live in the database, so
// something has to reach them, and this is the minimum that does.
// Writing a whole topic is the configurator's job — a line at a
// time through a command line is no way to author a manual.
func (s *Server) cmdEditHelp(c *ctx) {
	if !isWizard(c.w, ownerOf(c.w, c.who)) {
		c.tell("Permission denied!")
		return
	}
	sub, rest := trimCommand(strings.TrimSpace(c.arg))
	corpus, rest := trimCommand(strings.TrimSpace(rest))
	name, text, hasText := strings.Cut(rest, "=")
	name = strings.TrimSpace(name)

	if sub != "" && !world.KnownCorpus(corpus) &&
		!ascii.EqualFold(sub, "#help") {
		c.tell("No such help corpus.  Try one of: %s",
			strings.Join(world.Corpora(), " "))
		return
	}

	switch ascii.Fold(sub) {
	case "#list":
		s.listHelpTopics(c, corpus)
	case "#show":
		t, ok := c.w.LookupHelp(corpus, name, false)
		if !ok {
			c.tell("No such topic.")
			return
		}
		spitSegment(c, t.Body, "")
	case "#set":
		if !hasText {
			c.tell("Usage: @help #set <corpus> <topic>=<text>")
			return
		}
		s.setHelpTopic(c, corpus, name, text, false)
	case "#append":
		if !hasText {
			c.tell("Usage: @help #append <corpus> <topic>=<text>")
			return
		}
		s.setHelpTopic(c, corpus, name, text, true)
	case "#del":
		if c.w.DeleteHelpTopic(corpus, name) {
			c.tell("Topic deleted.")
		} else {
			c.tell("No such topic.")
		}
	case "#alias":
		s.aliasHelpTopic(c, corpus, name, text)
	default:
		c.send("@help #list <corpus>")
		c.send("@help #show <corpus> <topic>")
		c.send("@help #set <corpus> <topic>=<text>")
		c.send("@help #append <corpus> <topic>=<text>")
		c.send("@help #del <corpus> <topic>")
		c.send("@help #alias <corpus> <topic>=<alias>|<alias>")
		c.tell("Corpora: %s", strings.Join(world.Corpora(), " "))
		c.send("A topic named \"\" is the block a bare command shows.")
	}
}

// listHelpTopics shows what a corpus holds, with the header block
// named so it can be edited like any other topic.
func (s *Server) listHelpTopics(c *ctx, corpus string) {
	topics := c.w.HelpTopics(corpus)
	if len(topics) == 0 {
		c.tell("%s is empty.", corpus)
		return
	}
	for _, t := range topics {
		names := strings.Join(t.AllNames(), "|")
		if names == "" {
			names = "(header)"
		}
		c.tell("%3d  %-28s %d bytes", t.Ord, names, len(t.Body))
	}
}

// setHelpTopic writes or extends one topic.
func (s *Server) setHelpTopic(c *ctx, corpus, name, text string, add bool) {
	t, ok := c.w.LookupHelp(corpus, name, false)
	if !ok || !ascii.EqualFold(t.Name, name) {
		t = world.HelpTopic{Corpus: corpus, Name: name}
	}
	if add && t.Body != "" {
		t.Body += "\n" + text
	} else {
		t.Body = text
	}
	c.w.SetHelpTopic(t)
	c.tell("Topic saved.")
}

// aliasHelpTopic replaces a topic's other names. An empty list
// removes them all.
func (s *Server) aliasHelpTopic(c *ctx, corpus, name, aliases string) {
	t, ok := c.w.LookupHelp(corpus, name, false)
	if !ok || !ascii.EqualFold(t.Name, name) {
		c.tell("No such topic.")
		return
	}
	t.Aliases = nil
	for _, a := range strings.Split(aliases, "|") {
		if a = strings.TrimSpace(a); a != "" {
			t.Aliases = append(t.Aliases, a)
		}
	}
	c.w.SetHelpTopic(t)
	c.tell("Aliases set.")
}
