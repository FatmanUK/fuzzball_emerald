package mpi

import (
	"strings"
	"testing"
)

// stubHost is a world just big enough for the parser's tests.
type stubHost struct {
	told  []string
	props map[string]string
}

func newStub() *stubHost { return &stubHost{props: map[string]string{}} }

func (h *stubHost) Name(obj Ref) string { return "Object" }
func (h *stubHost) GetPropStr(obj Ref, path string) string {
	return h.props[itoa(int(obj))+"/"+path]
}
func (h *stubHost) SetPropStr(obj Ref, path, val string) {
	h.props[itoa(int(obj))+"/"+path] = val
}
func (h *stubHost) DelProp(obj Ref, path string) {
	delete(h.props, itoa(int(obj))+"/"+path)
}

func (h *stubHost) PropChildren(Ref, string) []string { return nil }
func (h *stubHost) BlessProp(Ref, string, bool)       {}

func (h *stubHost) Location(Ref) Ref { return 0 }
func (h *stubHost) Parent(obj Ref) Ref {
	// A flat world: everything sits directly in #0, and #0 has no parent,
	// so an environment walk terminates after one step.
	if obj == 0 {
		return -1
	}
	return 0
}
func (h *stubHost) Owner(Ref) Ref         { return 1 }
func (h *stubHost) Contents(Ref) []Ref    { return nil }
func (h *stubHost) Valid(obj Ref) bool    { return obj >= 0 }
func (h *stubHost) IsPlayer(obj Ref) bool { return obj == 1 }
func (h *stubHost) Online(Ref) bool       { return true }
func (h *stubHost) Match(Ref, string) Ref { return 1 }
func (h *stubHost) Notify(_ Ref, msg string) {
	h.told = append(h.told, msg)
}
func (h *stubHost) Now() int64 { return 1_700_000_000 }

func newEnv(h *stubHost) *Env {
	return &Env{Who: 1, What: 2, Perms: 2, Host: h}
}

// parse evaluates and fails the test on error.
func parse(t *testing.T, in string) string {
	t.Helper()
	out, err := Parse(newEnv(newStub()), in)
	if err != nil {
		t.Fatalf("parsing %q: %v", in, err)
	}
	return out
}

func TestPlainTextPassesThrough(t *testing.T) {
	for _, s := range []string{"", "hello", "no braces here", "a,b:c"} {
		if got := parse(t, s); got != s {
			t.Errorf("parse(%q) = %q", s, got)
		}
	}
}

