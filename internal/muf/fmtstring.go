package muf

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// refNothing is #-1, which a missing dbref field renders as.
const refNothing = ref.Nothing

// The sprintf-alike shared by FMTSTRING and ARRAY_FMTSTRINGS.
//
// Upstream implements the two separately — its own comment on
// prim_array_fmtstrings calls the overlap "a nasty amount" and the
// merge too big a job — but they are one grammar with two ways of
// reaching an argument: FMTSTRING pops each from the stack as it
// goes, ARRAY_FMTSTRINGS looks each up in a dictionary by the
// "[name]" the directive carries. That difference is the fmtDialect
// below; everything else is common.
//
// A directive is
//
//	% [- or |] [+ or space] [0] [width or *] [.precision or .*] [[field]] verb
//
// where '-' left-justifies, '|' centres, and the verbs are i
// (integer), s and S (string), d (a dbref as "#123"), D (a dbref as
// its name), l (a lock), f/e/g (a float), ? (the value's type name)
// and ~ (whichever of those suits the value's own type).

func init() {
	register("ARRAY_FMTSTRINGS", func(f *Frame) (*Result, error) {
		formatV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		rowsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if rowsV.Type != TypeArray {
			return nil, errf("Argument not an array of arrays. (1)")
		}
		for _, row := range rowsV.Array.Values() {
			if row.Type != TypeArray {
				return nil, errf("Argument not a homogenous array of arrays. (1)")
			}
		}
		if formatV.Type != TypeString {
			return nil, errf("Expected string argument. (2)")
		}
		h := f.hostOrNil()

		rows := rowsV.Array.Values()
		out := make([]Value, 0, len(rows))
		for _, row := range rows {
			s, err := formatWith(h, formatV.Str, rowDialect(row.Array))
			if err != nil {
				return nil, err
			}
			out = append(out, Str(s))
		}
		return nil, f.Push(Arr(NewList(out)))
	})
}

// stackDialect is FMTSTRING's: each directive takes the next value
// off the stack, and '*' takes a width from there too.
//
// Upstream pops as it walks the format left to right, so the first
// directive gets whatever is on top — "1 2 \"%i and %i\" fmtstring"
// reads as "2 and 1".
func (f *Frame) stackDialect() fmtDialect {
	return fmtDialect{
		arg: func(string, byte) (Value, error) { return f.Pop() },
		star: func() (int, error) {
			n, err := f.popInt()
			if err != nil {
				return 0, errf("Format specified integer argument not found.")
			}
			if n < 0 {
				return 0, errf("Dynamic precision value must be a positive integer.")
			}
			return int(n), nil
		},
	}
}

// rowDialect is ARRAY_FMTSTRINGS's: each directive names a key in the
// row it is rendering, looked up as an integer first and then as a
// string.
//
// A key the row does not hold is not an error; the directive gets an
// empty value of whatever type its verb asks for, so one missing
// field leaves a gap rather than failing the whole array.
func rowDialect(row *Array) fmtDialect {
	return fmtDialect{
		needField: true,
		arg: func(field string, verb byte) (Value, error) {
			if n, err := strconv.Atoi(field); err == nil {
				if v, ok := row.Get(Int(int64(n))); ok {
					return v, nil
				}
			}
			if v, ok := row.Get(Str(field)); ok {
				return v, nil
			}
			return emptyForVerb(verb), nil
		},
	}
}

// emptyForVerb is what ARRAY_FMTSTRINGS substitutes for a field the
// row does not have: a zero value of the type the verb was going to
// want.
func emptyForVerb(verb byte) Value {
	switch verb {
	case 'l':
		return Value{Type: TypeLock}
	case 'i':
		return Int(0)
	case 'e', 'f', 'g':
		return Float(0)
	case 'd', 'D':
		return Obj(refNothing)
	default:
		return Str("")
	}
}

// fmtDialect is what differs between the two primitives.
type fmtDialect struct {
	// arg supplies one directive's value. field is the "[name]"
	// text, empty when the directive carried none.
	arg func(field string, verb byte) (Value, error)
	// star reads a '*' width or precision from the stack.
	// ARRAY_FMTSTRINGS has no dynamic widths, and leaves this
	// nil.
	star func() (int, error)
	// needField is set by the dialect whose directives must name
	// a field.
	needField bool
}

