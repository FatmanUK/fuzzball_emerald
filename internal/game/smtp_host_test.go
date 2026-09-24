package game

import (
	"strings"
	"testing"
)

func TestBuildMessageHeaders(t *testing.T) {
	cfg := smtpSettings{fromAddr: "muck@example.com", fromName: "The MUCK"}
	msg := buildMessage(cfg, "player@example.net", "A Player", "Hello", "line one\r\nline two")

	for _, want := range []string{
		`From: "The MUCK" <muck@example.com>`,
		`To: "A Player" <player@example.net>`,
		"Subject: Hello",
		"MIME-Version: 1.0",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q:\n%s", want, msg)
		}
	}
	// The body follows a blank line, as RFC 5322 requires.
	head, body, ok := strings.Cut(msg, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator:\n%s", msg)
	}
	if body != "line one\r\nline two" {
		t.Errorf("body = %q", body)
	}
	if strings.Contains(head, "line one") {
		t.Error("the body leaked into the headers")
	}
}

// TestHeaderInjectionIsStripped is the check that matters: a subject
// and a recipient name are both chosen by a MUF program, so a newline
// in either must not be able to add headers of its own.
func TestHeaderInjectionIsStripped(t *testing.T) {
	cfg := smtpSettings{fromAddr: "muck@example.com", fromName: "The MUCK"}
	msg := buildMessage(cfg,
		"player@example.net",
		"Name\r\nBcc: victim@example.org",
		"Subject\r\nBcc: other@example.org",
		"body")

	// The injected text survives as *data* — folded into the
	// value it came from — which is fine. What must not happen
	// is a new header line, so the check is on what starts a line
	// rather than on the text appearing at all.
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Errorf("a header was injected through a name or subject:\n%s", head)
		}
	}
	// And the header block is still exactly the lines
	// buildMessage writes.
	if got, want := len(strings.Split(head, "\r\n")), 6; got != want {
		t.Errorf("header block has %d lines, want %d:\n%s", got, want, head)
	}
}

func TestSMTPAuthSelection(t *testing.T) {
	// No user means no authentication, whatever the mode says.
	if a := smtpAuth(smtpSettings{authMode: smtpAuthPlain}); a != nil {
		t.Error("authentication was attempted with no user set")
	}
	if a := smtpAuth(smtpSettings{user: "u", authMode: smtpAuthNone}); a != nil {
		t.Error("authentication was attempted with the mode set to none")
	}
	for _, mode := range []int{smtpAuthCramMD5, smtpAuthPlain, smtpAuthLogin} {
		if a := smtpAuth(smtpSettings{user: "u", password: "p", authMode: mode}); a == nil {
			t.Errorf("mode %d produced no authentication", mode)
		}
	}
}
