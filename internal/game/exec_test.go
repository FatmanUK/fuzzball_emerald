package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// describeWith sets a thing's description and returns the thing, so
// each case below reads as "a description of X shows Y".
func (h *harness) describeWith(t *testing.T, name, desc string) ref.Ref {
	t.Helper()
	var thing ref.Ref
	err := h.engine.Do(context.Background(), func(w *world.World) {
		wiz := h.wizRef()
		here := w.Get(wiz).Location
		o := w.Create(name, ref.TypeThing, wiz)
		o.Home = here
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		w.SetProp(o.Ref, propDesc,
			props.Value{Type: props.String, Str: desc})
		thing = o.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	return thing
}

// makeProgram compiles a program into the world without giving it an
// exit, since a message property reaches one by dbref or by
// registration rather than by being run as a command.
func (h *harness) makeProgram(t *testing.T, name, src string) ref.Ref {
	t.Helper()
	var prog ref.Ref
	err := h.engine.Do(context.Background(), func(w *world.World) {
		p := w.Create(name, ref.TypeProgram, h.wizRef())
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, src)
		prog = p.Ref
	})
	if err != nil {
		t.Fatal(err)
	}
	h.out()
	return prog
}

// TestDescriptionRunsAProgram is the bug exec_or_notify fixes. A
// description whose value begins with '@' names a MUF program, and
// Emerald printed it as literal text — so a world using the idiom
// showed "@123" to whoever looked.
func TestDescriptionRunsAProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	prog := h.makeProgram(t, "desc.muf",
		`: main me @ "The mirror shows you nothing." notify ;`)
	h.describeWith(t, "mirror", "@"+strings.TrimPrefix(prog.String(), "#"))

	h.send("look mirror")
	got := h.out()
	if !strings.Contains(got, "The mirror shows you nothing.") {
		t.Errorf("the program did not run:\n%s", got)
	}
	if strings.Contains(got, "@"+strings.TrimPrefix(prog.String(), "#")) {
		t.Errorf("the description printed literally:\n%s", got)
	}
}

// TestDescriptionRunsARegisteredProgram covers the other spelling,
// "@$name", which find_registered_obj resolves through _reg/ on the
// object carrying the description and then outwards.
func TestDescriptionRunsARegisteredProgram(t *testing.T) {
	h := newHarness(t)
	h.login()

	prog := h.makeProgram(t, "desc.muf",
		`: main me @ "Registered and running." notify ;`)
	err := h.engine.Do(context.Background(), func(w *world.World) {
		w.SetProp(ref.GlobalEnvironment, "_reg/lib-desc",
			props.Value{Type: props.Ref, Ref: prog})
	})
	if err != nil {
		t.Fatal(err)
	}
	h.describeWith(t, "portrait", "@$lib-desc")

	h.send("look portrait")
	if got := h.out(); !strings.Contains(got, "Registered and running.") {
		t.Errorf("the registered program did not run:\n%s", got)
	}
}

// TestExecOrNotifyPassesCommandAndArgs pins what the program is
// handed. Upstream sets match_cmdname to the caller context and
// match_args to the MPI-evaluated remainder — two different
// strings, which is why this does not just call SetReserved and stop.
func TestExecOrNotifyPassesCommandAndArgs(t *testing.T) {
	h := newHarness(t)
	h.login()

	prog := h.makeProgram(t, "echo.muf", `: main
  me @ swap "arg:" swap strcat notify
  me @ "cmd:" command @ strcat notify
;`)
	h.describeWith(t, "sign", "@"+
		strings.TrimPrefix(prog.String(), "#")+" {name:me}")

	h.send("look sign")
	got := h.out()
	// The argument text is MPI-evaluated before the program sees
	// it, so {name:me} arrives already expanded.
	if !strings.Contains(got, "arg:Wizard") {
		t.Errorf("the arguments were not the evaluated text:\n%s", got)
	}
	if !strings.Contains(got, "cmd:(@Desc)") {
		t.Errorf("COMMAND was not the caller context:\n%s", got)
	}
}

// TestExecOrNotifyFallsBackWhenNothingRuns covers the two branches
// upstream takes when the '@' named no program: the remainder is
// printed unparsed, and the nothing-special message stands in when
// there is no remainder.
func TestExecOrNotifyFallsBackWhenNothingRuns(t *testing.T) {
	h := newHarness(t)
	h.login()

	// Deliberately not MPI-parsed, which upstream's own comment
	// calls a crazy edge case and leaves alone.
	h.describeWith(t, "slate", "@9999 just {null:text}")
	h.send("look slate")
	if got := h.out(); !strings.Contains(got, "just {null:text}") {
		t.Errorf("the remainder was not printed verbatim:\n%s", got)
	}

	h.describeWith(t, "chalk", "@9999")
	h.send("look chalk")
	if got := h.out(); !strings.Contains(got, "You see nothing special.") {
		t.Errorf("a bare non-program did not fall back:\n%s", got)
	}
}

// TestExecOrNotifyLeavesOrdinaryTextAlone is the case that must not
// change: everything that does not begin with '@' is still MPI over
// the text, printed as before.
func TestExecOrNotifyLeavesOrdinaryTextAlone(t *testing.T) {
	h := newHarness(t)
	h.login()

	h.describeWith(t, "banner", "It reads: {name:me}.")
	h.send("look banner")
	if got := h.out(); !strings.Contains(got, "It reads: Wizard.") {
		t.Errorf("an ordinary description changed:\n%s", got)
	}
}

// TestPrefixMessage covers fbstrings.c's prefix_message, which
// parse_oprop always calls with SuppressIfPresent set.
func TestPrefixMessage(t *testing.T) {
	for _, tc := range []struct {
		text, want, why string
	}{
		{"smiles.", "Wizard smiles.", "the ordinary case"},
		{"'s hat glows.", "Wizard's hat glows.",
			"no space before a pose separator"},
		{"Wizard smiles.", "Wizard smiles.",
			"already prefixed, so left alone"},
		{"Wizardly things happen.",
			"Wizard Wizardly things happen.",
			"a longer word only starts the same way"},
		{"one\ntwo", "Wizard one\nWizard two",
			"every line is prefixed"},
		{"Wizard", "Wizard", "the prefix alone counts as present"},
	} {
		if got := prefixMessage(tc.text, "Wizard"); got != tc.want {
			t.Errorf("prefixMessage(%q) = %q, want %q (%s)",
				tc.text, got, tc.want, tc.why)
		}
	}
}

// TestSplitMesgWord pins the scan exec_or_notify does, which does not
// trim: "@ foo" has an empty first word upstream, so it names no
// program and prints "foo".
func TestSplitMesgWord(t *testing.T) {
	for _, tc := range [][3]string{
		{"123 args here", "123", "args here"},
		{"123", "123", ""},
		{" foo", "", "foo"},
		{"$lib-desc  two", "$lib-desc", " two"},
	} {
		w, r := splitMesgWord(tc[0])
		if w != tc[1] || r != tc[2] {
			t.Errorf("splitMesgWord(%q) = %q, %q; want %q, %q",
				tc[0], w, r, tc[1], tc[2])
		}
	}
}