// formatWith renders one format string.
func formatWith(h Host, format string, d fmtDialect) (string, error) {
	var b strings.Builder
	// tabStop counts the characters written since the last tab
	// stop or carriage return, which is what "\t" in a format
	// aligns against.
	tabStop := 0

	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			switch {
			case format[i] == '\\' && i+1 < len(format) && format[i+1] == 't':
				// Upstream advances to the next
				// multiple of eight, always writing
				// at least one space.
				b.WriteByte(' ')
				for tabStop = tabStop + 1; tabStop%8 != 0; tabStop++ {
					b.WriteByte(' ')
				}
				tabStop = 0
				i++
			case format[i] == '\r':
				b.WriteByte('\r')
				tabStop = 0
			default:
				b.WriteByte(format[i])
				tabStop++
			}
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}

		spec, next, err := parseDirective(format, i+1, d)
		if err != nil {
			return "", err
		}
		i = next
		out, err := spec.render(h, d)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
		tabStop += len(out)
	}
	return b.String(), nil
}

// justify is which way a directive's output sits in its field.
type justify int

const (
	justifyRight justify = iota
	justifyLeft
	justifyCentre
)

// directive is one parsed "%..." in a format.
type directive struct {
	just      justify
	plus      bool // '+': always show a sign
	space     bool // ' ': a space where a sign would go
	zero      bool // '0': pad numbers with zeros
	width     int  // -1 when the directive gave none
	precision int  // -1 when the directive gave none
	field     string
	verb      byte
}

// parseDirective reads one directive starting just past its '%',
// returning the index of its last character.
func parseDirective(format string, i int, d fmtDialect) (directive, int, error) {
	spec := directive{width: -1, precision: -1}
	invalid := errf("Invalid format string.")

	if i < len(format) {
		switch format[i] {
		case '-':
			spec.just, i = justifyLeft, i+1
		case '|':
			spec.just, i = justifyCentre, i+1
		}
	}
	if i < len(format) {
		switch format[i] {
		case '+':
			spec.plus, i = true, i+1
		case ' ':
			spec.space, i = true, i+1
		}
	}
	if i < len(format) && format[i] == '0' {
		spec.zero, i = true, i+1
	}

	var err error
	if spec.width, i, err = fmtNumber(format, i, d, invalid); err != nil {
		return spec, i, err
	}
	if i < len(format) && format[i] == '.' {
		i++
		if spec.precision, i, err = fmtNumber(format, i, d, invalid); err != nil {
			return spec, i, err
		}
		if spec.precision < 0 {
			return spec, i, invalid
		}
	}

	if i < len(format) && format[i] == '[' {
		end := strings.IndexByte(format[i:], ']')
		if end < 0 {
			return spec, i, errf("Specified format field didn't have an " +
				"array index terminator ']'.")
		}
		spec.field, i = format[i+1:i+end], i+end+1
	} else if d.needField {
		return spec, i, errf("Specified format field didn't have an array index.")
	}

	if i >= len(format) {
		return spec, i, invalid
	}
	spec.verb = format[i]
	return spec, i, nil
}

// fmtNumber reads a literal width or precision, or a '*' standing for
// one taken off the stack. It reports -1 when the directive gave
// neither.
func fmtNumber(format string, i int, d fmtDialect, invalid error) (int, int, error) {
	if i < len(format) && format[i] == '*' {
		if d.star == nil {
			return -1, i, invalid
		}
		n, err := d.star()
		return n, i + 1, err
	}
	start := i
	for i < len(format) && format[i] >= '0' && format[i] <= '9' {
		i++
	}
	if i == start {
		return -1, i, nil
	}
	n, _ := strconv.Atoi(format[start:i])
	return n, i, nil
}

// render turns one directive and its argument into text.
func (spec directive) render(h Host, d fmtDialect) (string, error) {
	v, err := d.arg(spec.field, spec.verb)
	if err != nil {
		return "", err
	}

	verb := spec.verb
	if verb == '~' {
		// '~' takes whichever verb suits the value it was
		// handed.
		switch v.Type {
		case TypeObject:
			verb = 'd'
		case TypeFloat:
			verb = 'g'
		case TypeInteger:
			verb = 'i'
		case TypeLock:
			verb = 'l'
		case TypeString:
			verb = 's'
		default:
			verb = '?'
		}
	}

	switch verb {
	case 'i':
		if v.Type != TypeInteger {
			return "", errf("Format specified integer argument not found.")
		}
		return spec.pad(fmt.Sprintf(spec.numeric('d'), v.Num)), nil

	case 'f', 'e', 'g':
		if v.Type != TypeFloat {
			return "", errf("Format specified float not found.")
		}
		return spec.pad(fmt.Sprintf(spec.numeric(verb), v.Float)), nil

	case 's', 'S':
		if v.Type != TypeString {
			return "", errf("Format specified string argument not found.")
		}
		return spec.pad(spec.text(v.Str)), nil

	case 'd':
		if v.Type != TypeObject {
			return "", errf("Format specified object not found.")
		}
		return spec.pad(spec.text(v.Ref.String())), nil

	case 'D':
		if v.Type != TypeObject {
			return "", errf("Format specified object not found.")
		}
		if h == nil || !h.Valid(v.Ref) {
			return "", errf("Format specified object not valid.")
		}
		return spec.pad(spec.text(h.Name(v.Ref))), nil

	case 'l':
		if v.Type != TypeLock {
			return "", errf("Format specified lock not found.")
		}
		return spec.pad(spec.text(v.String())), nil

	case '?':
		return spec.pad(spec.text(fmtTypeName(v))), nil

	default:
		return "", errf("Invalid format string.")
	}
}

