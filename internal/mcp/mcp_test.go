package mcp

import (
	"strings"
	"testing"
	"time"
)

// testFrame returns a frame that collects what it sends.
func testFrame(packages ...Package) (*Frame, *[]string) {
	var out []string
	f := NewFrame(func(s string) { out = append(out, s) }, packages)
	return f, &out
}

// negotiate takes a frame through the opening exchange and returns
// the key the server issued.
func negotiate(t *testing.T, f *Frame, out *[]string) string {
	t.Helper()
	if _, pass := f.ProcessInput("#$#mcp version: 2.1 to: 2.1"); pass {
		t.Fatal("the opening message was passed through as text")
	}
	if !f.Enabled() {
		t.Fatal("MCP is not enabled after the opening exchange")
	}
	for _, line := range *out {
		if strings.HasPrefix(line, "#$#mcp ") {
			if key, ok := argFromLine(line, "authentication-key"); ok {
				return key
			}
		}
	}
	t.Fatal("no authentication key was issued")
	return ""
}

// argFromLine pulls a quoted or bare argument out of a rendered
// message.
func argFromLine(line, name string) (string, bool) {
	i := strings.Index(line, name+": ")
	if i < 0 {
		return "", false
	}
	rest := line[i+len(name)+2:]
	if v, _, ok := readQuoted(rest); ok {
		return v, true
	}
	v, _, ok := readUnquoted(rest)
	return v, ok
}

func TestTextPassesThroughUntilNegotiated(t *testing.T) {
	f, _ := testFrame()

	for _, line := range []string{"look", "say hello", "#$#not-mcp foo"} {
		if got, pass := f.ProcessInput(line); !pass ||
			got != line {
			t.Errorf("ProcessInput(%q) = %q, %v; want the line unchanged", line, got, pass)
		}
	}
}

func TestNegotiationAnnouncesPackages(t *testing.T) {
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball-gui", MinVer: Version{1, 0}, MaxVer: Version{1, 3}},
	)
	negotiate(t, f, out)

	joined := strings.Join(*out, "\n")
	for _, want := range []string{
		`#$#mcp version: "2.1" to: "2.1"`,
		`package: "org-fuzzball-gui"`,
		`min-version: "1.0" max-version: "1.3"`,
		"mcp-negotiate-end",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("negotiation did not contain %q:\n%s", want, joined)
		}
	}
	if f.Version() != (Version{2, 1}) {
		t.Errorf("negotiated version %v, want 2.1", f.Version())
	}
}

func TestMessagesNeedTheAuthenticationKey(t *testing.T) {
	var got []*Message
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball-gui", MinVer: Version{1, 0}, MaxVer: Version{1, 3},
			Handle: func(_ *Frame, m *Message, _ Version) { got = append(got, m) }},
	)
	key := negotiate(t, f, out)

	// A message carrying the wrong key is not a message. It comes
	// back as text rather than being obeyed, which is the whole
	// point of the key: otherwise anything a world echoed could
	// drive a client.
	line := "#$#org-fuzzball-gui-ctrl-value 00000000 id: \"x\""
	if text, pass := f.ProcessInput(line); !pass || text != line {
		t.Errorf("a message with the wrong key was accepted")
	}
	if len(got) != 0 {
		t.Fatalf("a forged message reached the handler: %+v", got[0])
	}

	line = "#$#org-fuzzball-gui-ctrl-value " + key + " id: \"widget\" value: \"7\""
	if _, pass := f.ProcessInput(line); pass {
		t.Fatal("a valid message was passed through as text")
	}
	if len(got) != 1 {
		t.Fatalf("the handler saw %d messages, want 1", len(got))
	}
	if got[0].Package != "org-fuzzball-gui" ||
		got[0].Name != "ctrl-value" {
		t.Errorf("message parsed as %q / %q", got[0].Package, got[0].Name)
	}
	if v, _ := got[0].Arg("id"); v != "widget" {
		t.Errorf("id = %q, want widget", v)
	}
}

func TestMultilineArgumentsAreReassembled(t *testing.T) {
	var got *Message
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "dns-org-mud-moo-simpleedit", MinVer: Version{1, 0}, MaxVer: Version{1, 0},
			Handle: func(_ *Frame, m *Message, _ Version) { got = m }},
	)
	key := negotiate(t, f, out)

	for _, line := range []string{
		`#$#dns-org-mud-moo-simpleedit-set ` + key + ` reference: "#3" type: "program" content*: "" _data-tag: 1234`,
		`#$#* 1234 content: : main`,
		`#$#* 1234 content:   "hi" .tell`,
		`#$#* 1234 content: ;`,
		`#$#: 1234`,
	} {
		if _, pass := f.ProcessInput(line); pass {
			t.Fatalf("line was passed through as text: %q", line)
		}
	}
	if got == nil {
		t.Fatal("the message never completed")
	}
	lines, ok := got.Lines("content")
	if !ok {
		t.Fatal("the content argument is missing")
	}
	want := []string{": main", `  "hi" .tell`, ";"}
	if len(lines) != len(want) {
		t.Fatalf("content has %d lines, want %d: %q", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			// Leading spaces matter: this is how program
			// source survives the round trip.
			t.Errorf("content line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestSendSplitsLongAndMultilineValues(t *testing.T) {
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball-simpleedit", MinVer: Version{1, 0}, MaxVer: Version{1, 0}},
	)
	key := negotiate(t, f, out)
	f.ProcessInput(`#$#mcp-negotiate-can ` + key +
		` package: "org-fuzzball-simpleedit" min-version: "1.0" max-version: "1.0"`)
	*out = nil

	msg := NewMessage("org-fuzzball-simpleedit", "set").
		AddArg("reference", "#3").
		AddMultiline("content", []string{"line one", "line two"})
	if err := f.SendMessage(msg); err != nil {
		t.Fatal(err)
	}

	// The opening line, one line per line of content, and the
	// closing tag.
	if len(*out) != 4 {
		t.Fatalf("sent %d lines, want 4:\n%s", len(*out), strings.Join(*out, "\n"))
	}
	first := (*out)[0]
	if !strings.Contains(first, `content*: ""`) ||
		!strings.Contains(first, "_data-tag: ") {
		t.Errorf("the opening line does not defer the content:\n%s", first)
	}
	if !strings.Contains((*out)[1], "content: line one") {
		t.Errorf("first continuation line is %q", (*out)[1])
	}
	if !strings.HasPrefix((*out)[3], "#$#: ") {
		t.Errorf("the message was not closed: %q", (*out)[3])
	}
}

func TestSendRefusesAPackageTheClientDidNotAgreeTo(t *testing.T) {
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball-gui", MinVer: Version{1, 0}, MaxVer: Version{1, 3}},
	)
	negotiate(t, f, out)

	err := f.SendMessage(NewMessage("org-fuzzball-gui", "dlog-create"))
	if err == nil {
		t.Error("sending to an unagreed package should fail")
	}
}

