package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// checkargsHost is the little world the dbref characters need: a
// db_top and a type per ref, and nothing else.
//
// It is its own host rather than lockTestHost extended, because that
// one embeds a nil Host and so panics on any method it has not been
// given — including Top, which every dbref character here calls.
type checkargsHost struct {
	Host

	top   ref.Ref
	types map[ref.Ref]ref.ObjType
}

func (h *checkargsHost) Top() ref.Ref { return h.top }

func (h *checkargsHost) ObjType(r ref.Ref) ref.ObjType {
	if t, ok := h.types[r]; ok {
		return t
	}
	return ref.NoType
}

// The fixture the golden case uses, so the two read alike: #0 a room,
// #1 a player, #2 a program, #3 an exit, #4 a thing and #5 garbage,
// with db_top at 6.
func newCheckargsHost() *checkargsHost {
	return &checkargsHost{
		top: 6,
		types: map[ref.Ref]ref.ObjType{
			0: ref.TypeRoom,
			1: ref.TypePlayer,
			2: ref.TypeProgram,
			3: ref.TypeExit,
			4: ref.TypeThing,
			5: ref.TypeGarbage,
		},
	}
}

// runCheckargs puts stack under the signature and returns what
// CHECKARGS said, or "" for a pass.
func runCheckargs(t *testing.T, sig string, stack ...Value) string {
	t.Helper()
	f := &Frame{
		Level: 1,
		host:  newCheckargsHost(),
		Prog:  &Program{Ref: 2, MLevel: 3},
	}
	f.Stack = append(f.Stack, stack...)
	f.Stack = append(f.Stack, Str(sig))
	if _, err := primCheckargs(f); err != nil {
		return err.Error()
	}
	return ""
}

