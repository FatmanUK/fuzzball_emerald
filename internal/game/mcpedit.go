package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// `@mcpedit` and `@mcpprogram` are `@edit` and `@program` for a
// client that speaks MCP: instead of entering the line editor, the
// program's text is shipped to the client as a
// `dns-org-mud-moo-simpleedit content` message and comes back through
// the package handler when the user saves.
//
// Both were in the dispatch table with no handler, which is two of
// the nine names that had none.
//
// **The fallback is most of the design.** A client that never
// negotiated MCP, or negotiated it without that package, gets the
// line editor — so the commands are usable from any client and a
// world can bind them to a key without asking what is on the other
// end. `mcpedit_program` (`mcp.c:2011`) tests both conditions before
// it checks any permission at all.

// cmdMcpedit is do_mcpedit (`mcp.c:2088`).
//
// Its matcher is `@edit`'s and its refusals are not: where `@edit`
// answers "I don't see that here." for a failed match, this goes
// through `noisy_match_result`, and its permission failures are **MCP
// error messages** rather than lines of text — so a client shows
// them in a dialog and a plain client sees nothing at all.
func (s *Server) cmdMcpedit(c *ctx) {
	name := strings.TrimSpace(c.arg)
	if name == "" {
		c.tell("No program name given.")
		return
	}
	if !s.requireNotGuest(c, "@mcpedit") ||
		!s.requireMucker(c, "@mcpedit") {
		return
	}
	program := matchProgram(c, name)
	if !noisyMatch(c, name, program) {
		return
	}
	s.mcpEditProgram(c, program)
}

// cmdMcpprogram is do_mcpprogram (`mcp.c:2130`).
//
// It differs from `@program` in one line that matters: a failed match
// is **silent**, because it is `match_result` rather than
// `noisy_match_result` — the command's whole purpose is to create
// what is not there. An ambiguous name still reports itself.
//
// Upstream's own comment says it does **not** check for a MUCKER bit,
// "so do not rely on this for permission checking". The guard is the
// dispatcher's, exactly as for `@program`.
func (s *Server) cmdMcpprogram(c *ctx) {
	name, rname, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	rname = strings.TrimSpace(rname)
	if !s.requireNotGuest(c, "@mcpprogram") ||
		!s.requireMucker(c, "@mcpprogram") {
		return
	}

	program := matchProgram(c, name)
	if rname != "" || program == ref.Nothing {
		var err error
		program, err = s.createProgram(c.w, c.who, name)
		if err != nil {
			c.send(err.Error())
			return
		}
		c.tell("Program %s created.",
			s.unparse(c.w, c.who, program))
		if rname != "" {
			s.registerBuilt(c, rname, program)
		}
	} else if program == ref.Ambiguous {
		c.tell("I don't know which one you mean.")
		return
	}
	s.mcpEditProgram(c, program)
}

// mcpEditProgram is mcpedit_program (`mcp.c:2011`), shared by both
// commands.
//
// The order is upstream's and is the part worth reading: the two
// fallbacks come **first**, so a client that cannot be sent the text
// gets the line editor whatever the permissions are — and the line
// editor then makes the same two checks for itself. Only once MCP is
// known to be available do the refusals become MCP errors.
func (s *Server) mcpEditProgram(c *ctx, program ref.Ref) {
	f := c.d.MCP
	if f == nil || !f.Enabled() {
		s.edit(c, program)
		return
	}
	if v := f.Supports(mcp.MooSimpleEditPackage); v.Major == 0 &&
		v.Minor == 0 {
		s.edit(c, program)
		return
	}

	o := c.w.Get(program)
	if o == nil || o.Type() != ref.TypeProgram ||
		!s.controls(c.w, c.who, program) {
		f.SendError("edit program", "Permission denied!")
		return
	}
	if o.Flags&ref.Internal != 0 {
		f.SendError("edit program", "Sorry, this program is "+
			"currently being edited by someone else.  "+
			"Try again later.")
		return
	}

	// The current program is remembered on the player, which is
	// what the save message's reference is matched against —
	// and what `@list` with no argument falls back to.
	s.editLine[program] = s.editLine[program]
	src, _ := c.w.Source(program)

	// "<ref>.prog." is the reference the client sends back, and
	// the name is prose rather than a path: a client puts it in
	// the window title.
	msg := mcp.NewMessage(mcp.MooSimpleEditPackage, "content").
		AddArg("reference", itoa(int(program))+".prog.").
		AddArg("type", "muf-code").
		AddArg("name", sprintf("a program named %s(%d)",
			o.Name, int(program)))
	msg.AddMultiline("content", splitSource(src))
	_ = f.SendMessage(msg)
}