// numeric builds the printf spec for a number. Go's numeric verbs
// take the same flags as C's and render identically, so these are
// handed straight to fmt rather than padded by hand.
func (spec directive) numeric(verb byte) string {
	var b strings.Builder
	b.WriteByte('%')
	if spec.just == justifyLeft {
		b.WriteByte('-')
	}
	if spec.plus {
		b.WriteByte('+')
	} else if spec.space {
		b.WriteByte(' ')
	}
	if spec.zero {
		b.WriteByte('0')
	}
	if spec.width >= 0 {
		b.WriteString(strconv.Itoa(spec.width))
	}
	if spec.precision >= 0 {
		b.WriteByte('.')
		b.WriteString(strconv.Itoa(spec.precision))
	}
	b.WriteByte(verb)
	return b.String()
}

// text renders a string-shaped value into its field.
//
// This pads by hand rather than through fmt because C ignores the
// '0', '+' and ' ' flags on a string and Go honours them: "%08s" pads
// with spaces there and zeros here.
//
// The width counts visible characters, so any ANSI escape in the
// string is added back on top of it — otherwise a coloured name
// would be padded as though its escape bytes were letters, which is
// how upstream's own "repair the lengths to account for ansi codes"
// pass reads.
func (spec directive) text(s string) string {
	width, prec := spec.width, spec.precision
	if prec >= 0 {
		s = truncateVisible(s, prec)
	}
	if width < 0 {
		return s
	}
	if pad := width - visibleLen(s); pad > 0 {
		switch spec.just {
		case justifyLeft:
			return s + strings.Repeat(" ", pad)
		default:
			return strings.Repeat(" ", pad) + s
		}
	}
	return s
}

// pad applies the centring pass, which upstream runs over
// already-formatted output rather than as a justification of its own:
// it shifts the text left by half of whatever leading whitespace the
// field gave it.
func (spec directive) pad(s string) string {
	if spec.just != justifyCentre {
		return s
	}
	lead := len(s) - len(strings.TrimLeft(s, " "))
	if lead == 0 || lead == len(s) {
		return s
	}
	shift := lead / 2
	return s[shift:] + strings.Repeat(" ", shift)
}

// fmtTypeName is the '?' verb: what upstream calls each value type.
func fmtTypeName(v Value) string {
	switch v.Type {
	case TypeObject:
		return "OBJECT"
	case TypeFloat:
		return "FLOAT"
	case TypeInteger:
		return "INTEGER"
	case TypeLock:
		return "LOCK"
	case TypeString:
		return "STRING"
	case TypeArray:
		return "ARRAY"
	default:
		return "UNKNOWN"
	}
}

// visibleLen counts a string's characters, skipping ANSI escape
// sequences.
func visibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if w := ansiRun(s, i); w > 0 {
			i += w
			continue
		}
		i++
		n++
	}
	return n
}

// truncateVisible cuts a string to max visible characters, keeping
// whole escape sequences.
func truncateVisible(s string, max int) string {
	n := 0
	for i := 0; i < len(s); {
		if w := ansiRun(s, i); w > 0 {
			i += w
			continue
		}
		if n == max {
			return s[:i]
		}
		i++
		n++
	}
	return s
}

// ansiRun reports the length of the escape sequence starting at i, or
// 0 if there is none. It is upstream's own ANSI_STRLEN scan: ESC,
// then either a bare character or "[", digits and semicolons, and an
// "m".
func ansiRun(s string, i int) int {
	if s[i] != 0x1b {
		return 0
	}
	j := i + 1
	if j >= len(s) {
		return 1
	}
	if s[j] != '[' {
		return 2
	}
	j++
	for j < len(s) &&
		(s[j] >= '0' && s[j] <= '9' || s[j] == ';') {
		j++
	}
	if j < len(s) && s[j] == 'm' {
		j++
	}
	return j - i
}
