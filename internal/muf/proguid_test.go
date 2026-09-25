package muf

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// uidHost answers just enough for find_uid.
type uidHost struct {
	Host
	owner map[ref.Ref]ref.Ref
	flags map[ref.Ref]ref.Flags
}

func (h *uidHost) Owner(r ref.Ref) ref.Ref   { return h.owner[r] }
func (h *uidHost) Flags(r ref.Ref) ref.Flags { return h.flags[r] }

// TestProgUIDBranches walks all four of find_uid's branches.
//
// The refs: #10 is the program, #11 its owner, #20 whoever is running
// it, #21 their owner, #30 the trigger, #31 the trigger's owner.
func TestProgUIDBranches(t *testing.T) {
	const (
		prog, progOwner     = ref.Ref(10), ref.Ref(11)
		runner, runnerOwner = ref.Ref(20), ref.Ref(21)
		trig, trigOwner     = ref.Ref(30), ref.Ref(31)
	)

	for _, tc := range []struct {
		name   string
		flags  ref.Flags
		perms  Perms
		mlevel int
		trig   ref.Ref
		want   ref.Ref
	}{
		{"ordinary REGUID at mlevel 3", 0, RegUID, 3, trig,
			runnerOwner},
		{"below mlevel 2 is always the program's owner", 0, RegUID,
			1, trig, progOwner},
		{"STICKY acts as the program's owner", ref.Sticky, RegUID,
			3, trig, progOwner},
		{"SetUID does the same without the flag", 0, SetUID, 3,
			trig, progOwner},
		{"HAVEN acts as the trigger's owner", ref.Haven, RegUID, 3,
			trig, trigOwner},
		{"HardUID does the same without the flag", 0, HardUID, 3,
			trig, trigOwner},
		{"HardUID with no trigger falls back", 0, HardUID, 3,
			ref.Nothing, progOwner},
		// STICKY is tested before the mucker level, so it
		// wins even where the level would have said the same
		// thing.
		{"STICKY beats HAVEN", ref.Sticky | ref.Haven, HardUID, 3,
			trig, progOwner},
	} {
		h := &uidHost{
			owner: map[ref.Ref]ref.Ref{
				prog: progOwner, runner: runnerOwner,
				trig: trigOwner,
			},
			flags: map[ref.Ref]ref.Flags{prog: tc.flags},
		}
		f := &Frame{
			Prog:   &Program{Ref: prog, MLevel: tc.mlevel},
			Caller: runner,
			Trig:   tc.trig,
			Perms:  tc.perms,
		}
		if got := f.progUID(h); got != tc.want {
			t.Errorf("%s: progUID = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}

// TestForkDropsPerms pins an upstream oddity: prim_fork copies trig
// but not perms, so a HARDUID program's child runs REGUID.
func TestForkDropsPerms(t *testing.T) {
	f := &Frame{
		Prog:  &Program{Ref: 10, Code: []Inst{{}}},
		Trig:  30,
		Perms: HardUID,
	}
	child := f.fork()
	if child.Perms != RegUID {
		t.Errorf("forked Perms = %v, want RegUID: upstream's "+
			"prim_fork does not copy it", child.Perms)
	}
	if child.Trig != f.Trig {
		t.Errorf("forked Trig = %v, want %v: upstream does copy "+
			"that one", child.Trig, f.Trig)
	}
}
