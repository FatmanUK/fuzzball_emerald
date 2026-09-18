package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// harness runs a server over an in-memory descriptor, with no sockets.
//
// Output is drained on demand rather than by a background goroutine: a
// descriptor's Send happens synchronously on the world goroutine, so once a
// round trip through the engine has completed, everything the command produced
// is already sitting in the channel buffer. Collecting it in the background
// would race with the test reading it.
type harness struct {
	t      *testing.T
	s      *Server
	engine *world.Engine
	w      *world.World
	d      *session.Descriptor
}

// newHarness builds a small world and connects one client to it.
func newHarness(t *testing.T) *harness {
	t.Helper()

	w := world.New()
	w.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })

	room := w.Create("The Study", ref.TypeRoom, ref.God)
	wiz := w.Create("Wizard", ref.TypePlayer, ref.Nothing)
	wiz.Owner = wiz.Ref
	// A wizard with no mucker bits has mucker level 0, so programs it owns
	// are capped there: find_mlev takes the lower of the program's level
	// and its owner's.
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
	w.SetProp(room.Ref, propDesc, props.Value{Type: props.String, Str: "A quiet study."})

	engine := world.NewEngine(w, world.Options{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()

	h := &harness{t: t, s: New(engine, Options{}), engine: engine, w: w}

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

// send delivers a line and waits for the world to finish with it.
func (h *harness) send(line string) {
	h.t.Helper()
	h.s.Input(h.d, line)
	h.sync()
}

// sync waits until everything queued before now has run.
func (h *harness) sync() {
	h.t.Helper()
	// A no-op submitted after the command runs after it, so its completion
	// proves the command finished and all its output was queued.
	if err := h.engine.Do(context.Background(), func(*world.World) {}); err != nil {
		if h.d.Closed() {
			return // the command disconnected us, which is fine
		}
		h.t.Fatal(err)
	}
}

// out returns everything sent since the last call.
func (h *harness) out() string { return drainDescriptor(h.d) }

// drainDescriptor takes whatever is buffered for a descriptor without waiting.
func drainDescriptor(d *session.Descriptor) string {
	var lines []string
	for {
		select {
		case line, ok := <-d.Output():
			if !ok {
				return strings.Join(lines, "\n")
			}
			lines = append(lines, line)
		default:
			return strings.Join(lines, "\n")
		}
	}
}

// login authenticates as the test wizard.
func (h *harness) login() {
	h.t.Helper()
	h.send("connect Wizard secret")
	h.out()
}

func (h *harness) wizRef() ref.Ref {
	r, ok := h.w.PlayerNamed("Wizard")
	if !ok {
		h.t.Fatal("the test wizard is missing")
	}
	return r
}

func TestLoginAndLook(t *testing.T) {
	h := newHarness(t)
	h.send("connect Wizard secret")
	got := h.out()
	if !strings.Contains(got, "The Study") {
		t.Errorf("login did not show the room:\n%s", got)
	}
	if !strings.Contains(got, "A quiet study.") {
		t.Errorf("login did not show the description:\n%s", got)
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	h := newHarness(t)
	h.send("connect Wizard wrong")
	got := h.out()
	if strings.Contains(got, "The Study") {
		t.Errorf("a bad password logged in:\n%s", got)
	}
	if !strings.Contains(got, "does not exist") {
		t.Errorf("no failure message:\n%s", got)
	}
}

// TestEveryCommandRuns drives each command with plausible arguments and with
// none at all.
//
// The point is not the output but the absence of a panic: a handler that
// panics is caught by the engine, so without a check like this a broken
// command looks like one that silently does nothing. That is exactly how a
// wrong @tune parameter name hid during development.
func TestEveryCommandRuns(t *testing.T) {
	h := newHarness(t)
	h.login()

	lines := []string{
		// No arguments at all, which is the usual crash path.
		"look", "say", "pose", "page", "whisper", "go", "home",
		"inventory", "get", "drop", "examine",
		"@create", "@dig", "@open", "@link", "@unlink", "@name",
		"@describe", "@set", "@password", "@find", "@teleport",
		"@recycle", "@tune", "@version",

		// With arguments.
		"look me", "look here", "look nonesuch",
		"say hello", "pose waves", ":waves", "\"hello",
		"page Wizard=hi", "whisper Wizard=hi",
		"examine me", "examine here",
		"@create a widget", "@dig A Room", "@open out=here",
		"@describe me=A test wizard.", "@set me=dark", "@set me=!dark",
		"@set me=_test:value", "@set me=M3",
		"@name me=Wizard", "@find widget", "@tune #list penny",
		"@tune penny", "@tune penny=Groat",
		"@teleport me=here", "get widget", "drop widget",
		"inventory", "@recycle widget",

		// Nonsense that must be handled gracefully.
		"@nosuchcommand", "nosuchverb", "@", "!", "!look",
		"@set me=nosuchflag", "@link me=nowhere",
		strings.Repeat("x", 5000),
	}

	for _, line := range lines {
		h.send(line)
		got := h.out()
		if strings.Contains(got, "Something went wrong") {
			t.Errorf("command %q panicked:\n%s", line, got)
		}
	}
}

func TestQuitIsCaseSensitive(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Lowercase "quit" is not the interface command: it falls through so a
	// world can shadow it with an exit, which the starter world does.
	h.send("quit")
	if h.d.Closed() {
		t.Fatal("lowercase quit disconnected; it must fall through to exits")
	}
	got := h.out()
	if !strings.Contains(got, "I don't understand") {
		t.Errorf("lowercase quit = %q, want it unhandled", got)
	}

	h.send("QUIT")
	if !h.d.Closed() {
		t.Error("QUIT did not disconnect")
	}
}

func TestWhoIsCaseSensitiveAndTakesAFilter(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("WHO")
	got := h.out()
	if !strings.Contains(got, "Wizard") || !strings.Contains(got, "player connected") {
		t.Errorf("WHO output:\n%s", got)
	}

	// A filter that matches nobody.
	h.send("WHO nosuchplayer")
	got = h.out()
	if !strings.Contains(got, "0 players connected") {
		t.Errorf("filtered WHO:\n%s", got)
	}

	// Lowercase "who" is not the interface command.
	h.send("who")
	got = h.out()
	if strings.Contains(got, "player connected") {
		t.Errorf("lowercase who should not be the interface command:\n%s", got)
	}
}

func TestExitsShadowBuiltins(t *testing.T) {
	h := newHarness(t)
	h.login()
	wiz := h.wizRef()

	// A world may define its own "look"; the player's world wins.
	var exit ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(wiz).Location
		e := w.Create("look", ref.TypeExit, wiz)
		e.Dest = []ref.Ref{here}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Error(err)
		}
		w.SetProp(e.Ref, propSucc, props.Value{
			Type: props.String, Str: "The exit fired.",
		})
		exit = e.Ref
	}); err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look")
	got := h.out()
	if !strings.Contains(got, "The exit fired.") {
		t.Errorf("the exit did not shadow the built-in:\n%s", got)
	}

	// A wizard's '!' prefix skips exit matching to reach the built-in.
	h.send("!look")
	got = h.out()
	if strings.Contains(got, "The exit fired.") {
		t.Errorf("! should have skipped the exit:\n%s", got)
	}
	if !strings.Contains(got, "The Study") {
		t.Errorf("! did not reach the built-in look:\n%s", got)
	}
	_ = exit
}

