package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The three MCP packages this server answers for itself.
//
// `internal/session/mcp.go` advertised five names —
// `org-fuzzball-help`, `org-fuzzball-notify`,
// `org-fuzzball-simpleedit`, `org-fuzzball-languages` and
// `dns-org-mud-moo-simpleedit` — and `installMCPHandlers` gave
// every one of them `programPackageHandler`. So a client that spoke
// them got nothing unless a MUF program had claimed the message, and
// `@mcpedit` had nowhere to send a save *back* to: half a feature.
//
// Upstream registers `mcppkg_help_request`, `mcppkg_languages` and
// `mcppkg_simpleedit` for them (`mcp.c:996`), the last under three of
// the names. **A program cannot bind these**: `MCP_BIND` only reaches
// a package registered by `MCP_REGISTER`, whose handler is
// `muf_mcp_callback` (`p_mcp.c:519`), so the built-ins keep the
// server's. `programPackageHandler` stays as the default for what a
// program registers.

// installPackageHandlers replaces programPackageHandler on the four
// names upstream answers itself.
func (s *Server) installPackageHandlers(list []mcp.Package) {
	for i, p := range list {
		switch ascii.Fold(p.Name) {
		case mcp.HelpPackage:
			list[i].Handle = s.mcpHelpHandler()
		case mcp.LanguagesPackage:
			list[i].Handle = s.mcpLanguagesHandler()
		case mcp.SimpleEditPackage, mcp.MooSimpleEditPackage,
			mcp.NotifyPackage:
			// All three go to `mcppkg_simpleedit`, which
			// is upstream's and reads oddly: the notify
			// package is *output* only, so its handler
			// can never match a message name and does
			// nothing. Registered the same way anyway.
			list[i].Handle = s.mcpSimpleEditHandler()
		}
	}
}

// mcpLanguagesHandler is mcppkg_languages (`mcppkgs.c:412`): one
// message, one answer.
func (s *Server) mcpLanguagesHandler() func(*mcp.Frame,
	*mcp.Message, mcp.Version) {

	return func(f *mcp.Frame, msg *mcp.Message, _ mcp.Version) {
		v := f.Supports(mcp.LanguagesPackage)
		if v.Major == 0 && v.Minor == 0 {
			f.SendInband("MCP: " + mcp.LanguagesPackage +
				" not supported.")
			return
		}
		if !ascii.EqualFold(msg.Name, "request") {
			return
		}
		// Upstream's own TODO wonders whether the version
		// should be a constant and whether "7.0" is still
		// true. It is the MUCK's version, and this server
		// answers for the same language.
		_ = f.SendMessage(mcp.NewMessage(mcp.LanguagesPackage,
			"supported").AddArg("languages", "muf:7.0"))
	}
}

// mcpHelpHandler is mcppkg_help_request (`mcppkgs.c:451`): a client
// asks for a help topic by name and type, and gets the text or an
// error.
//
// Upstream opens the file named by one of four `file_*` parameters
// and walks it for the topic. Emerald's corpora are rows in Postgres,
// so the four type words map to corpus names instead — and the
// "Sorry, %s is missing." branch, which upstream reaches when the
// file will not open, is reached here when the corpus is empty.
func (s *Server) mcpHelpHandler() func(*mcp.Frame, *mcp.Message,
	mcp.Version) {

	return func(f *mcp.Frame, msg *mcp.Message, _ mcp.Version) {
		if v := f.Supports(mcp.HelpPackage); v.Major == 0 &&
			v.Minor == 0 {
			f.SendInband("MCP: " + mcp.HelpPackage +
				" not supported.")
			return
		}
		if !ascii.EqualFold(msg.Name, "request") {
			return
		}
		topic, _ := msg.Arg("topic")
		valtype, _ := msg.Arg("type")

		fail := func(text string) {
			_ = f.SendMessage(mcp.NewMessage(
				mcp.HelpPackage, "error").
				AddArg("text", text).
				AddArg("topic", topic))
		}
		corpus := helpCorpusFor(valtype)
		if corpus == "" {
			fail(sprintf("Sorry, %s is not a valid help "+
				"type.", valtype))
			return
		}
		_ = s.engine.Go(func(w *world.World) {
			if w.HelpCorpusEmpty(corpus) {
				fail(missingHelp(corpus))
				return
			}
			t, ok := w.LookupHelp(corpus, topic, false)
			if !ok {
				fail(sprintf("Sorry, no help "+
					"available on topic \"%s\"",
					topic))
				return
			}
			_ = f.SendMessage(mcp.NewMessage(
				mcp.HelpPackage, "entry").
				AddArg("topic", topic).
				AddMultiline("text",
					strings.Split(t.Body, "\n")))
		})
	}
}

