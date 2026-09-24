package ref

import "strconv"

// ObjType is the low three bits of an object's flag word.
type ObjType uint32

// Object types. These bit values are stored in legacy database dumps
// and must not be renumbered. 0x5 is unused; Fuzzball never assigned
// it.
const (
	TypeRoom    ObjType = 0x0
	TypeThing   ObjType = 0x1
	TypeExit    ObjType = 0x2
	TypePlayer  ObjType = 0x3
	TypeProgram ObjType = 0x4
	TypeGarbage ObjType = 0x6
	NoType      ObjType = 0x7

	TypeMask ObjType = 0x7
)

func (t ObjType) String() string {
	switch t {
	case TypeRoom:
		return "room"
	case TypeThing:
		return "thing"
	case TypeExit:
		return "exit"
	case TypePlayer:
		return "player"
	case TypeProgram:
		return "program"
	case TypeGarbage:
		return "garbage"
	case NoType:
		return "notype"
	default:
		return "type(" + strconv.Itoa(int(t)) + ")"
	}
}

// Flags is an object's flag word, type bits included.
type Flags uint32

// Object flags. Bit positions are dump-format compatible with
// Fuzzball 7.
//
// Flags marked "internal" describe live server state rather than
// anything worth persisting; see DumpMask.
const (
	Wizard        Flags = 0x10       // gets automatic control
	LinkOK        Flags = 0x20       // anybody can link to this
	Dark          Flags = 0x40       // contents are not listed
	Internal      Flags = 0x80       // internal use only
	Sticky        Flags = 0x100      // goes home when dropped
	Builder       Flags = 0x200      // may use construction commands
	ChownOK       Flags = 0x400      // may be @chowned; on a player, sees colour
	JumpOK        Flags = 0x800      // room may be jumped from, player jumped to
	KillOK        Flags = 0x4000     // may be killed
	Guest         Flags = 0x8000     // guest account
	Haven         Flags = 0x10000    // cannot kill here
	Abode         Flags = 0x20000    // may set home here
	Mucker        Flags = 0x40000    // programmer, high bit of mucker level
	Quell         Flags = 0x80000    // wizard permissions suppressed
	SMucker       Flags = 0x100000   // programmer, low bit of mucker level
	Interactive   Flags = 0x200000   // internal: player is in the MUF editor
	ObjectChanged Flags = 0x400000   // internal: object is dirty
	Vehicle       Flags = 0x1000000  // vehicle
	Zombie        Flags = 0x2000000  // zombie
	Listener      Flags = 0x4000000  // internal: has listen props
	XForcible     Flags = 0x8000000  // externally forcible
	ReadMode      Flags = 0x10000000 // internal: player is in a READ
	SaneBit       Flags = 0x20000000 // internal: database sanity check marker
	Yield         Flags = 0x40000000 // yield
	Overt         Flags = 0x80000000 // overt
)

// DumpMask is the set of flags that describe live server state. They
// are cleared when an object is loaded and never written back.
const DumpMask = Interactive | ObjectChanged | Listener | ReadMode | SaneBit

// Mucker levels.
const (
	MLevNone       = 0
	MLevApprentice = 1
	MLevJourneyman = 2
	MLevMaster     = 3
	MLevWizard     = 4
)

// Type returns the object type encoded in the flag word.
func (f Flags) Type() ObjType { return ObjType(f) & TypeMask }

// WithType returns f with its type bits replaced by t.
func (f Flags) WithType(t ObjType) Flags {
	return (f &^ Flags(TypeMask)) | Flags(t&TypeMask)
}

// Has reports whether every flag in mask is set.
func (f Flags) Has(mask Flags) bool { return f&mask == mask }

// RawMLevel is the mucker level encoded in the MUCKER and SMUCKER
// bits alone, between 0 and 3. It ignores the WIZARD bit.
func (f Flags) RawMLevel() int {
	lvl := 0
	if f&Mucker != 0 {
		lvl += 2
	}
	if f&SMucker != 0 {
		lvl++
	}
	return lvl
}

// MLevel is the effective mucker level, between 0 and 4. A wizard
// with either mucker bit set is level 4; otherwise the raw bits
// decide.
func (f Flags) MLevel() int {
	if f&Wizard != 0 && f&(Mucker|SMucker) != 0 {
		return MLevWizard
	}
	return f.RawMLevel()
}

// SetMLevel returns f with its mucker bits set to level, clamped to
// 0..3.
func (f Flags) SetMLevel(level int) Flags {
	f &^= Mucker | SMucker
	if level >= 2 {
		f |= Mucker
	}
	if level%2 == 1 {
		f |= SMucker
	}
	return f
}

// IsWizard reports whether the object has unquelled wizard powers.
func (f Flags) IsWizard() bool {
	return f&Wizard != 0 && f&Quell == 0
}

// IsTrueWizard reports whether the WIZARD bit is set, quelled or not.
func (f Flags) IsTrueWizard() bool { return f&Wizard != 0 }

// CanBuild reports whether the object may use construction commands.
func (f Flags) CanBuild() bool { return f&(Wizard|Builder) != 0 }

// typeCodes maps an object type to its @examine letter. Things render
// as no letter at all.
//
// Fuzzball 7 indexes the literal "R-EPFG" by type, which is off by
// one for TYPE_GARBAGE (0x6): it reads the string's NUL terminator,
// so upstream @examine prints an empty flag string for garbage. Type
// 0x5 was freed and garbage renumbered without the table following.
// We render the intended 'G'.
var typeCodes = map[ObjType]byte{
	TypeRoom:    'R',
	TypeExit:    'E',
	TypePlayer:  'P',
	TypeProgram: 'F',
	TypeGarbage: 'G',
}

// flagLetters lists the flag letters in the order @examine prints
// them.
var flagLetters = []struct {
	bit    Flags
	letter byte
}{
	{Wizard, 'W'}, {LinkOK, 'L'}, {KillOK, 'K'}, {Dark, 'D'}, {Sticky, 'S'},
	{Quell, 'Q'}, {Builder, 'B'}, {ChownOK, 'C'}, {JumpOK, 'J'}, {Guest, 'G'},
	{Haven, 'H'}, {Abode, 'A'}, {Vehicle, 'V'}, {XForcible, 'X'},
	{Zombie, 'Z'}, {Yield, 'Y'}, {Overt, 'O'},
}

// Unparse renders a flag word the way @examine and the object
// matchers do: a leading type letter (omitted for things) followed by
// one letter per flag, then M1/M2/M3 for the mucker level.
func (f Flags) Unparse() string {
	buf := make([]byte, 0, 24)
	if c, ok := typeCodes[f.Type()]; ok {
		buf = append(buf, c)
	}
	for _, fl := range flagLetters {
		if f&fl.bit != 0 {
			buf = append(buf, fl.letter)
		}
	}
	if lvl := f.RawMLevel(); lvl != 0 {
		buf = append(buf, 'M', byte('0'+lvl))
	}
	return string(buf)
}
