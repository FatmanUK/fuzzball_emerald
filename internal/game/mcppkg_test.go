package game

import (
	"strings"
	"testing"
)

// The three MCP packages this server now answers for itself. None is
// reachable from the golden harness, for the same reason `@mcpedit`
// is not: a message has to carry the authentication key the server
// issued, and it differs every run.

// negotiate brings the connection up with MCP and the named packages,
// and returns the key.
func negotiate(t *testing.T, h *harness, pkgs ...string) string {
	t.Helper()
	h.send("#$#mcp version: 2.1 to: 2.1")
	key := authKeyFrom(t, h.out())
	for _, p := range pkgs {
		h.send(`#$#mcp-negotiate-can ` + key +
			` package: "` + p + `"` +
			` min-version: "1.0" max-version: "1.0"`)
		h.out()
	}
	return key
}

// TestMCPLanguagesAnswers is the whole of mccpkg_languages: one
// message, one answer.
func TestMCPLanguagesAnswers(t *testing.T) {
	h := newHarness(t)
	h.login()
	key := negotiate(t, h, "org-fuzzball-languages")

	h.send("#$#org-fuzzball-languages-request " + key)
	got := h.out()
	if !strings.Contains(got,
		"#$#org-fuzzball-languages-supported") {
		t.Errorf("no answer:\n%s", got)
	}
	if !strings.Contains(got, "muf:7.0") {
		t.Errorf("the language is not named:\n%s", got)
	}
}

// TestMCPHelpAnswers covers the type words and the two failures a
// client can provoke. Upstream opens one of four files; Emerald's
// corpora are rows, so the type words name corpora instead -- and the
// "missing" branch, which upstream reaches when a file will not open,
// is reached here when the corpus is **empty**, which is what an
// unseeded world has.
func TestMCPHelpAnswers(t *testing.T) {
	h := newHarness(t)
	h.login()
	key := negotiate(t, h, "org-fuzzball-help")

	// A type that is not one of the four.
	h.send(`#$#org-fuzzball-help-request ` + key +
		` topic: "look" type: "nonsense"`)
	got := h.out()
	if !strings.Contains(got, "#$#org-fuzzball-help-error") ||
		!strings.Contains(got, "not a valid help type") {
		t.Errorf("a bad type said:\n%s", got)
	}

	// An empty corpus, which is upstream's unopenable file.
	h.send(`#$#org-fuzzball-help-request ` + key +
		` topic: "look" type: "help"`)
	h.sync()
	got = h.out()
	if !strings.Contains(got, "help is missing") {
		t.Errorf("an empty corpus said:\n%s", got)
	}

	h.seedHelp()

	// A topic that is there.
	h.send(`#$#org-fuzzball-help-request ` + key +
		` topic: "examine" type: "help"`)
	h.sync()
	got = h.out()
	if !strings.Contains(got, "#$#org-fuzzball-help-entry") {
		t.Errorf("no entry was sent:\n%s", got)
	}
	if !strings.Contains(got, "examine <thing>") {
		t.Errorf("the entry is not the text:\n%s", got)
	}

	// And one that is not.
	h.send(`#$#org-fuzzball-help-request ` + key +
		` topic: "nosuchtopic" type: "help"`)
	h.sync()
	got = h.out()
	if !strings.Contains(got, "#$#org-fuzzball-help-error") ||
		!strings.Contains(got, "no help available") {
		t.Errorf("a missing topic said:\n%s", got)
	}
}

// editSet sends a simpleedit save. The multiline argument has to be
// **declared** in the opening message -- `content*: ""` -- or the
// continuation lines are not continuation lines at all and arrive at
// the command parser as text.
func editSet(h *harness, key, tag, reference, valtype string,
	content ...string) {

	h.send(`#$#dns-org-mud-moo-simpleedit-set ` + key +
		` reference: "` + reference + `"` +
		` type: "` + valtype + `"` +
		` content*: "" _data-tag: "` + tag + `"`)
	for _, line := range content {
		h.send("#$#* " + tag + " content: " + line)
	}
	h.send("#$#: " + tag)
	h.sync()
}

