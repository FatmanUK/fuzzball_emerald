package muf

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// flagNames maps the names FLAG? and SET accept to their bits.
//
// Both a full name and its single-letter abbreviation work, which is what
// programs and @set both use.
var flagNames = map[string]ref.Flags{
	"abode": ref.Abode, "a": ref.Abode,
	"builder": ref.Builder, "b": ref.Builder,
	"chown_ok": ref.ChownOK, "c": ref.ChownOK,
	"dark": ref.Dark, "d": ref.Dark,
	"guest": ref.Guest, "g": ref.Guest,
	"haven": ref.Haven, "h": ref.Haven,
	"jump_ok": ref.JumpOK, "j": ref.JumpOK,
	"kill_ok": ref.KillOK, "k": ref.KillOK,
	"link_ok": ref.LinkOK, "l": ref.LinkOK,
	"overt": ref.Overt, "o": ref.Overt,
	"quell": ref.Quell, "q": ref.Quell,
	"sticky": ref.Sticky, "s": ref.Sticky,
	"vehicle": ref.Vehicle, "v": ref.Vehicle,
	"wizard": ref.Wizard, "w": ref.Wizard,
	"xforcible": ref.XForcible, "x": ref.XForcible,
	"yield": ref.Yield, "y": ref.Yield,
	"zombie": ref.Zombie, "z": ref.Zombie,
}

// flagNamed resolves a flag name to its bit.
func flagNamed(name string) (ref.Flags, bool) {
	f, ok := flagNames[ascii.Fold(name)]
	return f, ok
}

// hasFlag reports whether a flag word carries a named flag.
//
// The type names and the mucker levels are recognised too, because FLAG? takes
// them: "player?" is a separate primitive, but "FLAG? M3" is how a program asks
// about a mucker level.
func hasFlag(flags ref.Flags, name string) bool {
	n := ascii.Fold(name)
	if n == "" {
		return false
	}
	// A leading '!' inverts the test.
	if n[0] == '!' {
		return !hasFlag(flags, n[1:])
	}

	switch n {
	case "room", "r":
		return flags.Type() == ref.TypeRoom
	case "exit", "e":
		return flags.Type() == ref.TypeExit
	case "player", "p":
		return flags.Type() == ref.TypePlayer
	case "program", "f":
		return flags.Type() == ref.TypeProgram
	case "thing":
		return flags.Type() == ref.TypeThing
	case "m0":
		return flags.MLevel() == 0
	case "m1", "mucker":
		return flags.MLevel() == 1
	case "m2":
		return flags.MLevel() == 2
	case "m3":
		return flags.MLevel() == 3
	}

	if bit, ok := flagNamed(n); ok {
		return flags&bit != 0
	}
	return false
}

// equalFoldASCII compares two names the way property lookup does.
func equalFoldASCII(a, b string) bool { return ascii.EqualFold(a, b) }
