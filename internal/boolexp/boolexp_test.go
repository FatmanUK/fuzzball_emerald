package boolexp

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// fakeHost is a minimal, in-memory Host for testing the parser and
// evaluator without any of internal/world.
type fakeHost struct {
	names        map[ref.Ref]string
	owner        map[ref.Ref]ref.Ref
	location     map[ref.Ref]ref.Ref
	contents     map[ref.Ref][]ref.Ref
	types        map[ref.Ref]ref.ObjType
	flags        map[ref.Ref]ref.Flags
	propvals     map[ref.Ref]map[string]props.Value
	matches      map[string]ref.Ref
	wizards      map[ref.Ref]bool
	envcheck     bool
	parent       map[ref.Ref]ref.Ref
	runLockCalls []ref.Ref
	runLockOK    bool
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		names:    map[ref.Ref]string{},
		owner:    map[ref.Ref]ref.Ref{},
		location: map[ref.Ref]ref.Ref{},
		contents: map[ref.Ref][]ref.Ref{},
		types:    map[ref.Ref]ref.ObjType{},
		flags:    map[ref.Ref]ref.Flags{},
		propvals: map[ref.Ref]map[string]props.Value{},
		matches:  map[string]ref.Ref{},
		wizards:  map[ref.Ref]bool{},
		parent:   map[ref.Ref]ref.Ref{},
	}
}

func (h *fakeHost) Match(player ref.Ref, name string) ref.Ref {
	if r, ok := h.matches[name]; ok {
		return r
	}
	return ref.Nothing
}
func (h *fakeHost) Wizard(player ref.Ref) bool    { return h.wizards[player] }
func (h *fakeHost) Name(viewer, r ref.Ref) string { return h.names[r] }
func (h *fakeHost) Valid(r ref.Ref) bool          { _, ok := h.types[r]; return ok }
func (h *fakeHost) Type(r ref.Ref) ref.ObjType    { return h.types[r] }
func (h *fakeHost) Owner(r ref.Ref) ref.Ref       { return h.owner[r] }
func (h *fakeHost) Location(r ref.Ref) ref.Ref    { return h.location[r] }
func (h *fakeHost) Contents(r ref.Ref) []ref.Ref {
	return h.contents[r]
}
func (h *fakeHost) Flags(r ref.Ref) ref.Flags { return h.flags[r] }
func (h *fakeHost) Parent(r ref.Ref) ref.Ref {
	if p, ok := h.parent[r]; ok {
		return p
	}
	return ref.Nothing
}
func (h *fakeHost) LockEnvCheck() bool { return h.envcheck }
func (h *fakeHost) Prop(r ref.Ref, path string) (props.Value, bool) {
	m, ok := h.propvals[r]
	if !ok {
		return props.Value{}, false
	}
	v, ok := m[path]
	return v, ok
}
func (h *fakeHost) EvalLockProp(descr int, player, what ref.Ref, raw string, blessed bool) string {
	return raw // no MPI in these tests; plain string props round-trip
}
func (h *fakeHost) RunLock(descr int, player, prog, thing ref.Ref) bool {
	h.runLockCalls = append(h.runLockCalls, prog)
	return h.runLockOK
}

func (h *fakeHost) setProp(r ref.Ref, path string, v props.Value) {
	if h.propvals[r] == nil {
		h.propvals[r] = map[string]props.Value{}
	}
	h.propvals[r][path] = v
}

const (
	player1 ref.Ref = 1
	player2 ref.Ref = 2
	thing1  ref.Ref = 3
	room1   ref.Ref = 4
)

func TestParseDbloadConst(t *testing.T) {
	h := newFakeHost()
	b, err := Parse(h, 0, player1, "#123", true)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b == nil || b.Kind != Const || b.Thing != ref.Ref(123) {
		t.Fatalf("got %+v", b)
	}
}

func TestParseDbloadAndOrNot(t *testing.T) {
	h := newFakeHost()
	b, err := Parse(h, 0, player1, "#1&#2|!#3", true)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// & binds tighter than |: (#1&#2)|(!#3)
	if b.Kind != Or {
		t.Fatalf("top kind = %v, want Or", b.Kind)
	}
	if b.Sub1.Kind != And {
		t.Fatalf("left kind = %v, want And", b.Sub1.Kind)
	}
	if b.Sub2.Kind != Not {
		t.Fatalf("right kind = %v, want Not", b.Sub2.Kind)
	}
}

func TestParseParens(t *testing.T) {
	h := newFakeHost()
	b, err := Parse(h, 0, player1, "#1&(#2|#3)", true)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Kind != And || b.Sub2.Kind != Or {
		t.Fatalf("got %+v", b)
	}
}

