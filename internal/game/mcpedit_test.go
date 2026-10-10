package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `@mcpedit` and `@mcpprogram` ship a program's text to the client
// instead of entering the line editor, and fall back to the editor
// when the client cannot be sent it. Neither the message nor the
// fallback is reachable from the golden harness: a message has to
// carry the authentication key the server issued, which differs every
// run, and the editor holds the input line so the harness's marker is
// eaten.
//
// So both halves are tested here, where the key can be read back out
// of the negotiation.

// TestMcpeditFallsBackToTheLineEditor is the first thing
// `mcpedit_program` tests, before any permission: a client that never
// negotiated MCP gets `@edit`.
func TestMcpeditFallsBackToTheLineEditor(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@mcpprogram spell")
	got := h.out()
	if !strings.Contains(got, "Program spell") {
		t.Errorf("no program was created:\n%s", got)
	}
	if !strings.Contains(got, "Entering editor for") {
		t.Errorf("the line editor was not entered:\n%s", got)
	}
	// Leave the editor so the harness's next line is a command
	// again.
	h.send("q")
	h.out()

	h.send("@mcpedit spell")
	if got := h.out(); !strings.Contains(got,
		"Entering editor for") {
		t.Errorf("@mcpedit did not fall back:\n%s", got)
	}
	h.send("q")
	h.out()
}

// TestMcpeditSendsTheProgramText is the other path: a client that
// supports dns-org-mud-moo-simpleedit is sent the text as a "content"
// message and the editor is **not** entered.
func TestMcpeditSendsTheProgramText(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@mcpprogram spell")
	h.out()
	h.send("q")
	h.out()

	h.send("#$#mcp version: 2.1 to: 2.1")
	key := authKeyFrom(t, h.out())
	h.send(`#$#mcp-negotiate-can ` + key +
		` package: "dns-org-mud-moo-simpleedit"` +
		` min-version: "1.0" max-version: "1.0"`)
	h.out()

	h.send("@mcpedit spell")
	got := h.out()
	if strings.Contains(got, "Entering editor for") {
		t.Errorf("the line editor was entered anyway:\n%s",
			got)
	}
	if !strings.Contains(got,
		"#$#dns-org-mud-moo-simpleedit-content") {
		t.Errorf("no content message was sent:\n%s", got)
	}
	// The reference is "<dbref>.prog.", which is what the save
	// message is matched against, and the name is prose for a
	// window title rather than a path.
	if !strings.Contains(got, ".prog.") {
		t.Errorf("the reference is not a program's:\n%s", got)
	}
	if !strings.Contains(got, "a program named spell") {
		t.Errorf("the name is not the prose one:\n%s", got)
	}
	if !strings.Contains(got, "muf-code") {
		t.Errorf("the type is not muf-code:\n%s", got)
	}
}

// TestMcpeditRefusalsAreMCPErrors is the consequence of the fallback
// coming first: once MCP is known to be available, a permission
// failure is an out-of-band **error** rather than a line of text, so
// a client shows it in a dialog.
func TestMcpeditRefusalsAreMCPErrors(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@mcpprogram spell")
	h.out()
	h.send("q")
	h.out()

	h.send("#$#mcp version: 2.1 to: 2.1")
	key := authKeyFrom(t, h.out())
	for _, pkg := range []string{
		"dns-org-mud-moo-simpleedit", "org-fuzzball-notify"} {

		h.send(`#$#mcp-negotiate-can ` + key +
			` package: "` + pkg + `"` +
			` min-version: "1.0" max-version: "1.0"`)
		h.out()
	}

	// INTERNAL is what the **line** editor sets, and
	// `mcpedit_program` does not: sending the text to a client
	// locks nothing, so two `@mcpedit`s in a row both work. The
	// flag is set directly here, because the only way to set it
	// from a command is to enter the line editor -- which then
	// eats whatever is typed next.
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			for _, o := range []ref.Ref{ref.Ref(2)} {
				w.Get(o).Flags |= ref.Internal
			}
		}); err != nil {
		t.Fatal(err)
	}
	h.send("@mcpedit spell")
	got := h.out()
	if !strings.Contains(got, "#$#org-fuzzball-notify-error") {
		t.Errorf("the refusal was not an MCP error:\n%s", got)
	}
	if !strings.Contains(got, "currently being edited") {
		t.Errorf("the wrong refusal:\n%s", got)
	}
}