// missingHelp is upstream's unopenable-file message, which here means
// a corpus with no rows in it.
func missingHelp(corpus string) string {
	return sprintf("Sorry, %s is missing.  Management has "+
		"been notified.", corpus)
}

// helpCorpusFor maps the four type words upstream accepts onto the
// corpora they name. Anything else is not a help type at all.
func helpCorpusFor(valtype string) string {
	switch ascii.Fold(valtype) {
	case "man":
		return "man"
	case "mpi":
		return "mpi"
	case "help":
		return "help"
	case "news":
		return "news"
	}
	return ""
}

// mcpSimpleEditHandler is mcppkg_simpleedit (`mcppkgs.c:85`): the
// other end of `@mcpedit`, and of any client that offers to edit a
// property or a `@tune` parameter.
//
// The reference is `<dbref>.<category>.<rest>`, and the five
// categories are `prop`, `proplist`, `prog`, `sysparm` and `user` —
// the last of which does nothing, with an upstream TODO wondering
// whether it should be an error.
//
// Every category re-checks its own permissions, which upstream's own
// TODO calls insecure for being duplicated. Reproduced, because what
// each one checks is not quite the same.
func (s *Server) mcpSimpleEditHandler() func(*mcp.Frame,
	*mcp.Message, mcp.Version) {

	return func(f *mcp.Frame, msg *mcp.Message, _ mcp.Version) {
		if !ascii.EqualFold(msg.Name, "set") {
			return
		}
		ref0, _ := msg.Arg("reference")
		valtype, _ := msg.Arg("type")
		content, _ := msg.Lines("content")
		obj, category, rest, ok := parseEditRef(ref0)
		if !ok {
			f.SendError("simpleedit-set",
				"Bad reference value.")
			return
		}
		_ = s.engine.Go(func(w *world.World) {
			d := s.descriptorFor(f)
			if d == nil || !d.Connected {
				return
			}
			s.simpleEditSet(w, f, d.Player, d.ID, obj,
				category, rest, valtype, content)
		})
	}
}

// parseEditRef splits "<dbref>.<category>.<rest>".
//
// A missing dbref is **NOTHING**, not an error: `sysparm` has no
// object, so its reference begins with a bare dot. The number is
// bounded at eight digits, which is upstream's own guard against a
// client sending something enormous.
func parseEditRef(s string) (obj ref.Ref, category, rest string,
	ok bool) {

	obj = ref.Nothing
	i := 0
	if i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			n = n*10 + int(s[i]-'0')
			if n >= 100000000 {
				return ref.Nothing, "", "", false
			}
			i++
		}
		obj = ref.Ref(n)
	}
	if i >= len(s) || s[i] != '.' {
		return ref.Nothing, "", "", false
	}
	i++
	j := strings.IndexByte(s[i:], '.')
	if j < 0 {
		return ref.Nothing, "", "", false
	}
	return obj, s[i : i+j], s[i+j+1:], true
}