func TestParseProp(t *testing.T) {
	h := newFakeHost()
	h.wizards[player1] = false
	b, err := Parse(h, 0, player1, "foo:bar", false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Kind != Prop || b.PropName != "foo" || b.PropValue != "bar" {
		t.Fatalf("got %+v", b)
	}
}

func TestParseHiddenPropDenied(t *testing.T) {
	h := newFakeHost()
	h.wizards[player1] = false
	_, err := Parse(h, 0, player1, "@foo:bar", false)
	if err == nil {
		t.Fatalf("expected permission error")
	}
}

func TestParseHiddenPropAllowedForWizard(t *testing.T) {
	h := newFakeHost()
	h.wizards[player1] = true
	b, err := Parse(h, 0, player1, "@foo:bar", false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Kind != Prop {
		t.Fatalf("got %+v", b)
	}
}

func TestParseMatchNotFound(t *testing.T) {
	h := newFakeHost()
	_, err := Parse(h, 0, player1, "nosuchthing", false)
	if err == nil {
		t.Fatalf("expected a match error")
	}
}

func TestParseMatchAmbiguous(t *testing.T) {
	h := newFakeHost()
	h.matches["Rex"] = ref.Ambiguous
	_, err := Parse(h, 0, player1, "Rex", false)
	if err == nil {
		t.Fatalf("expected an ambiguous-match error")
	}
}

// TestParseErrorNotifyFlag checks which parse failures upstream's own
// parser would have shown to the player (Notify true — a match failure or
// the hidden-property permission check) against which it fails on silently
// (Notify false — a bare syntax error). Confirmed against the real server:
// an unparseable "" PARSELOCK argument produces no message at all, which is
// only correct if syntax errors stay unnotified.
func TestParseErrorNotifyFlag(t *testing.T) {
	h := newFakeHost()
	h.matches["ok"] = thing1

	cases := []struct {
		name   string
		input  string
		notify bool
	}{
		{"match not found", "nosuchthing", true},
		{"hidden prop denied", "@foo:bar", true},
		{"unbalanced parens", "(ok", false},
		{"empty prop name", ":bar", false},
		{"empty prop value", "foo:", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(h, 0, player1, c.input, false)
			if err == nil {
				t.Fatalf("Parse(%q): expected an error", c.input)
			}
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("Parse(%q) error = %T, want *ParseError", c.input, err)
			}
			if pe.Notify != c.notify {
				t.Errorf("Parse(%q).(*ParseError).Notify = %v, want %v", c.input, pe.Notify, c.notify)
			}
		})
	}
}

func TestEvalNilAlwaysPasses(t *testing.T) {
	h := newFakeHost()
	if !Eval(h, 0, player1, nil, thing1) {
		t.Fatalf("nil boolexp should pass")
	}
}

func TestEvalConstIsPlayer(t *testing.T) {
	h := newFakeHost()
	h.types[player1] = ref.TypePlayer
	b := &Expr{Kind: Const, Thing: player1}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("player should match itself")
	}
}

func TestEvalConstIsOwner(t *testing.T) {
	h := newFakeHost()
	h.types[player2] = ref.TypePlayer
	h.owner[player1] = player2
	b := &Expr{Kind: Const, Thing: player2}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("player's owner should match")
	}
}

func TestEvalConstIsCarried(t *testing.T) {
	h := newFakeHost()
	h.types[thing1] = ref.TypeThing
	h.contents[player1] = []ref.Ref{thing1}
	b := &Expr{Kind: Const, Thing: thing1}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("carried object should match")
	}
}

func TestEvalConstIsLocation(t *testing.T) {
	h := newFakeHost()
	h.types[room1] = ref.TypeRoom
	h.location[player1] = room1
	b := &Expr{Kind: Const, Thing: room1}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("location should match")
	}
}

func TestEvalConstUnrelatedFails(t *testing.T) {
	h := newFakeHost()
	h.types[player2] = ref.TypePlayer
	b := &Expr{Kind: Const, Thing: player2}
	if Eval(h, 0, player1, b, thing1) {
		t.Fatalf("unrelated dbref should not match")
	}
}

func TestEvalConstNothingFails(t *testing.T) {
	h := newFakeHost()
	b := &Expr{Kind: Const, Thing: ref.Nothing}
	if Eval(h, 0, player1, b, thing1) {
		t.Fatalf("NOTHING should never match")
	}
}

func TestEvalConstProgramRuns(t *testing.T) {
	h := newFakeHost()
	prog := ref.Ref(50)
	h.types[prog] = ref.TypeProgram
	h.runLockOK = true
	b := &Expr{Kind: Const, Thing: prog}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("program returning success should pass")
	}
	if len(h.runLockCalls) != 1 || h.runLockCalls[0] != prog {
		t.Fatalf("RunLock not invoked correctly: %v", h.runLockCalls)
	}

	h.runLockOK = false
	if Eval(h, 0, player1, b, thing1) {
		t.Fatalf("program aborting should fail")
	}
}

