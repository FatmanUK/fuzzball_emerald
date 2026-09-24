package ref

import "github.com/FatmanUK/fuzzball_emerald/internal/ascii"

// flagNames maps a flag's spellings to its bit, in the order
// upstream's str_to_flag tests them.
//
// A name given is matched as a *prefix* of one of these, not for
// equality: "d" is the DARK flag because "dark" is the first entry it
// prefixes, and "m" is MUCKER for the same reason. The order
// therefore decides what an ambiguous abbreviation means, so it is
// upstream's own rather than alphabetical — and a lookup must walk
// the list rather than hash it.
var flagNames = []struct {
	spellings []string
	bit       Flags
}{
	{[]string{"abode", "autostart", "abate"}, Abode},
	{[]string{"builder", "bound"}, Builder},
	{[]string{"chown_ok", "color"}, ChownOK},
	{[]string{"dark", "debug"}, Dark},
	{[]string{"guest"}, Guest},
	{[]string{"haven", "hide", "harduid"}, Haven},
	{[]string{"interactive"}, Interactive},
	{[]string{"jump_ok"}, JumpOK},
	{[]string{"kill_ok"}, KillOK},
	{[]string{"link_ok"}, LinkOK},
	{[]string{"mucker"}, Mucker},
	{[]string{"nucker"}, SMucker},
	{[]string{"overt"}, Overt},
	{[]string{"quell"}, Quell},
	{[]string{"sticky", "silent", "setuid"}, Sticky},
	{[]string{"vehicle", "viewable"}, Vehicle},
	{[]string{"wizard"}, Wizard},
	{[]string{"truewizard"}, Wizard},
	{[]string{"xforcible", "xpress"}, XForcible},
	{[]string{"yield"}, Yield},
	{[]string{"zombie"}, Zombie},
}

// FlagNamed resolves a flag name, or any unambiguous prefix of one,
// to its bit — upstream's str_to_flag.
func FlagNamed(name string) (Flags, bool) {
	name = ascii.Fold(name)
	if name == "" {
		return 0, false
	}
	for _, f := range flagNames {
		for _, s := range f.spellings {
			if len(name) <= len(s) &&
				s[:len(name)] == name {
				return f.bit, true
			}
		}
	}
	return 0, false
}

// HasNamed reports whether an object carries a named flag —
// upstream's has_flag, which both MUF's FLAG? and MPI's {flag?} call.
//
// Two things about it are easy to get wrong. It knows only *flags*: a
// type name or a mucker level is not one, so "{flag?:me,player}" is
// false even for a player, and PLAYER? is the primitive that answers
// that. And plain "wizard" asks whether the object's wizard powers
// are actually in effect, so a quelled wizard is not one —
// "truewizard" is the spelling that reads the bit itself.
//
// A leading '!' inverts the test, and may be repeated.
func (flags Flags) HasNamed(name string) bool {
	negated := false
	for len(name) > 0 && name[0] == '!' {
		name = name[1:]
		negated = !negated
	}

	trueWizard := isPrefixFold("truewizard", name)
	bit, ok := FlagNamed(name)

	var result bool
	switch {
	case ok && bit == Wizard && !trueWizard:
		result = flags.IsWizard()
	case ok:
		result = flags&bit != 0
	}
	return result != negated
}

// isPrefixFold reports whether given is a prefix of full, ignoring
// case — upstream's string_prefix, whose argument order is the
// other way round.
func isPrefixFold(full, given string) bool {
	given = ascii.Fold(given)
	return given != "" && len(given) <= len(full) && full[:len(given)] == given
}