func TestAtCommandPrefixMatching(t *testing.T) {
	h := newHarness(t)
	h.login()

	// An unambiguous prefix reaches its command.
	h.send("@vers")
	if got := h.out(); !strings.Contains(got, "Fuzzball Emerald") {
		t.Errorf("@vers = %q, want the version", got)
	}

	// An ambiguous prefix matches nothing rather than picking arbitrarily.
	// "@d" could be @dig, @describe or @dump.
	h.send("@d")
	if got := h.out(); !strings.Contains(got, "don't know that command") {
		t.Errorf("@d = %q, want it refused as ambiguous", got)
	}
}

func TestBuildAndMove(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.send("@dig Cellar")
	got := h.out()
	if !strings.Contains(got, "created with number") {
		t.Fatalf("@dig failed:\n%s", got)
	}
	cellar := dbrefFrom(t, got)

	h.send("@open down;d=" + cellar)
	got = h.out()
	if !strings.Contains(got, "Linked to") {
		t.Fatalf("@open failed:\n%s", got)
	}

	// Both the name and the alias work.
	h.send("down")
	if got := h.out(); !strings.Contains(got, "Cellar") {
		t.Errorf("going down failed:\n%s", got)
	}
	h.send("@teleport me=here")
	h.out()
}

func TestSpeechReachesOthersInTheRoom(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A second connection for the same player is not another listener, so
	// bring in a genuinely separate character.
	var other ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		o := w.Create("Bystander", ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		other = o.Ref
	}); err != nil {
		t.Fatal(err)
	}

	d2, err := h.s.Connect(session.TransportLine, "test")
	if err != nil {
		t.Fatal(err)
	}
	// Bind the second descriptor without going through a password.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Hub().Bind(d2, other, w.Now())
	}); err != nil {
		t.Fatal(err)
	}
	drainDescriptor(d2)

	h.send("say hello there")

	got := drainDescriptor(d2)
	if !strings.Contains(got, `Wizard says, "hello there"`) {
		t.Errorf("the bystander did not hear the speech:\n%s", got)
	}
	d2.Close()
}

