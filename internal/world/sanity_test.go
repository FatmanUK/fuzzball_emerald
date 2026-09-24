package world

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// sane builds a small, consistent world: a room (#0), a player in it
// (#1), a thing the player carries (#2), and an exit on the room
// (#3).
func sane(t *testing.T) *World {
	t.Helper()
	w := New()
	room := w.Create("Room", ref.TypeRoom, ref.Nothing)
	room.Location = ref.Nothing
	room.Dropto = ref.Nothing

	who := w.Create("Player", ref.TypePlayer, ref.Nothing)
	who.Owner = who.Ref
	who.Home = room.Ref
	room.Owner = who.Ref

	thing := w.Create("thing", ref.TypeThing, who.Ref)
	thing.Home = room.Ref

	exit := w.Create("out", ref.TypeExit, who.Ref)
	exit.Dest = []ref.Ref{room.Ref}

	for _, m := range []struct{ what, where ref.Ref }{
		{who.Ref, room.Ref}, {thing.Ref, who.Ref}, {exit.Ref, room.Ref},
	} {
		if err := w.MoveTo(m.what, m.where); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// check runs the checker and returns the problems as strings.
func check(w *World) []string {
	var out []string
	w.Check(func(string) {}, func(v Violation) {
		out = append(out, v.Ref.String()+" "+v.Problem)
	})
	return out
}

// hasProblem reports whether any finding mentions the text.
func hasProblem(found []string, text string) bool {
	for _, f := range found {
		if strings.Contains(f, text) {
			return true
		}
	}
	return false
}

func TestCheckPassesAHealthyWorld(t *testing.T) {
	if found := check(sane(t)); len(found) > 0 {
		t.Errorf("a healthy world reported problems:\n%s", strings.Join(found, "\n"))
	}
}

func TestCheckFindsDamage(t *testing.T) {
	cases := []struct {
		name   string
		damage func(w *World)
		want   string
	}{
		{"an owner that is not a player", func(w *World) {
			w.Get(2).Owner = 0 // the room
		}, "has a non-player object as its owner."},

		{"a location that cannot contain anything", func(w *World) {
			w.Get(2).Location = 3 // inside the exit
		}, "thinks it is located in a non-container object"},

		{"a home that is not a room", func(w *World) {
			w.Get(1).Home = 3 // a player homed to an exit
		}, "has its home set to a non-room object"},

		{"a drop-to that is an exit", func(w *World) {
			w.Get(0).Dropto = 3
		}, "has its dropto set to a non-room, non-thing object"},

		{"an exit pointing at nothing real", func(w *World) {
			w.Get(3).Dest = []ref.Ref{999}
		}, "has an invalid object as one of its link destinations"},

		{"a program with contents", func(w *World) {
			p := w.Create("prog", ref.TypeProgram, 1)
			p.Location = 0
			p.Contents = 2
		}, "is a program whose contents aren't #-1"},

		{"an object nothing points at", func(w *World) {
			w.Get(0).Contents = ref.Nothing
		}, "appears to be an orphan object"},

		{"an object two containers claim", func(w *World) {
			w.Get(0).Contents = 2 // the thing, which the player holds
		}, "is referred to by more than one object's"},

		{"a name that is missing", func(w *World) {
			w.Get(2).Name = ""
		}, "doesn't have a name"},

		{"a loop in a contents chain", func(w *World) {
			// The thing points back at itself, so walking
			// the player's contents never ends.
			w.Get(2).Next = 2
		}, "loop"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := sane(t)
			tc.damage(w)
			found := check(w)
			if !hasProblem(found, tc.want) {
				t.Errorf("did not report %q; found:\n%s",
					tc.want, strings.Join(found, "\n"))
			}
		})
	}
}

// TestFixRepairsWhatCheckFinds damages a world in every way the fixer
// claims to handle, repairs it, and checks that nothing is left.
func TestFixRepairsWhatCheckFinds(t *testing.T) {
	w := sane(t)
	// The starting room has to exist for players to be sent to
	// it.
	if err := w.SetTune("player_start", "#0"); err != nil {
		t.Fatal(err)
	}

	w.Get(2).Name = ""    // a nameless thing
	w.Get(2).Owner = 0    // owned by a room
	w.Get(2).Location = 3 // located in an exit
	w.Get(1).Home = 3     // a player homed to an exit
	w.Get(0).Dropto = 3   // a drop-to that is an exit
	w.Get(3).Dest = []ref.Ref{999}
	w.Get(0).Contents = ref.Nothing // orphaning everything in the room

	log, unfixed := w.Fix()
	if len(unfixed) > 0 {
		t.Errorf("repair left %d problems it could not fix: %+v", len(unfixed), unfixed)
	}
	if len(log) == 0 {
		t.Error("repair changed nothing")
	}
	if found := check(w); len(found) > 0 {
		t.Errorf("problems survived the repair:\n%s\nrepair log:\n%s",
			strings.Join(found, "\n"), strings.Join(log, "\n"))
	}
}

// TestFixReportsWhatItCannotRepair checks that damage with nothing
// left to reason from is reported rather than papered over.
func TestFixReportsWhatItCannotRepair(t *testing.T) {
	w := sane(t)
	if err := w.SetTune("player_start", "#0"); err != nil {
		t.Fatal(err)
	}
	// Type 7 is not a type the database defines.
	w.Get(2).Flags = w.Get(2).Flags&^ref.Flags(ref.TypeMask) | ref.Flags(7)

	_, unfixed := w.Fix()
	if len(unfixed) != 1 ||
		!strings.Contains(unfixed[0].Problem, "unknown object type") {
		t.Errorf("expected one unfixable finding, got %+v", unfixed)
	}
}

// TestFixCreatesLostAndFound checks that an object whose owner cannot
// be worked out is given somewhere to go rather than being discarded.
func TestFixCreatesLostAndFound(t *testing.T) {
	w := sane(t)
	if err := w.SetTune("player_start", "#0"); err != nil {
		t.Fatal(err)
	}
	w.Get(2).Owner = 999
	w.Get(2).Location = 999

	log, _ := w.Fix()
	if !hasProblem(log, "lost+found") {
		t.Fatalf("no lost+found was made:\n%s", strings.Join(log, "\n"))
	}
	if o := w.Get(2); !w.Valid(o.Owner) ||
		w.Get(o.Owner).Type() != ref.TypePlayer {
		t.Errorf("the thing is still owned by %v", w.Get(2).Owner)
	}
	if found := check(w); len(found) > 0 {
		t.Errorf("problems survived:\n%s", strings.Join(found, "\n"))
	}
}