// TestMCPSimpleEditSetsAProperty is the other end of `@mcpedit`, and
// the part that made it half a feature: a save had nowhere to go.
func TestMCPSimpleEditSetsAProperty(t *testing.T) {
	h := newHarness(t)
	h.login()
	key := negotiate(t, h, "dns-org-mud-moo-simpleedit",
		"org-fuzzball-notify")

	rf := strings.TrimPrefix(h.wizRef().String(), "#")

	// A string-list is joined with carriage returns into **one**
	// property, which is what makes a multi-line description one
	// value rather than a list.
	editSet(h, key, "t1", rf+".prop._/de", "string-list",
		"first line", "second line")
	h.out()

	h.send("examine me=**")
	got := h.out()
	if !strings.Contains(got, "first line") ||
		!strings.Contains(got, "second line") {
		t.Errorf("the description did not land:\n%s", got)
	}

	// A proplist is the other spelling: a count and numbered
	// children, which is what a client editing a list sends.
	editSet(h, key, "t2", rf+".proplist.notes", "string-list",
		"one", "two", "three")
	h.out()

	h.send("examine me=**")
	got = h.out()
	for _, want := range []string{"notes#/:3", "notes#/1",
		"one", "two", "three"} {

		if !strings.Contains(got, want) {
			t.Errorf("no %q in the list:\n%s", want, got)
		}
	}

	// An empty line in the middle of a list is stored as a
	// **space**: an empty value removes the property, which would
	// shorten the list under the count that was just written.
	editSet(h, key, "t2b", rf+".proplist.gap", "string-list",
		"top", "", "end")
	h.out()

	h.send("examine me=**")
	got = h.out()
	for _, want := range []string{"gap#/:3", "gap#/2"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in the list:\n%s", want, got)
		}
	}

	// A system property is refused, and the refusal is an
	// out-of-band error rather than a line.
	editSet(h, key, "t3", rf+".prop.@__sys__/x", "string", "nope")
	got = h.out()
	if !strings.Contains(got, "#$#org-fuzzball-notify-error") ||
		!strings.Contains(got, "Permission denied") {
		t.Errorf("a system property got through:\n%s", got)
	}

	// An unknown category says so, and a reference with no
	// category at all is a bad reference.
	editSet(h, key, "t4", rf+".nonsense.x", "string", "nope")
	if got := h.out(); !strings.Contains(got,
		"Unknown reference category") {
		t.Errorf("an unknown category said:\n%s", got)
	}
	editSet(h, key, "t5", "nonsense", "string", "nope")
	if got := h.out(); !strings.Contains(got,
		"Bad reference value") {
		t.Errorf("a bad reference said:\n%s", got)
	}
}

// TestMCPSimpleEditSavesAProgram is the save half of `@mcpedit`: the
// text comes back, is stored, and is compiled with the failure
// reported to whoever saved it -- which is `do_compile`'s own
// force_err_display argument.
func TestMCPSimpleEditSavesAProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@mcpprogram spell")
	h.out()
	h.send("q")
	h.out()

	key := negotiate(t, h, "dns-org-mud-moo-simpleedit",
		"org-fuzzball-notify")

	editSet(h, key, "p1", "2.prog.", "string-list",
		": main", `  me @ "it works" notify`, ";")
	h.out()

	h.send("@list spell")
	got := h.out()
	if !strings.Contains(got, "it works") {
		t.Errorf("the program was not saved:\n%s", got)
	}

	// And it compiled, which a plain save does not do.
	h.send("@find spell=l")
	h.out()
	editSet(h, key, "p2", "2.prog.", "string-list", ": main")
	got = h.out()
	if !strings.Contains(got, "Error") {
		t.Errorf("a broken program said nothing:\n%s", got)
	}
}