// simpleEditSet applies one save, on the world goroutine.
func (s *Server) simpleEditSet(w *world.World, f *mcp.Frame,
	player ref.Ref, descr int, obj ref.Ref,
	category, path, valtype string, content []string) {

	switch ascii.Fold(category) {
	case "prop":
		if !s.editableProp(w, f, player, obj, path) {
			return
		}
		switch ascii.Fold(valtype) {
		case "string-list", "string":
			// A string-list is joined with carriage
			// returns into **one** property, which is
			// what makes a multi-line description one
			// value rather than a list.
			w.SetProp(obj, path, props.Value{
				Type: props.String,
				Str:  strings.Join(content, "\r")})
		case "integer":
			if len(content) != 1 {
				f.SendError("simpleedit-set",
					"Bad integer value.")
				return
			}
			w.SetProp(obj, path, props.Value{
				Type: props.Int,
				Num:  int64(leadingInt(content[0]))})
		}
	case "proplist":
		if !s.editableProp(w, f, player, obj, path) {
			return
		}
		if !ascii.EqualFold(valtype, "string-list") {
			f.SendError("simpleedit-set",
				"Bad value type for proplist.")
			return
		}
		// The count property is removed first either way, so
		// an empty list leaves no stale "name#" behind.
		w.SetProp(obj, path+"#", props.Value{})
		if len(content) == 0 {
			return
		}
		w.SetProp(obj, path+"#", props.Value{
			Type: props.Int, Num: int64(len(content))})
		for i, line := range content {
			if line == "" {
				// An empty line is stored as a space,
				// because an empty value would remove
				// the property and shorten the list.
				line = " "
			}
			w.SetProp(obj, sprintf("%s#/%d", path, i+1),
				props.Value{Type: props.String,
					Str: line})
		}
	case "prog":
		o := w.Get(obj)
		if o == nil {
			f.SendError("simpleedit-set",
				"Bad reference object.")
			return
		}
		if o.Type() != ref.TypeProgram ||
			!s.controls(w, player, obj) {
			f.SendError("simpleedit-set",
				"Permission denied.")
			return
		}
		// `Mucker(player)` as well as controlling it: a
		// wizard with no mucker bits may not save a program.
		if p := w.Get(player); p == nil ||
			p.Flags.MLevel() < 1 {
			f.SendError("simpleedit-set",
				"Permission denied.")
			return
		}
		if o.Flags&ref.Internal != 0 {
			f.SendError("simpleedit-set", "Sorry, this "+
				"program is currently being edited."+
				"  Try again later.")
			return
		}
		lines := make([]string, len(content))
		for i, line := range content {
			if line == "" {
				// An empty line is stored as a space,
				// which is upstream's and is what
				// keeps a blank line in the middle of
				// a program.
				line = " "
			}
			lines[i] = line
		}
		w.SaveSource(obj, joinSource(lines))
		s.InvalidateProgram(obj)
		s.logProgramText(w, player, obj, joinSource(lines))
		s.log.Info("program saved",
			"program", obj.String(),
			"name", nameOf(w, obj),
			"by", nameOf(w, player),
			"player", player.String(),
			"lines", len(lines))
		// `do_compile(descr, player, obj, 1)` -- the final
		// argument is "force_err_display", so a compile
		// failure is reported to whoever saved rather than
		// only logged.
		if _, notes, err := s.compileSource(w, obj,
			joinSource(lines)); err != nil {
			for _, n := range notes {
				s.send(w, player, n)
			}
			s.send(w, player, compileErrorText(err))
		} else {
			for _, n := range notes {
				s.send(w, player, n)
			}
		}
	case "sysparm":
		if p := w.Get(player); p == nil ||
			!p.Flags.IsWizard() {
			f.SendError("simpleedit-set",
				"Permission denied.")
			return
		}
		if len(content) != 1 {
			f.SendError("simpleedit-set",
				"Bad @tune value.")
			return
		}
		mlev := (&mufHost{s: s, w: w}).TuneMLevel(player)
		_ = w.SetParm(path, content[0], mlev,
			s.tuneRefResolverFor(w, player))
	case "user":
		// Nothing, which is upstream's — with a TODO asking
		// whether it should be an error.
	default:
		f.SendError("simpleedit-set",
			"Unknown reference category.")
	}
}

// editableProp is the permission block `prop` and `proplist` share:
// the object has to exist and be controlled, the name may hold no
// ':', and the sigils apply.
func (s *Server) editableProp(w *world.World, f *mcp.Frame,
	player, obj ref.Ref, path string) bool {

	if w.Get(obj) == nil {
		f.SendError("simpleedit-set", "Bad reference object.")
		return false
	}
	if !s.controls(w, player, obj) {
		f.SendError("simpleedit-set", "Permission denied.")
		return false
	}
	if strings.ContainsRune(path, ':') {
		f.SendError("simpleedit-set", "Bad property name.")
		return false
	}
	wiz := isWizard(w, ownerOf(w, player))
	if props.IsSystem(path) || !wiz &&
		(props.IsSeeOnly(path) || props.IsHidden(path)) {
		f.SendError("simpleedit-set", "Permission denied.")
		return false
	}
	return true
}
