package game

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// securityRecords captures the audit trail a harness produces.
type securityRecords struct {
	buf *bytes.Buffer
}

// messages returns the "msg" of every record on the security channel,
// so a test asserts what was audited rather than how it was worded.
func (r *securityRecords) messages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(r.buf.String(), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if rec["channel"] == "security" {
			out = append(out, rec["msg"].(string))
		}
	}
	return out
}

func (r *securityRecords) has(t *testing.T, want string) bool {
	t.Helper()
	for _, m := range r.messages(t) {
		if m == want {
			return true
		}
	}
	return false
}

// auditing points a harness's server at a JSON logger so its records
// can be read back.
func auditing(t *testing.T, h *harness) *securityRecords {
	t.Helper()
	buf := &bytes.Buffer{}
	h.s.log = slog.New(slog.NewJSONHandler(buf, nil))
	return &securityRecords{buf: buf}
}

func TestFailedLoginIsAudited(t *testing.T) {
	h := newHarness(t)
	rec := auditing(t, h)

	h.send("connect Wizard wrongpassword")
	h.out()

	if !rec.has(t, "failed login") {
		t.Errorf("a failed login was not audited: %v", rec.messages(t))
	}
}

func TestSuccessfulLoginIsAudited(t *testing.T) {
	h := newHarness(t)
	rec := auditing(t, h)

	h.send("connect Wizard secret")
	h.out()

	if !rec.has(t, "connected") {
		t.Errorf("a login was not audited: %v", rec.messages(t))
	}
}

func TestPasswordChangeIsAudited(t *testing.T) {
	h := newHarness(t)
	h.login()
	rec := auditing(t, h)

	h.send("@password wrong=whatever")
	h.out()
	if !rec.has(t, "failed password change") {
		t.Errorf("a refused password change was not audited: %v", rec.messages(t))
	}

	h.send("@password secret=newsecret")
	h.out()
	if !rec.has(t, "password changed") {
		t.Errorf("a password change was not audited: %v", rec.messages(t))
	}
}

// TestRefusedWizardCommandIsAudited covers the case the channel
// exists for: one of these is a typo, and a run of them is someone
// trying the doors.
func TestRefusedWizardCommandIsAudited(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Drop the wizard bit so the next command is refused.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		o := w.Get(h.wizRef())
		o.Flags &^= ref.Wizard
	}); err != nil {
		t.Fatal(err)
	}

	rec := auditing(t, h)
	h.send("@toad Wizard")
	h.out()

	if !rec.has(t, "refused a wizard command") {
		t.Errorf("a refused wizard command was not audited: %v", rec.messages(t))
	}
}