func TestOutputOverflowDisconnectsRatherThanStalling(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A descriptor nobody is draining must not be able to block the world.
	d2, err := h.s.Connect(session.TransportLine, "stuck")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10_000 && !d2.Closed(); i++ {
		d2.Send("flood")
	}
	if !d2.Closed() {
		t.Error("a descriptor that fell behind should have been closed")
	}
	if !d2.Overflowed() {
		t.Error("the overflow should have been recorded")
	}

	// The world is still serving.
	h.send("look")
	if got := h.out(); !strings.Contains(got, "The Study") {
		t.Errorf("the world stopped serving after an overflow:\n%s", got)
	}
}

// dbrefFrom pulls a "#123" out of a creation message.
func dbrefFrom(t *testing.T, s string) string {
	t.Helper()
	i := strings.Index(s, "number #")
	if i < 0 {
		t.Fatalf("no dbref in %q", s)
	}
	rest := s[i+len("number "):]
	end := strings.IndexAny(rest, " .,\n")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// installProgram compiles a program into the test world and gives it an exit,
// returning the exit's name.
func (h *harness) installProgram(t *testing.T, name, src string) string {
	t.Helper()
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location

		prog := w.Create(name+".muf", ref.TypeProgram, wiz)
		prog.Flags = prog.Flags.SetMLevel(3)
		w.SetSource(prog.Ref, src)

		e := w.Create(name, ref.TypeExit, wiz)
		e.Dest = []ref.Ref{prog.Ref}
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.out()
	return name
}

// TestMufReadTakesTheNextLine covers what the golden harness structurally
// cannot: a program that waits for input.
//
// The harness marks the end of a command's output by sending a pose and
// reading until it appears, and a program waiting on a READ consumes that
// marker as its input. There is no marker a READ would not eat, so the
// behaviour is pinned here instead.
func TestMufReadTakesTheNextLine(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "asknane", `: main
  me @ "Your name?" notify
  read
  "Hello, " swap strcat me @ swap notify
;`)

	h.send("asknane")
	if got := h.out(); !strings.Contains(got, "Your name?") {
		t.Fatalf("the program did not run:\n%s", got)
	}

	// The next line goes to the program, not the command parser.
	h.send("Igor")
	got := h.out()
	if !strings.Contains(got, "Hello, Igor") {
		t.Errorf("the read did not receive the line:\n%s", got)
	}
	if strings.Contains(got, "I don't understand") {
		t.Errorf("the line reached the command parser instead:\n%s", got)
	}
}

// TestBreakEscapesARead checks that a player can get out of a program that is
// waiting on them.
func TestBreakEscapesARead(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "waits", `: main
  me @ "waiting" notify
  read
  pop
;`)
	h.send("waits")
	h.out()

	h.send("@Q")
	if got := h.out(); !strings.Contains(got, "aborted") {
		t.Errorf("@Q should have escaped the read:\n%s", got)
	}

	// The command parser is reachable again.
	h.send("!look")
	if got := h.out(); !strings.Contains(got, "The Study") {
		t.Errorf("commands should work again after escaping:\n%s", got)
	}
}

// TestProcessListing checks that a suspended program shows up in @ps and can
// be killed.
func TestProcessListing(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A sleeping program rather than a reading one: a program waiting on a
	// READ would consume the "@ps" as its input.
	h.installProgram(t, "naps", `: main
  me @ "sleeping" notify
  30 sleep
  pop
;`)
	h.send("naps")
	h.out()

	h.send("@ps")
	got := h.out()
	if !strings.Contains(got, "sleep") {
		t.Errorf("@ps should list the sleeping program:\n%s", got)
	}
	if !strings.Contains(got, "1 process") {
		t.Errorf("@ps should report one process:\n%s", got)
	}

	// The pid is the first column of the listing's second line.
	h.send("@kill 1")
	if got := h.out(); !strings.Contains(got, "killed") {
		t.Errorf("@kill did not stop it:\n%s", got)
	}

	h.send("@ps")
	if got := h.out(); !strings.Contains(got, "0 processes") {
		t.Errorf("the process should be gone:\n%s", got)
	}
}

// TestSleepingProgramResumes checks that a tick wakes a sleeper.
func TestSleepingProgramResumes(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.installProgram(t, "naps", `: main
  me @ "before" notify
  0 sleep
  me @ "after" notify
;`)
	h.send("naps")
	if got := h.out(); !strings.Contains(got, "before") {
		t.Fatalf("the program did not start:\n%s", got)
	}

	// A tick is what resumes it.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		h.s.Tick(w)
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.out(); !strings.Contains(got, "after") {
		t.Errorf("the sleeper did not resume:\n%s", got)
	}
}
