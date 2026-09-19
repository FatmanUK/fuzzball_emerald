package game

import (
	"context"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// makeProgrammer creates a player with mucker level 3, so a program it owns
// compiles with a real effective mlevel rather than being capped at 0 the
// way the harness's own "Wizard" player (Wizard|Builder, no mucker bits) —
// see CLAUDE.md's "a wizard with no mucker bits has level 0" — would cap it.
func makeProgrammer(w *world.World, name string) ref.Ref {
	p := w.Create(name, ref.TypePlayer, ref.Nothing)
	p.Owner = p.Ref
	p.Flags = p.Flags.SetMLevel(3)
	return p.Ref
}

// TestCanCallOwnerAlwaysReaches checks that CanCall's permission gate — the
// owner/wizard/Linkable check, separate from the public's own mlev floor —
// lets the target's own owner through even without being a wizard or the
// program being Linkable.
func TestCanCallOwnerAlwaysReaches(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(1, owner, p.Ref, "foo") {
			t.Error("the owner should reach a declared public despite not being a wizard")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallMlevelFourAlwaysReaches checks the other unconditional branch:
// a level-4 caller passes even for someone else's non-linkable program.
func TestCanCallMlevelFourAlwaysReaches(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(4, stranger.Ref, p.Ref, "foo") {
			t.Error("a level-4 caller should reach any declared public")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallDeniesNonLinkableStranger checks the permission gate's failing
// case: a low-level caller who neither owns the program nor finds it
// Linkable is refused before the public table is even consulted.
func TestCanCallDeniesNonLinkableStranger(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		stranger := w.Create("Stranger", ref.TypePlayer, ref.Nothing)
		stranger.Owner = stranger.Ref

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(1, stranger.Ref, p.Ref, "foo") {
			t.Error("a non-owning, non-wizard, non-linkable caller should be refused")
		}

		p.Flags |= ref.LinkOK
		if !host.CanCall(1, stranger.Ref, p.Ref, "foo") {
			t.Error("a LinkOK program should let a stranger call its publics")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallRespectsThePublicsOwnMlevelFloor checks that reaching the
// permission gate is not enough on its own: the caller must also meet the
// mlev the public itself was declared at (1 for "public", 4 for "wizcall").
func TestCanCallRespectsThePublicsOwnMlevelFloor(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\nwizcall foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(3, owner, p.Ref, "foo") {
			t.Error("a wizcall public should refuse a level-3 caller")
		}
		if !host.CanCall(4, owner, p.Ref, "foo") {
			t.Error("a wizcall public should accept a level-4 caller")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallIsCaseInsensitiveAndCompilesOnDemand checks the two mechanical
// details prim_cancallp handles itself: the public name is matched with
// strcasecmp, and a program that has never been run yet still answers
// correctly because CanCall compiles it rather than assuming it already is.
func TestCanCallIsCaseInsensitiveAndCompilesOnDemand(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if !host.CanCall(1, owner, p.Ref, "FOO") {
			t.Error("the public name should be matched case-insensitively")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallMissingNameFails checks that a name the target never declared
// public reports false rather than erroring.
func TestCanCallMissingNameFails(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := makeProgrammer(w, "Owner")
		p := w.Create("lib.muf", ref.TypeProgram, owner)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(3, owner, p.Ref, "bar") {
			t.Error("an undeclared name should not be callable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCanCallUncompiledMlevelZeroFails checks that ProgMLevel(target) > 0 is
// its own unconditional gate: a program whose effective mlevel is 0 — here,
// because its owner has no mucker bits — can never be called, even by its
// own owner.
func TestCanCallUncompiledMlevelZeroFails(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Do(context.Background(), func(w *world.World) {
		owner := w.Create("NoBits", ref.TypePlayer, ref.Nothing)
		owner.Owner = owner.Ref
		p := w.Create("lib.muf", ref.TypeProgram, owner.Ref)
		p.Flags = p.Flags.SetMLevel(3)
		w.SetSource(p.Ref, ": foo ;\npublic foo\n")

		host := &mufHost{s: h.s, w: w}
		if host.CanCall(4, owner.Ref, p.Ref, "foo") {
			t.Error("a program whose owner grants it mlevel 0 should never be callable")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