func TestEvalAndOrNot(t *testing.T) {
	h := newFakeHost()
	tru := &Expr{Kind: Const, Thing: ref.Nothing} // always false (NOTHING)
	// Build True via Not(False)
	falseExpr := tru
	trueExpr := &Expr{Kind: Not, Sub1: falseExpr}

	and := &Expr{Kind: And, Sub1: trueExpr, Sub2: falseExpr}
	if Eval(h, 0, player1, and, thing1) {
		t.Fatalf("true & false should fail")
	}

	or := &Expr{Kind: Or, Sub1: trueExpr, Sub2: falseExpr}
	if !Eval(h, 0, player1, or, thing1) {
		t.Fatalf("true | false should pass")
	}
}

func TestEvalPropOnThing(t *testing.T) {
	h := newFakeHost()
	h.types[thing1] = ref.TypeThing
	h.setProp(thing1, "pass", props.Value{Type: props.String, Str: "letmein"})
	b := &Expr{Kind: Prop, PropName: "pass", PropValue: "letmein"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("matching string prop on thing should pass")
	}
}

func TestEvalPropOnPlayer(t *testing.T) {
	h := newFakeHost()
	h.setProp(player1, "pass", props.Value{Type: props.String, Str: "letmein"})
	b := &Expr{Kind: Prop, PropName: "pass", PropValue: "letmein"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("matching string prop on the evaluating player should pass")
	}
}

func TestEvalPropWildcard(t *testing.T) {
	h := newFakeHost()
	h.setProp(player1, "pass", props.Value{Type: props.String, Str: "letmein"})
	b := &Expr{Kind: Prop, PropName: "pass", PropValue: "let*"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("wildcard value should match")
	}
}

func TestEvalPropIntZeroMatches(t *testing.T) {
	// Reproduces the upstream quirk: an int prop of 0 always "matches" a
	// lock's string-value check, since the value comparison upstream passes
	// is hard-coded to 0.
	h := newFakeHost()
	h.setProp(player1, "counter", props.Value{Type: props.Int, Num: 0})
	b := &Expr{Kind: Prop, PropName: "counter", PropValue: "anything"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("int prop == 0 should match regardless of PropValue")
	}
}

func TestEvalPropIntNonzeroFails(t *testing.T) {
	h := newFakeHost()
	h.setProp(player1, "counter", props.Value{Type: props.Int, Num: 5})
	b := &Expr{Kind: Prop, PropName: "counter", PropValue: "anything"}
	if Eval(h, 0, player1, b, thing1) {
		t.Fatalf("nonzero int prop should not match")
	}
}

func TestEvalPropRecursesIntoContents(t *testing.T) {
	h := newFakeHost()
	h.contents[player1] = []ref.Ref{thing1}
	h.setProp(thing1, "pass", props.Value{Type: props.String, Str: "letmein"})
	b := &Expr{Kind: Prop, PropName: "pass", PropValue: "letmein"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("prop on a carried object should be found")
	}
}

func TestEvalPropEnvCheck(t *testing.T) {
	h := newFakeHost()
	h.envcheck = true
	h.parent[player1] = room1
	h.setProp(room1, "pass", props.Value{Type: props.String, Str: "letmein"})
	b := &Expr{Kind: Prop, PropName: "pass", PropValue: "letmein"}
	if !Eval(h, 0, player1, b, thing1) {
		t.Fatalf("prop found via lock_envcheck should pass")
	}

	h.envcheck = false
	if Eval(h, 0, player1, b, thing1) {
		t.Fatalf("without lock_envcheck the environment should not be searched")
	}
}

func TestUnparseRoundTrip(t *testing.T) {
	h := newFakeHost()
	cases := []string{
		"#1",
		"#1&#2",
		"#1|#2",
		"!#1",
		"#1&#2|#3",
		"#1&(#2|#3)",
		"(#1|#2)&#3",
		"foo:bar",
	}
	for _, in := range cases {
		b, err := Parse(h, 0, player1, in, true)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		out := Unparse(h, player1, b, false)
		b2, err := Parse(h, 0, player1, out, true)
		if err != nil {
			t.Fatalf("re-Parse(%q): %v", out, err)
		}
		out2 := Unparse(h, player1, b2, false)
		if out != out2 {
			t.Fatalf("Unparse not stable for %q: %q vs %q", in, out, out2)
		}
	}
}

func TestUnparseUnlocked(t *testing.T) {
	h := newFakeHost()
	if got := Unparse(h, player1, nil, false); got != Unlocked {
		t.Fatalf("got %q, want %q", got, Unlocked)
	}
}

func TestUnparseFullname(t *testing.T) {
	h := newFakeHost()
	h.names[thing1] = "Rex"
	b := &Expr{Kind: Const, Thing: thing1}
	if got := Unparse(h, player1, b, true); got != "Rex" {
		t.Fatalf("got %q, want Rex", got)
	}
}

func TestUnparseParenthesizesOrUnderAnd(t *testing.T) {
	h := newFakeHost()
	b := &Expr{Kind: And, Sub1: &Expr{Kind: Const, Thing: 1}, Sub2: &Expr{Kind: Or,
		Sub1: &Expr{Kind: Const, Thing: 2}, Sub2: &Expr{Kind: Const, Thing: 3}}}
	got := Unparse(h, player1, b, false)
	want := "#1&(#2|#3)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
