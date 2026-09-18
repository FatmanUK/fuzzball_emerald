package golden

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// mcpScript negotiates MCP and then exchanges a message.
//
// The authentication key differs every run, so the script cannot name it. It
// uses "0" and relies on both servers rejecting the message the same way: what
// is being compared is the negotiation itself, which is the part a client
// depends on.
var mcpScript = Script{
	"#$#mcp version: 2.1 to: 2.1",
	`#$#mcp-negotiate-can 0 package: "org-fuzzball-simpleedit" min-version: "1.0" max-version: "1.0"`,
	"#$#not-a-real-package foo: bar",
	`#$"#$#this is quoted text`,
	"look",
}

// TestMCPNegotiationMatchesFuzzball compares the opening exchange.
func TestMCPNegotiationMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), ": main 1 pop ;")
	if err != nil {
		t.Fatal(err)
	}

	const quiet = 400 * time.Millisecond
	oracle, err := RunOracleQuiet(ctx, fx, mcpScript, quiet)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, mcpScript, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}

	for i, cmd := range mcpScript {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(maskKeys(want), maskKeys(got)); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}

// authKey matches the eight hex digits of an authentication key or data tag.
var authKey = regexp.MustCompile(`\b[0-9A-F]{8}\b`)

// maskKeys replaces the per-connection keys, which are random by design and
// differ on every run, with a fixed string.
//
// The key's presence and position are what matter — a client reads it out of
// the opening message and quotes it back on everything after — and those are
// still compared.
func maskKeys(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		kept = append(kept, authKey.ReplaceAllString(line, "<key>"))
	}
	return strings.Join(kept, "\n")
}