func TestFunctionCalls(t *testing.T) {
	cases := map[string]string{
		"{add:2,3}":       "5",
		"x{add:2,3}y":     "x5y",
		"{toupper:shout}": "SHOUT",
		"{null:anything}": "",
		"{strlen:hello}":  "5",
	}
	for in, want := range cases {
		if got := parse(t, in); got != want {
			t.Errorf("parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNesting(t *testing.T) {
	if got := parse(t, "{add:{add:1,2},{add:3,4}}"); got != "10" {
		t.Errorf("got %q, want 10", got)
	}
}

// TestBacktickTogglesLiteralness covers the one delimiter the golden cases
// cannot carry, because a backtick also ends Go's raw string literals.
func TestBacktickTogglesLiteralness(t *testing.T) {
	in := "`{add:1,2}`"
	if got := parse(t, in); got != "{add:1,2}" {
		t.Errorf("parse(%q) = %q, want the text unevaluated", in, got)
	}
	// The backticks themselves are not output.
	if strings.Contains(parse(t, "`x`"), "`") {
		t.Error("a backtick should toggle literalness, not be printed")
	}
}

func TestDoubledBraceIsLiteral(t *testing.T) {
	if got := parse(t, "{{not a call}"); got != "{not a call}" {
		t.Errorf("got %q", got)
	}
}

func TestEscapes(t *testing.T) {
	if got := parse(t, `\{add:1,2\}`); got != "{add:1,2}" {
		t.Errorf("got %q, want the braces escaped", got)
	}
	if got := parse(t, `a\rb`); got != "a\rb" {
		t.Errorf(`\r = %q, want a carriage return`, got)
	}
	if got := parse(t, `\[`); got != "\x1b" {
		t.Errorf(`\[ = %q, want an escape character`, got)
	}
}

func TestVariables(t *testing.T) {
	env := newEnv(newStub())
	if err := env.SetVar("name", "Igor"); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(env, "hello {&name}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello Igor" {
		t.Errorf("got %q", got)
	}
}

func TestUnknownVariableIsAnError(t *testing.T) {
	if _, err := Parse(newEnv(newStub()), "{&nosuch}"); err == nil {
		t.Error("an unknown variable should be an error")
	}
}

func TestWithBindsAVariable(t *testing.T) {
	if got := parse(t, "{with:n,5,{&n}{&n}}"); got != "55" {
		t.Errorf("got %q, want 55", got)
	}
	// The binding does not outlive the call.
	if _, err := Parse(newEnv(newStub()), "{with:n,5,x}{&n}"); err == nil {
		t.Error("a with-binding should not outlive its call")
	}
}

func TestShortCircuit(t *testing.T) {
	// {and} must not evaluate past a false, and {or} not past a true. A
	// call to an unknown function in the skipped part would fail if it were
	// evaluated.
	for _, in := range []string{"{and:0,{nosuchfunc:x}}", "{or:1,{nosuchfunc:x}}"} {
		if _, err := Parse(newEnv(newStub()), in); err != nil {
			t.Errorf("parse(%q) should have stopped early: %v", in, err)
		}
	}
	// And they do evaluate when they must, so the failure surfaces.
	if _, err := Parse(newEnv(newStub()), "{and:1,{nosuchfunc:x}}"); err == nil {
		t.Error("{and} should evaluate its second argument when the first is true")
	}
}

func TestIfDoesNotEvaluateTheUntakenBranch(t *testing.T) {
	if _, err := Parse(newEnv(newStub()), "{if:1,ok,{nosuchfunc:x}}"); err != nil {
		t.Errorf("the false branch should not have been evaluated: %v", err)
	}
	if _, err := Parse(newEnv(newStub()), "{if:0,{nosuchfunc:x},ok}"); err != nil {
		t.Errorf("the true branch should not have been evaluated: %v", err)
	}
}

func TestRecursionIsBounded(t *testing.T) {
	// A property that evaluates itself would otherwise loop forever.
	env := newEnv(newStub())
	deep := strings.Repeat("{concat:", 0) // no nesting needed; depth comes from Parse
	_ = deep
	for i := 0; i < recursionLimit+5; i++ {
		env.depth++
	}
	if _, err := Parse(env, "{add:1,2}"); err == nil {
		t.Error("the recursion limit should have stopped this")
	}
}

func TestInstructionLimit(t *testing.T) {
	env := newEnv(newStub())
	env.MaxInstructions = 3
	in := "{add:1,1}{add:1,1}{add:1,1}{add:1,1}{add:1,1}"
	if _, err := Parse(env, in); err == nil {
		t.Error("the instruction limit should have stopped this")
	}
}

func TestUnterminatedCallIsAnError(t *testing.T) {
	if _, err := Parse(newEnv(newStub()), "{add:1,2"); err == nil {
		t.Error("a call with no closing brace should be an error")
	}
}

func TestArityIsChecked(t *testing.T) {
	if _, err := Parse(newEnv(newStub()), "{add:1}"); err == nil {
		t.Error("too few arguments should be an error")
	}
	if _, err := Parse(newEnv(newStub()), "{abs:1,2,3}"); err == nil {
		t.Error("too many arguments should be an error")
	}
}

func TestEvalReportsRatherThanPropagates(t *testing.T) {
	// A failure in a description must not break the look that read it.
	h := newStub()
	env := newEnv(h)
	if got := Eval(env, "before {nosuchfunc:x} after"); got != "" {
		t.Errorf("Eval returned %q, want empty on failure", got)
	}
	if len(h.told) != 1 || !strings.Contains(h.told[0], "Unrecognized function") {
		t.Errorf("the failure should have been reported: %v", h.told)
	}
}

func TestFunctionTableIsPopulated(t *testing.T) {
	if Count() != 140 {
		t.Errorf("Count() = %d, want 140", Count())
	}
	t.Logf("%d of %d MPI functions implemented", Implemented(), Count())
}

// The rest of Host is stubbed out: these tests cover the parser rather than
// the world it reaches, so each answers the least interesting thing it can.
func (h *stubHost) NotifyExcept(Ref, []Ref, string) {}
func (h *stubHost) TypeName(obj Ref) string {
	if obj == 1 {
		return "Player"
	}
	return "Thing"
}
func (h *stubHost) FlagString(Ref) string    { return "" }
func (h *stubHost) HasFlag(Ref, string) bool { return false }
func (h *stubHost) Exits(Ref) []Ref          { return nil }
func (h *stubHost) Links(Ref) []Ref          { return nil }
func (h *stubHost) Value(Ref) int            { return 0 }
func (h *stubHost) Timestamps(Ref) (int64, int64, int64, int) {
	return 0, 0, 0, 0
}
func (h *stubHost) Controls(Ref, Ref) bool    { return false }
func (h *stubHost) Locked(int, Ref, Ref) bool { return false }
func (h *stubHost) TestLock(int, Ref, Ref, string) (bool, error) {
	return false, nil
}
func (h *stubHost) OnlinePlayers() []Ref              { return nil }
func (h *stubHost) Idle(Ref) int                      { return 0 }
func (h *stubHost) OnTime(Ref) int                    { return 0 }
func (h *stubHost) Width(Ref) int                     { return 0 }
func (h *stubHost) Height(Ref) int                    { return 0 }
func (h *stubHost) TuneGet(string) (string, bool)     { return "", false }
func (h *stubHost) MuckName() string                  { return "Test" }
func (h *stubHost) PronounSub(_ Ref, s string) string { return s }
func (h *stubHost) Force(int, Ref, string)            {}
func (h *stubHost) Kill(int) bool                     { return false }
func (h *stubHost) RunMUF(int, Ref, Ref, string) (string, error) {
	return "", nil
}
func (h *stubHost) Delay(int, Ref, Ref, Ref, int, string, bool) {}