// TestCheckargsSignatureErrors covers the refusals about the
// signature rather than about an argument. None of them carries a
// position suffix, and the unknown-character one does not test for
// stack underflow first — so a stray letter is reported even with
// nothing on the stack to have checked.
func TestCheckargsSignatureErrors(t *testing.T) {
	const tooBig = "Multiplier too large in argument expression."
	cases := []struct {
		sig, want string
	}{
		// The empty string is a NULL shared_string upstream,
		// which it reads as "no arguments expected" and
		// returns on before the loop runs at all.
		{"", ""},
		{"q", "Unknown argument type in expression."},
		{"I", "Unknown argument type in expression."},
		{"*", "Unknown argument type in expression."},
		{"0", "Bad multiplier '0' in argument expression."},
		{"00", "Bad multiplier '0' in argument expression."},
		{"1024", tooBig},
		{"99999", tooBig},
		// An absurd run of digits overflows upstream's int,
		// which is undefined behaviour; growth is clamped
		// here so it lands where a non-overflowing C would.
		{"99999999999999999999", tooBig},
		// A multiplier with nothing to its left never finds
		// what it was meant to repeat.
		{"1023", "Badly formed argument expression."},
		{"1", "Badly formed argument expression."},
		// Upstream's wording, with no full stop. Every other
		// message here has one.
		{"{", "Mismatched { in argument expression"},
		{"{{", "Mismatched { in argument expression"},
		// A digit-pushed repeat sits on the range stack and
		// leaves currpos on the character to its left, so a
		// brace there finds the wrong kind of entry.
		{"{3", "Misformed argument expression."},
		// A space describes no argument, so it needs no stack
		// item and an all-space signature passes on an empty
		// stack.
		{" ", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := runCheckargs(t, c.sig); got != c.want {
			t.Errorf("%q: got %q, want %q",
				c.sig, got, c.want)
		}
	}
}

// TestCheckargsPositionSuffix pins the ABORT_CHECKARGS macro
// (p_stack.c:1124). Every per-argument refusal names which stack
// position failed, counted down from the top, and the offset is
// relative — so it does not depend on how deep the stack is below
// what the signature describes.
func TestCheckargsPositionSuffix(t *testing.T) {
	cases := []struct {
		sig   string
		stack []Value
		want  string
	}{
		{"i", nil, "Stack underflow. (top)"},
		{"ii", nil, "Stack underflow. (top)"},
		{"is", []Value{Int(5), Int(6)},
			"Expected a string. (top)"},
		{"si", []Value{Int(5), Int(6)},
			"Expected a string. (top-1)"},
		{"ssi", []Value{Int(5), Int(6)},
			"Expected a string. (top-1)"},
		{"i3", []Value{Int(5), Int(6)},
			"Stack underflow. (top-2)"},
		// Relative, not absolute: the same signature over the
		// same two items reports the same offset however much
		// junk is underneath.
		{"si", []Value{Str("a"), Str("b"), Int(5), Int(6)},
			"Expected a string. (top-1)"},
	}
	for _, c := range cases {
		got := runCheckargs(t, c.sig, c.stack...)
		if got != c.want {
			t.Errorf("%q: got %q, want %q",
				c.sig, got, c.want)
		}
	}
}

// TestCheckargsTypes covers one character against one value for every
// type the language names.
func TestCheckargsTypes(t *testing.T) {
	const wantDict = "Expected a dictionary array. (top)"
	list := NewList([]Value{Int(1)})
	dict := NewDict()
	dict.Set(Str("k"), Int(1))

	cases := []struct {
		sig  string
		v    Value
		want string
	}{
		{"i", Int(5), ""},
		{"i", Float(1.5), "Expected an integer. (top)"},
		{"n", Float(1.5), ""},
		{"n", Int(5), "Expected a float. (top)"},
		{"s", Str("abc"), ""},
		{"S", Str("abc"), ""},
		// Upstream's empty string is a NULL shared_string,
		// which is what the uppercase form refuses.
		{"s", Str(""), ""},
		{"S", Str(""), "Expected a non-null string. (top)"},
		{"s", Int(5), "Expected a string. (top)"},
		{"?", Int(5), ""},
		{"?", Str(""), ""},
		{"l", Value{Type: TypeLock}, ""},
		{"l", Int(5),
			"Expected a lock boolean expression. (top)"},
		{"v", Value{Type: TypeVar}, ""},
		{"v", Value{Type: TypeLVar}, ""},
		{"v", Value{Type: TypeSVar}, ""},
		{"v", Int(5), "Expected a variable. (top)"},
		{"a", Value{Type: TypeAddress}, ""},
		{"a", Int(5), "Expected a function address. (top)"},
		{"y", Arr(list), ""},
		{"Y", Arr(list), ""},
		{"x", Arr(list), wantDict},
		{"y", Arr(dict), ""},
		{"x", Arr(dict), ""},
		{"Y", Arr(dict),
			"Expected a non-dictionary array. (top)"},
		{"y", Int(5), "Expected an array. (top)"},
		// A nil array passes 'Y': upstream guards the
		// dictionary test on the pointer being non-NULL.
		{"Y", Value{Type: TypeArray}, ""},
		{"x", Value{Type: TypeArray}, wantDict},
	}
	for _, c := range cases {
		got := runCheckargs(t, c.sig, c.v)
		if got != c.want {
			t.Errorf("%q over %v: got %q, want %q",
				c.sig, c.v.Type, got, c.want)
		}
	}
}

// TestCheckargsDbrefs is the twelve dbref characters against every
// ref worth trying, and is the part of the primitive most likely to
// surprise.
//
// What the lowercase forms accept is wider than it looks. The outer
// test is `ref >= db_top || ref < HOME`, so NOTHING (#-1) and
// AMBIGUOUS (#-2) survive it, and each lowercase case then asks its
// type question only of a *non-negative* ref — so `p` takes #-1 and
// #-2 as readily as a real player. The room case is wider still:
// Typeof(HOME) is TYPE_ROOM (db.h:423) and the explicit HOME refusal
// the other five carry is simply missing from it, so `r` and `R` both
// accept #-3.
func TestCheckargsDbrefs(t *testing.T) {
	const (
		okay    = ""
		badRef  = "Invalid dbref. (top)"
		notPlay = "Expected player dbref. (top)"
		notRoom = "Expected room dbref. (top)"
		notThng = "Expected thing dbref. (top)"
		notExit = "Expected exit dbref. (top)"
		notProg = "Expected program dbref. (top)"
	)
	// The columns are d D p P r R t T e E f F.
	chars := "dDpPrRtTeEfF"
	table := []struct {
		r    ref.Ref
		want [12]string
	}{
		{0, [12]string{okay, okay, notPlay, notPlay, okay,
			okay, notThng, notThng, notExit, notExit,
			notProg, notProg}},
		{1, [12]string{okay, okay, okay, okay, notRoom,
			notRoom, notThng, notThng, notExit, notExit,
			notProg, notProg}},
		{2, [12]string{okay, okay, notPlay, notPlay, notRoom,
			notRoom, notThng, notThng, notExit, notExit,
			okay, okay}},
		{3, [12]string{okay, okay, notPlay, notPlay, notRoom,
			notRoom, notThng, notThng, okay, okay,
			notProg, notProg}},
		{4, [12]string{okay, okay, notPlay, notPlay, notRoom,
			notRoom, okay, okay, notExit, notExit,
			notProg, notProg}},
		// Garbage: the only thing 'D' refuses that 'd'
		// accepts, which is its whole reason for existing.
		{5, [12]string{okay, badRef, notPlay, notPlay,
			notRoom, notRoom, notThng, notThng, notExit,
			notExit, notProg, notProg}},
		// Past db_top, where the outer test fires and every
		// character reports the same thing.
		{999, [12]string{badRef, badRef, badRef, badRef,
			badRef, badRef, badRef, badRef, badRef,
			badRef, badRef, badRef}},
		// NOTHING and AMBIGUOUS pass every lowercase form.
		{ref.Nothing, [12]string{okay, badRef, okay, notPlay,
			okay, notRoom, okay, notThng, okay, notExit,
			okay, notProg}},
		{ref.Ambiguous, [12]string{okay, badRef, okay,
			notPlay, okay, notRoom, okay, notThng, okay,
			notExit, okay, notProg}},
		// HOME is a room to both forms, and refused by the
		// other five.
		{ref.Home, [12]string{okay, okay, notPlay, notPlay,
			okay, okay, notThng, notThng, notExit,
			notExit, notProg, notProg}},
	}
	for _, row := range table {
		for i := 0; i < len(chars); i++ {
			sig := string(chars[i])
			got := runCheckargs(t, sig, Obj(row.r))
			if got != row.want[i] {
				t.Errorf("%q over %v: %q, want %q",
					sig, row.r, got, row.want[i])
			}
		}
	}
	// And a non-dbref value, which every one of the twelve
	// reports the same way.
	for i := 0; i < len(chars); i++ {
		sig := string(chars[i])
		got := runCheckargs(t, sig, Int(5))
		if got != "Expected a dbref. (top)" {
			t.Errorf("%q over an int: got %q", sig, got)
		}
	}
}

// TestCheckargsGroupsAndMultipliers covers the repetition machinery:
// a `{ }` group whose count comes off the stack, a numeric multiplier
// that repeats whatever is to its left, and the three corners of the
// zero-count skip scan.
func TestCheckargsGroupsAndMultipliers(t *testing.T) {
	cases := []struct {
		sig   string
		stack []Value
		want  string
	}{
		// A multiplier repeats the single character to its
		// left, because the scan runs right to left and so
		// reads the number before what it applies to.
		{"i1", []Value{Int(5)}, ""},
		{"i2", []Value{Int(5), Int(6)}, ""},
		{"i2", []Value{Int(5)}, "Stack underflow. (top-1)"},
		{"i3", []Value{Int(5), Int(6), Int(7)}, ""},

		// A group's count is a runtime value, so closing one
		// consumes a stack item.
		{"{i}", []Value{Int(5), Int(6), Int(2)}, ""},
		{"{i}", []Value{Int(5), Int(6), Int(7), Int(3)}, ""},
		{"{i}", []Value{Int(5), Int(1)}, ""},
		{"{i}", []Value{Int(5), Int(3)},
			"Stack underflow. (top-2)"},
		{"{i}", []Value{Int(5), Int(-1)}, "Range counter " +
			"should be non-negative. (top)"},
		{"{i}", []Value{Int(5), Str("x")},
			"Expected an integer range counter. (top)"},

		// A count of zero skips the body without checking any
		// of it.
		{"{i}", []Value{Int(5), Int(0)}, ""},
		{"{i}", []Value{Str("x"), Int(0)}, ""},

		// An empty group would repeat nothing for however
		// many times the stack asked, which upstream's own
		// comment says could hang the server; it forces the
		// count to 1 instead. Only the timing differs, so
		// this pins the result rather than the guard.
		{"{}", []Value{Int(500)}, ""},
		{"{ }", []Value{Int(500)}, ""},
		{"{   }", []Value{Int(500)}, ""},

		// The skip scan does not understand nesting: it stops
		// at the first `{` to its left, which for a nested
		// group is the inner one, leaving the outer `{`
		// unbalanced.
		{"{{i}}", []Value{Int(0)},
			"Mismatched { in argument expression"},

		// And its bound is `currpos > 0`, so index 0 is never
		// examined. With no brace at all the scan moves
		// nothing and the character at 0 is type-checked
		// after all.
		{"i}", []Value{Str("x"), Int(0)},
			"Expected an integer. (top-1)"},
		// The range the brace opened is never closed, because
		// there is no brace to close it — so passing the
		// type check only gets as far as the end of the scan.
		{"i}", []Value{Int(5), Int(0)},
			"Badly formed argument expression."},

		// A space describes no argument, so it neither
		// consumes a stack item nor shifts the offsets.
		{"i i", []Value{Int(5), Int(6)}, ""},
		{"i  i", []Value{Int(5), Int(6)}, ""},
		{" i ", []Value{Int(5)}, ""},
	}
	for _, c := range cases {
		got := runCheckargs(t, c.sig, c.stack...)
		if got != c.want {
			t.Errorf("%q: got %q, want %q",
				c.sig, got, c.want)
		}
	}
}

// TestCheckargsComplexity pins MAX_COMPLEXITY at 18, and the order of
// the two tests in the brace branch. With exactly eighteen counters
// for nineteen groups the nineteenth brace is both too deep and short
// of an argument; upstream tests the depth first, so that is what it
// reports.
func TestCheckargsComplexity(t *testing.T) {
	const complex = "Argument expression ridiculously complex."

	deep := func(n int) string {
		s := ""
		for i := 0; i < n; i++ {
			s += "{"
		}
		s += "i"
		for i := 0; i < n; i++ {
			s += "}"
		}
		return s
	}
	ones := func(n int) []Value {
		out := make([]Value, n)
		for i := range out {
			out[i] = Int(1)
		}
		return out
	}

	// Eighteen nested groups fit; the counters are all consumed
	// by the braces, so the innermost 'i' finds nothing left.
	if got := runCheckargs(t, deep(18), ones(18)...); got !=
		"Stack underflow. (top-18)" {
		t.Errorf("eighteen deep: got %q", got)
	}
	// Nineteen do not, with counters to spare.
	if got := runCheckargs(t, deep(19), ones(19)...); got !=
		complex {
		t.Errorf("nineteen deep: got %q", got)
	}
	// Nor with one short, which is where the order shows.
	if got := runCheckargs(t, deep(19), ones(18)...); got !=
		complex {
		t.Errorf("nineteen deep, eighteen counters: got %q",
			got)
	}
}

// TestCheckargsNonString is the one refusal that is not about the
// signature's contents, and its wording is upstream's: "Non string",
// not "Non-string".
func TestCheckargsNonString(t *testing.T) {
	f := &Frame{
		Level: 1,
		host:  newCheckargsHost(),
		Prog:  &Program{Ref: 2, MLevel: 3},
	}
	f.Stack = append(f.Stack, Int(5))
	_, err := primCheckargs(f)
	if err == nil || err.Error() != "Non string argument." {
		t.Errorf("got %v, want Non string argument.", err)
	}
}

// ProgMLevel declines to answer, so Frame.MLevel falls back to the
// level the test set on the Program itself.
//
// Every host here embeds a nil Host, which is deliberate: anything a
// test does not answer is a programming error rather than a silent
// zero. Frame.MLevel reads this for *every* mucker gate, though, so
// without it a primitive with a floor segfaults rather than saying
// which method it wanted.
func (h *checkargsHost) ProgMLevel(ref.Ref) (int, bool) {
	return 0, false
}

// UnableToSetFlag declines to refuse, so a frame that reaches SET in
// this test is not stopped by a rule the host cannot answer. These
// hosts embed a nil Host, so a missing method is a segfault at the
// point of use rather than a compile error.
func (h *checkargsHost) UnableToSetFlag(ref.Ref, int, ref.Ref,
	ref.Flags, bool) (string, bool) {
	return "", false
}
