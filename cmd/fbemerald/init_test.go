package main

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// TestMinimalWorldIsBootable checks the world "fbemerald init" writes
// against what the server needs to serve it.
//
// Every one of these is load-bearing rather than incidental:
// player_start, default_room_parent and lost_and_found all default to
// #0, so #0 must exist and must be a room; the login path needs the
// player indexed by name with a hash that verifies; and a wizard with
// no mucker bits has level 0, which find_mlev would then apply to
// every program they own.
func TestMinimalWorldIsBootable(t *testing.T) {
	w, err := minimalWorld("Nexus", "Wizard", "secret")
	if err != nil {
		t.Fatal(err)
	}

	room := w.Get(ref.GlobalEnvironment)
	if room == nil {
		t.Fatal("no #0")
	}
	if room.Type() != ref.TypeRoom {
		t.Errorf("#0 is %v, want a room", room.Type())
	}
	if room.Owner != ref.God {
		t.Errorf("#0 owned by %v, want #1", room.Owner)
	}
	if _, ok := w.GetProp(ref.GlobalEnvironment, "_/de"); !ok {
		t.Error("#0 has no description, so look says nothing")
	}

	wiz := w.Get(ref.God)
	if wiz == nil {
		t.Fatal("no #1")
	}
	if wiz.Type() != ref.TypePlayer {
		t.Errorf("#1 is %v, want a player", wiz.Type())
	}
	if wiz.Owner != ref.God {
		t.Errorf("#1 owned by %v, want itself", wiz.Owner)
	}
	if !wiz.Flags.IsWizard() {
		t.Error("#1 is not a wizard")
	}
	if wiz.Flags&ref.Builder == 0 {
		t.Error("#1 is not a builder, so cannot @dig")
	}
	// Raw level 3 plus the Wizard bit, which is what makes
	// find_mlev answer 4 — and what stops it answering 0.
	if got := wiz.Flags.RawMLevel(); got != 3 {
		t.Errorf("#1 raw mucker level = %d, want 3", got)
	}
	if got := wiz.Flags.MLevel(); got != ref.MLevWizard {
		t.Errorf("#1 effective mucker level = %d, want %d",
			got, ref.MLevWizard)
	}
	if wiz.Location != ref.GlobalEnvironment {
		t.Errorf("#1 is at %v, want #0", wiz.Location)
	}
	if wiz.Home != ref.GlobalEnvironment {
		t.Errorf("#1's home is %v, want #0", wiz.Home)
	}

	if r, ok := w.PlayerNamed("wizard"); !ok || r != ref.God {
		t.Errorf("PlayerNamed = %v, %v; want #1, true "+
			"— the login path needs the index",
			r, ok)
	}
	res := password.Verify(wiz.PasswordHash, "secret")
	if !res.OK {
		t.Error("the password does not verify")
	}
	if res := password.Verify(wiz.PasswordHash, "wrong"); res.OK {
		t.Error("the wrong password verifies")
	}
	// A fresh hash must not ask to be upgraded; that flag is for
	// the two legacy formats the importer brings in.
	if res.NeedsUpgrade {
		t.Error("a freshly written hash wants upgrading")
	}
}

// TestMinimalWorldRefusesUnusableNames applies ok_object_name to the
// operator's own input, through the same game.NameOK the game uses
// — a world whose first room is called "here" could not be referred
// to by any command.
func TestMinimalWorldRefusesUnusableNames(t *testing.T) {
	for _, tc := range []struct{ room, wiz, why string }{
		{"here", "Wizard", "a matcher-reserved room name"},
		{"Nexus", "me", "a matcher-reserved player name"},
		{"", "Wizard", "a blank room name"},
		{"Nexus", "  ", "a whitespace-only wizard name"},
		{"#0", "Wizard", "a room name starting with '#'"},
		{"Nex=us", "Wizard", "a room name with '='"},
	} {
		_, err := minimalWorld(tc.room, tc.wiz, "x")
		if err == nil {
			t.Errorf("%s was accepted: room=%q "+
				"wizard=%q", tc.why, tc.room,
				tc.wiz)
		}
	}
}

// TestReadPasswordLineKeepsSurroundingSpaces is the detail that locks
// somebody out if it is wrong: a password may begin or end with a
// space, so only the line ending is stripped.
func TestReadPasswordLineKeepsSurroundingSpaces(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"secret\n", "secret"},
		{"secret\r\n", "secret"},
		{"secret", "secret"},
		{" pad ding \n", " pad ding "},
		{"two words\nignored\n", "two words"},
	} {
		got, err := readPasswordFrom(strings.NewReader(tc.in))
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q gave %q, want %q", tc.in, got,
				tc.want)
		}
	}
	_, err := readPasswordFrom(strings.NewReader("\n"))
	if err == nil {
		t.Error("an empty password was accepted")
	}
}
