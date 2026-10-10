package game

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// `internal/logging` replaced *where* a log line goes; the decisions
// about *whether* were never ported, so five parameters had no
// reader. None of this is oracle-reachable: a log line is not a
// transcript.

// logHarness is the harness with its log captured.
type logHarness struct {
	*harness
	buf *bytes.Buffer
}

func newLogHarness(t *testing.T) *logHarness {
	t.Helper()
	buf := &bytes.Buffer{}
	h := newHarnessWithLogger(t, slog.New(
		slog.NewTextHandler(buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	return &logHarness{harness: h, buf: buf}
}

// logged returns everything written since the last call.
func (h *logHarness) logged() string {
	h.t.Helper()
	h.syncEngine()
	out := h.buf.String()
	h.buf.Reset()
	return out
}

// quell takes the test wizard's powers away, which is what the
// command-log gate actually asks about: `Wizard(OWNER(player))`
// excludes QUELL. A wizard cannot clear its own W bit at all --
// `01d373b` made sure of that -- so quelling is the only route to a
// mortal here, and it is the faithful one.
func (h *logHarness) quell() {
	h.t.Helper()
	h.send("@set me=Q")
	h.logged()
}

// unquell puts them back, because a quelled wizard may not @tune.
func (h *logHarness) unquell() {
	h.t.Helper()
	h.send("@set me=!Q")
	h.logged()
}

// TestLogCommandsGate is the gate itself: `log_commands` **or** the
// player's owner being an unquelled wizard, so a wizard is logged
// whatever the parameter says.
func TestLogCommandsGate(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	h.send("@tune log_commands=no")
	h.logged()

	// Still logged, because the player is a wizard.
	h.send("look")
	if got := h.logged(); !strings.Contains(got, "verb=look") {
		t.Errorf("a wizard was not logged:\n%s", got)
	}

	h.quell()
	h.send("look")
	if got := h.logged(); strings.Contains(got, "verb=look") {
		t.Errorf("a mortal was logged with the gate off:\n%s",
			got)
	}

	h.unquell()
	h.send("@tune log_commands=yes")
	h.quell()
	h.send("look")
	if got := h.logged(); !strings.Contains(got, "verb=look") {
		t.Errorf("the gate did not turn back on:\n%s", got)
	}
}

// TestLogMasksPasswordsUpstreamsWay is the three-way mask, which is
// applied to the **whole line** and truncates differently in each
// case -- and which keeps a `@pcreate`'s player name where this used
// to redact the argument whole.
func TestLogMasksPasswordsUpstreamsWay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"@password old=new", "@password [***]"},
		{"@passwordfoo bar", "@password [***]"},
		{"@newpassword Bob=hunter2",
			"@newpassword [***]"},
		{"@pcreate Bob=hunter2", "@pcreate Bob=[***]"},
		{"@pcreate Bob", "@pcreate Bob"},
		{"look", "look"},
	}
	for _, c := range cases {
		if got := maskSecrets(c.in); got != c.want {
			t.Errorf("maskSecrets(%q) = %q, want %q",
				c.in, got, c.want)
		}
	}
}

// TestLogInteractive is the editor and a READ, neither of which was
// logged at all.
func TestLogInteractive(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	// Both parameters have to be set **before** the editor opens:
	// once it is open every line goes to it, so a @tune typed
	// there is program text.
	h.send("@tune log_interactive=no")
	h.send("@program spell")
	h.logged()

	h.send("a line of program")
	if got := h.logged(); strings.Contains(got, "interactive") {
		t.Errorf("logged with the parameter off:\n%s", got)
	}
	h.send("x")
	h.send("@tune log_interactive=yes")
	h.send("@edit spell")
	h.logged()

	h.send("another line")
	got := h.logged()
	if !strings.Contains(got, "mode=[INTERP]") {
		t.Errorf("the editor was not logged:\n%s", got)
	}
	if !strings.Contains(got, "another line") {
		t.Errorf("the line was not logged:\n%s", got)
	}
	h.send("x")
	h.logged()
}

// TestLogFailedCommands is the `bad:` label's own line, which is
// **not** gated on log_commands and **is** gated on the player not
// controlling the room they are standing in.
func TestLogFailedCommands(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	h.send("@tune log_failed_commands=yes")
	h.logged()

	// The wizard controls the room, so nothing is recorded --
	// which is upstream's second condition and reads like an
	// oversight until you notice it spares a builder fumbling in
	// their own workshop.
	h.send("flibble")
	if got := h.logged(); strings.Contains(got, "HUH") {
		t.Errorf("logged in a room the player controls:\n%s",
			got)
	}

	// Chown the room elsewhere and the same typo is recorded.
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			o := w.Get(w.Get(h.wizRef()).Location)
			o.Owner = ref.GlobalEnvironment
		}); err != nil {
		t.Fatal(err)
	}
	h.quell()
	h.send("flibble")
	got := h.logged()
	if !strings.Contains(got, "HUH") {
		t.Errorf("a typo in somebody else's room:\n%s", got)
	}
	if !strings.Contains(got, "verb=flibble") {
		t.Errorf("the command was not named:\n%s", got)
	}

	h.unquell()
	h.send("@tune log_failed_commands=no")
	h.quell()
	h.send("flibble")
	if got := h.logged(); strings.Contains(got, "HUH") {
		t.Errorf("logged with the parameter off:\n%s", got)
	}
}

