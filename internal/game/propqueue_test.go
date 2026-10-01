package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestPropqueueRunsAProgram covers the half of the mechanism the
// golden harness cannot: a propqueue whose value names a **program**
// rather than MPI. Installing one needs the editor, which holds the
// input line, so the oracle cannot be driven through it — and the
// two forms share every line of resolution, so the MPI half pins the
// mechanism and this pins the launch.
func TestPropqueueRunsAProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	// A program that reports its own COMMAND and stack argument,
	// which is what tells the two apart: upstream hands a
	// propqueue "Queued event." as COMMAND and the queue's own
	// name as the pushed value.
	prog := h.installPropqProgram(t, "hook", `: main
  "arg=" swap strcat me @ swap notify
  command @ "cmd=" swap strcat me @ swap notify
;`)

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		here := w.Get(h.wizRef()).Location
		w.SetProp(here, propLookQueue, props.Value{
			Type: props.String, Str: prog.String(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look")
	got := h.out()
	// The argument is the dbref of what was looked at, written
	// with a '#'.
	if !strings.Contains(got, "arg=#") {
		t.Errorf("the program got no dbref argument:\n%s", got)
	}
	if !strings.Contains(got, "cmd=Queued event.") {
		t.Errorf("COMMAND was not \"Queued event.\":\n%s", got)
	}
}

// TestPropqueueRefusesTheWrongProgram pins the three tests upstream
// makes before running a program a property named, each of which is a
// way of stopping a world from making somebody else's hook run code
// they do not control.
func TestPropqueueRefusesTheWrongProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	prog := h.installPropqProgram(t, "hook",
		`: main pop "ran" me @ swap notify ;`)

	ctx := context.Background()
	var other ref.Ref
	err := h.engine.Do(ctx, func(w *world.World) {
		p := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		p.Owner = p.Ref
		// Mucker bits, because the floor is tested against
		// the owner's level as well as the program's: a
		// level-0 owner would make the LINK_OK half
		// untestable.
		p.Flags = p.Flags.SetMLevel(3)
		other = p.Ref
		here := w.Get(h.wizRef()).Location
		w.SetProp(here, propLookQueue, props.Value{
			Type: props.String, Str: prog.String(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	// The baseline: the test player owns the program, so it runs.
	h.send("look")
	if got := h.out(); !strings.Contains(got, "ran") {
		t.Fatalf("the hook did not run at all:\n%s", got)
	}

	// Somebody else's program, not LINK_OK: refused silently,
	// which is the whole difficulty of debugging a propqueue.
	err = h.engine.Do(ctx, func(w *world.World) {
		w.Get(prog).Owner = other
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	h.send("look")
	if got := h.out(); strings.Contains(got, "ran") {
		t.Errorf("somebody else's program ran:\n%s", got)
	}

	// LINK_OK makes it public again.
	err = h.engine.Do(ctx, func(w *world.World) {
		w.Get(prog).Flags |= ref.LinkOK
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	h.send("look")
	if got := h.out(); !strings.Contains(got, "ran") {
		t.Errorf("a LINK_OK program was still refused:\n%s", got)
	}

	// And the mucker floor, which is tested against the program
	// *and* its owner.
	err = h.engine.Do(ctx, func(w *world.World) {
		w.Get(prog).Flags = w.Get(prog).Flags.SetMLevel(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	h.send("look")
	if got := h.out(); strings.Contains(got, "ran") {
		t.Errorf("a level-0 program ran at mlev 1:\n%s", got)
	}
}

// TestPropqueueDepthIsBounded covers the shared recursion counter.
// Reaching it from MPI needs {force}, whose own refusals Emerald has
// only half of, so it is driven from a program instead: one that
// looks again, which re-enters the queue that started it.
func TestPropqueueDepthIsBounded(t *testing.T) {
	h := newHarness(t)
	h.login()

	prog := h.installPropqProgram(t, "loop", `: main
  pop me @ "look" force
;`)

	ctx := context.Background()
	err := h.engine.Do(ctx, func(w *world.World) {
		// FORCE is a wizbit primitive and a program runs at
		// find_mlev — the lower of its own level and its
		// owner's — so mucker bits alone cap it at 3. The
		// WIZARD flag on the program is what lifts it to 4.
		o := w.Get(prog)
		o.Flags = o.Flags.SetMLevel(3) | ref.Wizard
		here := w.Get(h.wizRef()).Location
		w.SetProp(here, propLookQueue, props.Value{
			Type: props.String, Str: prog.String(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()

	h.send("look")
	got := h.out()
	if !strings.Contains(got,
		"Propqueue stopped to prevent infinite loop.") {
		t.Errorf("the depth bound did not report:\n%s",
			lastLines(got, 6))
	}
	// And the counter must come back down, or every later look
	// would be refused.
	if h.s.propqLevel != 0 {
		t.Errorf("propqLevel left at %d", h.s.propqLevel)
	}
}

// TestListenValueMayBeConditional pins the one thing only a listen
// propqueue does: "Message=action" runs the right-hand side only when
// the left matches what was said.
func TestListenValueMayBeConditional(t *testing.T) {
	for _, tc := range []struct {
		value, before, after string
		found                bool
	}{
		{"&Hello.", "&Hello.", "", false},
		{"hello=&Hi there.", "hello", "&Hi there.", true},
		{"hel*=&Hi.", "hel*", "&Hi.", true},
		// An escaped '=' is part of the pattern, not the
		// separator.
		{`a\=b=&Yes.`, `a\=b`, "&Yes.", true},
		{"=&Always.", "", "&Always.", true},
	} {
		before, after, found := cutUnescaped(tc.value, '=')
		if before != tc.before || after != tc.after ||
			found != tc.found {
			t.Errorf("cutUnescaped(%q) = %q, %q, %v; "+
				"want %q, %q, %v", tc.value, before,
				after, found, tc.before, tc.after,
				tc.found)
		}
	}
}

// TestPropqTargetReadsAllFourForms pins how a property names a
// program, which is four spellings and one of them is not a program.
func TestPropqTargetReadsAllFourForms(t *testing.T) {
	w := world.New()
	room := w.Create("Room", ref.TypeRoom, ref.God)
	prog := w.Create("prog", ref.TypeProgram, ref.God)
	w.SetProp(room.Ref, "_reg/hook", props.Value{
		Type: props.Ref, Ref: prog.Ref,
	})

	for _, tc := range []struct {
		name string
		v    props.Value
		text string
		prog ref.Ref
		mpi  bool
	}{
		{"mpi", props.Value{Type: props.String, Str: "&Hi."},
			"Hi.", ref.Nothing, true},
		{"hash", props.Value{Type: props.String,
			Str: prog.Ref.String()}, "", prog.Ref, false},
		{"bare number", props.Value{
			Type: props.String, Str: "2"},
			"", ref.Ref(2), false},
		{"registered", props.Value{Type: props.String,
			Str: "$hook"}, "", prog.Ref, false},
		{"a ref property", props.Value{Type: props.Ref,
			Ref: prog.Ref}, "", prog.Ref, false},
		{"nonsense", props.Value{Type: props.String,
			Str: "do the thing"}, "", ref.Nothing, false},
		{"#nonsense", props.Value{Type: props.String,
			Str: "#two"}, "", ref.Nothing, false},
		{"empty", props.Value{Type: props.String, Str: ""},
			"", ref.Nothing, false},
		{"ambiguous", props.Value{Type: props.Ref,
			Ref: ref.Ambiguous}, "", ref.Nothing, false},
	} {
		text, got, isMPI := propqTarget(w, room.Ref, tc.v)
		if text != tc.text || got != tc.prog ||
			isMPI != tc.mpi {
			t.Errorf("%s: = %q, %v, %v; want %q, %v, %v",
				tc.name, text, got, isMPI,
				tc.text, tc.prog, tc.mpi)
		}
	}
}

// installPropqProgram compiles a program into the world and returns
// its ref, which is what a propqueue property holds.
func (h *harness) installPropqProgram(t *testing.T, name,
	src string) ref.Ref {

	t.Helper()
	var r ref.Ref
	if err := h.engine.Do(context.Background(),
		func(w *world.World) {
			wiz := h.wizRef()
			prog := w.Create(name+".muf",
				ref.TypeProgram, wiz)
			prog.Flags = prog.Flags.SetMLevel(3)
			w.SetSource(prog.Ref, src)
			r = prog.Ref
		}); err != nil {
		t.Fatal(err)
	}
	h.out()
	return r
}

// lastLines trims a transcript to its tail, for a failure message.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
