package game

import (
	"context"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestExamineShowsOnlyTheOwnerToOthers checks the privacy gate:
// someone who neither controls an object nor passes its read lock is
// told who owns it and nothing else.
func TestExamineShowsOnlyTheOwnerToOthers(t *testing.T) {
	h := newHarness(t)
	h.login()

	var theirs, theirExit ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		// Someone else's thing, in the room, with a
		// description that must not leak.
		other := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		other.Owner = other.Ref

		here := w.Get(h.wizRef()).Location
		o := w.Create("locket", ref.TypeThing, other.Ref)
		o.Home = here
		o.Props.SetString(propDesc, "A secret.")
		if err := w.MoveTo(o.Ref, here); err != nil {
			t.Error(err)
		}
		theirs = o.Ref

		// An exit that points nowhere may be examined by
		// anyone, because anyone may link it.
		e := w.Create("gate", ref.TypeExit, other.Ref)
		if err := w.MoveTo(e.Ref, here); err != nil {
			t.Error(err)
		}
		theirExit = e.Ref
	}); err != nil {
		t.Fatal(err)
	}
	// The test wizard controls everything, so it has to ask as
	// someone who does not.
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		mortal := w.Create("Mortal", ref.TypePlayer, ref.Nothing)
		mortal.Owner = mortal.Ref

		c := &ctx{w: w, d: h.d, who: mortal.Ref, out: h.d.Send}
		s := h.s
		if s.canLink(w, mortal.Ref, theirs) {
			t.Error("a stranger should not be able to link someone else's thing")
		}
		if !s.canLink(w, mortal.Ref, theirExit) {
			t.Error("anyone may link an exit that points nowhere")
		}
		s.printOwner(c, theirs)
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.out(); !strings.Contains(got, "Owner: Stranger") ||
		strings.Contains(got, "A secret.") {
		t.Errorf("a stranger was shown too much:\n%s", got)
	}
}

// TestFlagDescriptionRenamesByType checks the flags that mean
// different things on different types, which is most of the
// interesting ones.
func TestFlagDescriptionRenamesByType(t *testing.T) {
	cases := []struct {
		typ   ref.ObjType
		flags ref.Flags
		want  string
	}{
		{ref.TypeThing, 0, "Type: THING"},
		{ref.TypeProgram, ref.Sticky, "Type: PROGRAM  Flags: SETUID"},
		{ref.TypePlayer, ref.Sticky, "Type: PLAYER  Flags: SILENT"},
		{ref.TypeThing, ref.Sticky, "Type: THING  Flags: STICKY"},
		{ref.TypeProgram, ref.Dark, "Type: PROGRAM  Flags: DEBUG"},
		{ref.TypeRoom, ref.Dark, "Type: ROOM  Flags: DARK"},
		{ref.TypePlayer, ref.ChownOK, "Type: PLAYER  Flags: COLOR"},
		{ref.TypeExit, ref.XForcible, "Type: EXIT/ACTION  Flags: XPRESS"},
		{ref.TypeThing, ref.XForcible, "Type: THING  Flags: XFORCIBLE"},
		{ref.TypeProgram, ref.Vehicle, "Type: PROGRAM  Flags: VIEWABLE"},
		{ref.TypeProgram, ref.Abode, "Type: PROGRAM  Flags: AUTOSTART"},
		{ref.TypeExit, ref.Abode, "Type: EXIT/ACTION  Flags: ABATE"},
		{ref.TypeThing, ref.Haven, "Type: THING  Flags: HIDE"},
		{ref.TypeRoom, ref.Guest, "Type: ROOM  Flags: NOGUEST"},
		{ref.TypePlayer, ref.Guest, "Type: PLAYER  Flags: GUEST"},
		// Order is fixed, and a mucker level appears as one
		// word.
		{ref.TypePlayer, ref.Wizard | ref.Mucker | ref.SMucker | ref.Builder,
			"Type: PLAYER  Flags: WIZARD MUCKER3 BUILDER"},
	}
	for _, tc := range cases {
		o := &world.Object{Flags: tc.flags.WithType(tc.typ), Props: props.New()}
		if got := flagDescription(o); got != tc.want {
			t.Errorf("flags %v on a %v = %q, want %q", tc.flags, tc.typ, got, tc.want)
		}
	}
}

// TestPropertyListingHidesSystemProps checks that the property lister
// keeps the server's own propdir to itself, and hidden props to
// wizards.
func TestPropertyListingHidesSystemProps(t *testing.T) {
	h := newHarness(t)
	h.login()
	h.send("@create box")
	h.out()

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		for _, r := range w.Contents(h.wizRef()) {
			o := w.Get(r)
			if o == nil || o.Name != "box" {
				continue
			}
			o.Props.SetString("open", "yes")
			o.Props.SetString("@secret", "wizards only")
			o.Props.SetString("@__sys__/private", "never")
		}
	}); err != nil {
		t.Fatal(err)
	}

	// A wizard sees the hidden property but never the system one.
	h.send("ex box=**")
	got := h.out()
	if !strings.Contains(got, "/open:yes") {
		t.Errorf("an ordinary property was not listed:\n%s", got)
	}
	if !strings.Contains(got, "/@secret:wizards only") {
		t.Errorf("a wizard should see a hidden property:\n%s", got)
	}
	if strings.Contains(got, "@__sys__") {
		t.Errorf("a system property was listed:\n%s", got)
	}
}

// TestCreationCosts checks that building takes money and gives an
// object a value, which is where an object's worth comes from.
func TestCreationCosts(t *testing.T) {
	h := newHarness(t)
	h.login()

	var mortal ref.Ref
	if err := h.engine.Do(context.Background(), func(w *world.World) {
		o := w.Create("Pauper", ref.TypePlayer, ref.Nothing)
		o.Owner = o.Ref
		o.Flags |= ref.Builder
		o.Props.Set(propValue, props.Value{Type: props.Int, Num: 12})
		if err := w.MoveTo(o.Ref, w.Get(h.wizRef()).Location); err != nil {
			t.Error(err)
		}
		mortal = o.Ref
	}); err != nil {
		t.Fatal(err)
	}

	// The mortal has no connection, so its replies are captured
	// rather than read off a descriptor.
	run := func(line string) string {
		h.t.Helper()
		var said []string
		if err := h.engine.Do(context.Background(), func(w *world.World) {
			verb, arg := trimCommand(line)
			c := &ctx{w: w, d: h.d, who: mortal, verb: verb, arg: arg,
				out: func(s string) { said = append(said, s) }}
			h.s.cmdCreate(c)
		}); err != nil {
			t.Fatal(err)
		}
		return strings.Join(said, "\n")
	}

	// The first object is affordable at the default cost of ten.
	if got := run("@create widget"); !strings.Contains(got, "created.") {
		t.Fatalf("@create said %q", got)
	}
	// The second is not: two pennies are left.
	if got := run("@create gizmo"); !strings.Contains(got, "don't have enough") {
		t.Errorf("a player with two pennies could still build: %q", got)
	}

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		if left := valueOf(w, mortal); left != 2 {
			t.Errorf("the builder has %d pennies left, want 2", left)
		}
		for _, r := range w.Contents(mortal) {
			if o := w.Get(r); o != nil &&
				o.Name == "widget" {
				if v := valueOf(w, r); v != 1 {
					t.Errorf("the widget is worth %d, want 1", v)
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}

	// A wizard pays for nothing.
	h.send("@create freebie")
	if got := h.out(); !strings.Contains(got, "created.") {
		t.Errorf("a wizard was charged: %q", got)
	}
}