// TestLogSlowCommands is `cmd_log_threshold_msec`, which nothing
// timed. The comparison is **greater than**, so a threshold of zero
// logs every command that took any measurable time.
func TestLogSlowCommands(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	h.send("@tune cmd_log_threshold_msec=600000")
	h.logged()

	h.send("look")
	if got := h.logged(); strings.Contains(got,
		"slow command") {

		t.Errorf("a look took ten minutes?\n%s", got)
	}

	h.send("@tune cmd_log_threshold_msec=0")
	h.logged()
	h.send("look")
	got := h.logged()
	if !strings.Contains(got, "slow command") {
		t.Errorf("a threshold of zero logged nothing:\n%s",
			got)
	}
	if !strings.Contains(got, "WIZ:") {
		t.Errorf("no whowhere prefix:\n%s", got)
	}
}

// TestSlowCommandThresholdIsExclusive is the comparison itself, which
// is **greater than** and not "at least": a command that took exactly
// the threshold is not logged. A real clock never produces that
// exactly, so the duration is handed in rather than measured -- and
// the clock *is* the real one, upstream's `gettimeofday`, since a
// frozen one would make every command take no time and a threshold of
// zero would then log nothing at all.
func TestSlowCommandThresholdIsExclusive(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	h.send("@tune cmd_log_threshold_msec=100")
	h.logged()

	for _, tc := range []struct {
		took time.Duration
		want bool
	}{
		{99 * time.Millisecond, false},
		{100 * time.Millisecond, false},
		{101 * time.Millisecond, true},
	} {
		err := h.engine.Do(context.Background(),
			func(w *world.World) {
				h.s.logSlowCommand(w, h.d, "look",
					tc.took)
			})
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Contains(h.logged(), "slow command")
		if got != tc.want {
			t.Errorf("%v logged = %v, want %v",
				tc.took, got, tc.want)
		}
	}
}

// TestLogProgramText is `log_program_text`, which `logging.Program`
// had no caller for at all.
func TestLogProgramText(t *testing.T) {
	h := newLogHarness(t)
	h.login()
	h.send("@tune log_programs=no")
	h.send("@program spell")
	h.send("i")
	h.send(`  me @ "secret source" notify`)
	h.send(".")
	h.logged()

	h.send("q")
	if got := h.logged(); strings.Contains(got,
		"secret source") {

		t.Errorf("logged with the parameter off:\n%s", got)
	}

	h.send("@tune log_programs=yes")
	h.send("@edit spell")
	h.logged()
	h.send("q")
	got := h.logged()
	if !strings.Contains(got, "program text") {
		t.Errorf("the save was not logged:\n%s", got)
	}
	if !strings.Contains(got, "secret source") {
		t.Errorf("the text was not logged:\n%s", got)
	}
}

// TestWhowherePrefix is `whowhere` (`log.c:343`), which every one of
// these lines carries: a WIZ marker for a wizard-owned object, the
// object's own name when it is not a player, its owner, and the room.
func TestWhowherePrefix(t *testing.T) {
	h := newHarness(t)
	h.login()

	var player, thing string
	err := h.engine.Do(context.Background(),
		func(w *world.World) {
			wiz := h.wizRef()
			player = whowhere(w, wiz)
			o := w.Create("puppet", ref.TypeThing, wiz)
			if err := w.MoveTo(o.Ref,
				w.Get(wiz).Location); err != nil {
				t.Error(err)
			}
			thing = whowhere(w, o.Ref)
		})
	if err != nil {
		t.Fatal(err)
	}
	want := "WIZ: Wizard(#1) in The Study(#0)"
	if player != want {
		t.Errorf("a player: %q, want %q", player, want)
	}
	if !strings.Contains(thing, "puppet owned by Wizard(") {
		t.Errorf("a thing: %q", thing)
	}
}

// newHarnessWithLogger is newHarness with the server's logger
// supplied, which is how the log can be read back.
func newHarnessWithLogger(t *testing.T,
	log *slog.Logger) *harness {

	t.Helper()
	w := world.New()

	room := w.Create("The Study", ref.TypeRoom, ref.God)
	wiz := w.Create("Wizard", ref.TypePlayer, ref.Nothing)
	wiz.Owner = wiz.Ref
	wiz.Flags |= ref.Wizard | ref.Builder
	wiz.Flags = wiz.Flags.SetMLevel(3)
	wiz.Home = room.Ref
	hashed, err := password.Hash("secret")
	if err != nil {
		t.Fatal(err)
	}
	wiz.PasswordHash = hashed
	if err := w.MoveTo(wiz.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	w.SetProp(room.Ref, propDesc, props.Value{
		Type: props.String, Str: "A quiet study."})

	engine := world.NewEngine(w,
		world.Options{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, s: New(engine, Options{Logger: log}),
		engine: engine, w: w}
	base := time.Unix(1_700_000_000, 0).UTC()
	w.SetClock(func() time.Time {
		return base.Add(time.Duration(h.clockOffset.Load()))
	})

	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()

	d, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.d = d

	t.Cleanup(func() {
		d.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the world goroutine did not stop")
		}
	})
	return h
}
