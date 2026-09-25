package game

import (
	"strings"
	"testing"
)

// TestCommandVariableIsTheVerb pins upstream's two different strings.
// interp() sets variables[3] from match_cmdname and pushes match_args
// (interp.c:688 and :738), and process_command sets those to the
// command word and the rest of the line (game.c:695). Emerald passed
// the argument for both, so a program asking "command @" was told its
// own argument.
func TestCommandVariableIsTheVerb(t *testing.T) {
	h := newHarness(t)
	h.login()

	// An exit that runs a program matches its first word and
	// leaves the rest as the argument, so the two strings differ.
	h.installProgram(t, "shout", `: main
  "arg[" swap strcat "]" strcat me @ swap notify
  "cmd[" command @ strcat "]" strcat me @ swap notify
;`)

	h.send("shout hello there")
	got := h.out()
	if !strings.Contains(got, "cmd[shout]") {
		t.Errorf("COMMAND was not the verb:\n%s", got)
	}
	if !strings.Contains(got, "arg[hello there]") {
		t.Errorf("the argument was not the whole line:\n%s",
			got)
	}

	// An exit reached with no argument has an empty one, and the
	// verb is still the verb.
	h.send("shout")
	got = h.out()
	if !strings.Contains(got, "cmd[shout]") {
		t.Errorf("COMMAND was not the verb, no argument:\n%s",
			got)
	}
	if !strings.Contains(got, "arg[]") {
		t.Errorf("the argument was not empty:\n%s", got)
	}
}

// TestCommandVariableKeepsTheTypedCase is the detail that decides
// which string the verb comes from: match_exits copies out of
// md->match_name, what the player *typed*, not out of the exit's own
// name. So an exit called "Shout" reached by typing "shout" reports
// "shout".
func TestCommandVariableKeepsTheTypedCase(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "Shout", `: main
  "cmd[" command @ strcat "]" strcat me @ swap notify
;`)

	h.send("shout hello")
	if got := h.out(); !strings.Contains(got, "cmd[shout]") {
		t.Errorf("COMMAND used the exit's spelling:\n%s", got)
	}
}
