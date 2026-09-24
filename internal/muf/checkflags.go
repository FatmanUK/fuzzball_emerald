package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// flagCheck is upstream's struct flgchkdat: a compiled flag-match
// expression, as ARRAY_FILTER_FLAGS and FINDNEXT take and @find and
// @owned parse.
//
// The language is a string of single characters, each a test, with
// '!' negating the one that follows it. Negation is per-character
// rather than per-expression, which is why every test here comes in a
// positive and a negative half rather than one half and a flag.
type flagCheck struct {
	forType bool
	isType  ref.ObjType

	notRoom, notExit, notThing, notPlayer, notProg bool

	forLevel bool
	isLevel  int
	notLevel [4]bool

	setFlags, clearFlags ref.Flags

	forLink  bool
	isLinked bool

	forOld bool
	isOld  bool
}

// parseFlagCheck is upstream's init_checkflags, minus its output-type
// half: the commands that parse one of these strings also read a
// display mode off the end of it, after an '=' , which the two
// primitives using this never pass. Anything after an '=' is
// therefore dropped rather than interpreted.
//
// Upstream's size tests, '~' and '^', are parsed and then ignored.
// They compare against size_object, this server's objects are laid
// out nothing like the C's, and examine's own "Memory used" line is
// masked in the golden case for exactly that reason — so a size
// clause filters nothing here rather than filtering by a number that
// would not mean the same thing.
func parseFlagCheck(flags string) flagCheck {
	if i := strings.IndexByte(flags, '='); i >= 0 {
		flags = flags[:i]
	}

	var c flagCheck
	// mode counts down: '!' sets it to 2 so it survives the
	// decrement at the end of its own iteration and applies to
	// the next character only.
	mode := 0
	for i := 0; i < len(flags); i++ {
		ch := upperByte(flags[i])
		neg := mode != 0
		switch ch {
		case '!':
			if mode != 0 {
				mode = 0
			} else {
				mode = 2
			}
		case 'R':
			c.setType(neg, ref.TypeRoom, &c.notRoom)
		case 'T':
			c.setType(neg, ref.TypeThing, &c.notThing)
		case 'E':
			c.setType(neg, ref.TypeExit, &c.notExit)
		case 'P':
			c.setType(neg, ref.TypePlayer, &c.notPlayer)
		case 'F':
			c.setType(neg, ref.TypeProgram, &c.notProg)
		case '~', '^':
			// Skip the size's digits so they are not read
			// as level tests.
			for i+1 < len(flags) && flags[i+1] >= '0' &&
				flags[i+1] <= '9' {
				i++
			}
		case 'U':
			c.forLink, c.isLinked = true, neg
		case '@':
			c.forOld, c.isOld = true, !neg
		case '0', '1', '2', '3':
			level := int(ch - '0')
			if neg {
				c.notLevel[level] = true
			} else {
				c.forLevel, c.isLevel = true, level
			}
		case 'M':
			// 'M' is "has any mucker level", so its two
			// halves are the other way round from the
			// digits': plain M excludes level 0, and !M
			// asks for exactly level 0.
			if neg {
				c.forLevel, c.isLevel = true, 0
			} else {
				c.notLevel[0] = true
			}
		case ' ':
			// A space after '!' re-arms the negation for
			// the next character rather than consuming
			// it.
			if mode != 0 {
				mode = 2
			}
		default:
			if flag, ok := checkFlagLetters[ch]; ok {
				if neg {
					c.clearFlags |= flag
				} else {
					c.setFlags |= flag
				}
			}
		}
		if mode != 0 {
			mode--
		}
	}
	return c
}

func (c *flagCheck) setType(neg bool, t ref.ObjType, not *bool) {
	if neg {
		*not = true
		return
	}
	c.forType, c.isType = true, t
}

// checkFlagLetters is upstream's own letter-to-flag mapping inside
// init_checkflags. It is not the same set as the flags examine
// prints: there is no letter here for the internal ones.
var checkFlagLetters = map[byte]ref.Flags{
	'A': ref.Abode,
	'B': ref.Builder,
	'C': ref.ChownOK,
	'D': ref.Dark,
	'G': ref.Guest,
	'H': ref.Haven,
	'J': ref.JumpOK,
	'K': ref.KillOK,
	'L': ref.LinkOK,
	'O': ref.Overt,
	'Q': ref.Quell,
	'S': ref.Sticky,
	'V': ref.Vehicle,
	'W': ref.Wizard,
	'X': ref.XForcible,
	'Y': ref.Yield,
	'Z': ref.Zombie,
}

// matches is upstream's checkflags: whether one object satisfies the
// expression.
func (c flagCheck) matches(h Host, what ref.Ref) bool {
	t := h.ObjType(what)
	if c.forType && t != c.isType {
		return false
	}
	for _, bad := range [...]struct {
		set bool
		typ ref.ObjType
	}{
		{c.notRoom, ref.TypeRoom},
		{c.notExit, ref.TypeExit},
		{c.notThing, ref.TypeThing},
		{c.notPlayer, ref.TypePlayer},
		{c.notProg, ref.TypeProgram},
	} {
		if bad.set && t == bad.typ {
			return false
		}
	}

	flags := h.Flags(what)
	level := flags.RawMLevel()
	if c.forLevel && level != c.isLevel {
		return false
	}
	if c.notLevel[level] {
		return false
	}

	if flags&c.clearFlags != 0 {
		return false
	}
	if ^flags&c.setFlags != 0 {
		return false
	}

	if c.forLink && linked(h, what, t) != c.isLinked {
		return false
	}

	if c.forOld && recentlyTouched(h, what) == c.isOld {
		return false
	}
	return true
}

// linked is checkflags' own per-type idea of what having a link
// means: a room has a drop-to, an exit has at least one destination,
// and a player or thing always counts as linked because its home
// always is one.
func linked(h Host, what ref.Ref, t ref.ObjType) bool {
	switch t {
	case ref.TypeRoom, ref.TypeExit:
		return len(h.Links(what)) > 0
	case ref.TypePlayer, ref.TypeThing:
		return true
	default:
		return false
	}
}

// recentlyTouched is the inverse of checkflags' "old" test: an object
// counts as old only when both its last use and its last change are
// further back than the aging_time parameter.
func recentlyTouched(h Host, what ref.Ref) bool {
	_, modified, used, _ := h.Timestamps(what)
	now := h.Now().Unix()
	aging := int64(h.TuneSpan("aging_time").Seconds())
	return now-used < aging || now-modified < aging
}

func upperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}