func TestQuotedInbandTextIsUnquoted(t *testing.T) {
	f, out := testFrame(Package{Name: NegotiatePackage, MinVer: Version{1, 0},
		MaxVer: Version{2, 0}, Handle: NegotiateHandler})
	negotiate(t, f, out)

	got, pass := f.ProcessInput(`#$"#$#this is not a message`)
	if !pass || got != "#$#this is not a message" {
		t.Errorf("ProcessInput unquoted to %q, %v", got, pass)
	}

	// And the other direction: text that would look like a
	// message is quoted on the way out.
	*out = nil
	f.SendInband("#$#pretending to be a message")
	if len(*out) != 1 ||
		!strings.HasPrefix((*out)[0], QuotePrefix) {
		t.Errorf("outgoing text was not quoted: %q", *out)
	}
}

func TestVersionSelection(t *testing.T) {
	cases := []struct {
		minA, maxA, minB, maxB, want Version
	}{
		{Version{2, 1}, Version{2, 1}, Version{1, 0}, Version{2, 1}, Version{2, 1}},
		{Version{2, 1}, Version{2, 1}, Version{2, 1}, Version{2, 1}, Version{2, 1}},
		{Version{2, 1}, Version{2, 1}, Version{1, 0}, Version{1, 0}, Version{}},
		{Version{1, 0}, Version{2, 0}, Version{1, 5}, Version{3, 0}, Version{2, 0}},
	}
	for _, tc := range cases {
		if got := SelectVersion(tc.minA, tc.maxA, tc.minB, tc.maxB); got != tc.want {
			t.Errorf("SelectVersion(%v..%v, %v..%v) = %v, want %v",
				tc.minA, tc.maxA, tc.minB, tc.maxB, got, tc.want)
		}
	}
}

func TestPackageNamesResolveToTheLongestMatch(t *testing.T) {
	var seen string
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball", MinVer: Version{1, 0}, MaxVer: Version{1, 0},
			Handle: func(_ *Frame, m *Message, _ Version) { seen = "short:" + m.Name }},
		Package{Name: "org-fuzzball-gui", MinVer: Version{1, 0}, MaxVer: Version{1, 3},
			Handle: func(_ *Frame, m *Message, _ Version) { seen = "long:" + m.Name }},
	)
	key := negotiate(t, f, out)

	// Package names are hierarchical, so the longest registered
	// prefix wins: this belongs to org-fuzzball-gui, not to
	// org-fuzzball.
	f.ProcessInput("#$#org-fuzzball-gui-ctrl-value " + key + ` id: "x"`)
	if seen != "long:ctrl-value" {
		t.Errorf("message went to %q, want the gui package", seen)
	}
}

// TestAHandlerMaySendWhileHandling checks that a package handler can
// answer the message it was given.
//
// This is the obvious thing for a handler to do, and the frame has to
// have released its lock before calling one: holding it across the
// callback turns every such reply into a deadlock, which is exactly
// what the first version of this did.
func TestAHandlerMaySendWhileHandling(t *testing.T) {
	f, out := testFrame(
		Package{Name: NegotiatePackage, MinVer: Version{1, 0}, MaxVer: Version{2, 0},
			Handle: NegotiateHandler},
		Package{Name: "org-fuzzball-help", MinVer: Version{1, 0}, MaxVer: Version{1, 0},
			Handle: func(fr *Frame, m *Message, _ Version) {
				// Reading state and sending both take
				// the lock.
				_ = fr.Enabled()
				_ = fr.Supports("org-fuzzball-help")
				_ = fr.SendMessage(NewMessage("org-fuzzball-help", "reply").
					AddArg("topic", "yes"))
			}},
	)
	key := negotiate(t, f, out)
	f.ProcessInput(`#$#mcp-negotiate-can ` + key +
		` package: "org-fuzzball-help" min-version: "1.0" max-version: "1.0"`)
	*out = nil

	done := make(chan struct{})
	go func() {
		f.ProcessInput(`#$#org-fuzzball-help-request ` + key + ` topic: "building"`)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler deadlocked against the frame's own lock")
	}

	if len(*out) != 1 ||
		!strings.Contains((*out)[0], "org-fuzzball-help-reply") {
		t.Errorf("the handler's reply did not go out: %q", *out)
	}
}
