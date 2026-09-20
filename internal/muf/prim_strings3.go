package muf

import (
	"math/rand"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// STOD, OTELL, PRONOUN_SUB, STRENCRYPT, STRDECRYPT, TEXTATTR and
// POSE-SEPARATOR? are ports of the remaining src/p_strings.c primitives.
// ARRAY_FMTSTRINGS, the one primitive left unported in this file, is a
// dict-driven %(field)s reimplementation of FMTSTRING's own sprintf-alike
// parser — the C's own doc comment calls it "actually very complex" — and is
// deliberately deferred rather than rushed.
func init() {
	register("STOD", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(parseSTOD(s)))
	})

	register("POSE-SEPARATOR?", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		ok := s != "" && strings.ContainsRune("' ,-", rune(s[0]))
		return nil, f.Push(Bool(ok))
	})

	register("OTELL", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if msg != "" {
			h.NotifyExcept(h.Location(f.Caller), []ref.Ref{f.Caller}, msg)
		}
		return nil, nil
	})

	register("PRONOUN_SUB", func(f *Frame) (*Result, error) {
		msg, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, errf("Invalid argument (1)")
		}
		return nil, f.Push(Str(pronounSub(h, obj, msg)))
	})

	register("STRENCRYPT", strCrypt(strEncrypt))
	register("STRDECRYPT", strCrypt(strDecrypt))

	register("TEXTATTR", func(f *Frame) (*Result, error) {
		attrsV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		textV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if textV.Type != TypeString {
			return nil, errf("Non-string argument. (1)")
		}
		if attrsV.Type != TypeString {
			return nil, errf("Non-string argument. (2)")
		}
		out, err := textAttr(textV.Str, attrsV.Str)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(out))
	})
}

// parseSTOD is a port of prim_stod's own hand-rolled parse: an optional
// leading '#', an optional '+', then a run of digits (one leading '-'
// allowed) — if anything but whitespace follows those digits, the whole
// argument is ref.Nothing; otherwise it is whatever atoi would make of the
// digits alone.
func parseSTOD(s string) ref.Ref {
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimPrefix(s, "+")

	end := 0
	if end < len(s) && s[end] == '-' {
		end++
	}
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end < len(s) && !isSpaceByte(s[end]) {
		return ref.Nothing
	}
	// atoi's own behaviour: no digits at all, sign included, parses as 0
	// rather than failing.
	n, _ := strconv.Atoi(s[:end])
	return ref.Ref(n)
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\v' || b == '\f'
}

// pronounDefaults is upstream's fbstrings.c subjective/possessive/
// objective/reflexive/absolute tables, indexed [gender][form].
//
// This covers only the default substitution table, keyed off the gender
// property's raw value — not upstream's full pronoun_substitute, which also
// checks a per-object PRONOUNS_PROPDIR override, a global one on #0, and a
// "_default" propdir fallback chain, and lets a substitution itself expand
// to "%N" (the player's own name). Those layers are deliberately not
// reproduced: this is the common case a MUF program actually relies on.
var pronounDefaults = map[string][5]string{
	"s": {"", "it", "she", "he", "sie"},
	"p": {"", "its", "her", "his", "hir"},
	"o": {"", "it", "her", "him", "hir"},
	"r": {"", "itself", "herself", "himself", "hirself"},
	"a": {"", "its", "hers", "his", "hirs"},
}

// genderIndex maps a gender property's value to pronounDefaults' own index:
// unassigned/unrecognised, neuter, female, male, herm/hermaphrodite.
func genderIndex(sex string) int {
	switch strings.ToLower(strings.TrimSpace(sex)) {
	case "male":
		return 3
	case "female":
		return 2
	case "hermaphrodite", "herm":
		return 4
	case "neuter":
		return 1
	}
	return 0
}

// pronounSub is a port of pronoun_substitute — see pronounDefaults' own doc
// comment for what is deliberately not reproduced. %% is a literal %, and
// an unrecognised directive letter is passed through unchanged, both
// matching the C.
func pronounSub(h Host, who ref.Ref, s string) string {
	genderProp, _ := h.TuneGet("gender_prop")
	sex, _ := h.GetProp(who, genderProp)
	idx := genderIndex(sex.StringValue())

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		c := s[i]
		if c == '%' {
			b.WriteByte('%')
			continue
		}
		table, ok := pronounDefaults[strings.ToLower(string(c))]
		if !ok {
			b.WriteByte('%')
			b.WriteByte(c)
			continue
		}
		sub := table[idx]
		if c >= 'A' && c <= 'Z' && sub != "" {
			sub = strings.ToUpper(sub[:1]) + sub[1:]
		}
		b.WriteString(sub)
	}
	return b.String()
}

// enarr is upstream's own scramble table: ROT13 on letters, \r<->127 and
// ESCAPE_CHAR(27)<->31 swapped, identity otherwise.
var enarr = func() [256]byte {
	var a [256]byte
	for i := range a {
		a[i] = byte(i)
	}
	for i := byte('A'); i <= 'M'; i++ {
		a[i] += 13
	}
	for i := byte('N'); i <= 'Z'; i++ {
		a[i] -= 13
	}
	a['\r'], a[127] = 127, '\r'
	a[27], a[31] = 31, 27
	return a
}()

const cryptCharCount = 97
const cryptOffset = 32 - (cryptCharCount - 96)

// strEncrypt is a port of strencrypt. Its own entropy includes a byte of
// real randomness (RANDOM() >> 24), so — unlike the rest of this
// codebase — its output cannot be golden-compared against upstream's
// directly; strdecrypt(strencrypt(s, k), k) == s is what is verified
// instead, which is the only contract MUF code actually depends on.
func strEncrypt(data, key string) string {
	seed := 0
	for _, c := range []byte(key) {
		seed = ((int(c) ^ seed) + 170) % 192
	}
	seed2 := 0
	for _, c := range []byte(data) {
		seed2 = ((int(c) ^ seed2) + 21) & 0xff
	}
	seed3 := (seed2 ^ (seed ^ int(rand.Uint32()>>24))) & 0x3f
	seed2 = seed3

	count := seed + 11
	out := make([]byte, 0, len(data)+2)
	out = append(out, byte(' '+2), byte(' '+seed3))

	kb := []byte(key)
	ki := 0
	for _, c := range []byte(data) {
		count = ((int(kb[ki]) ^ count) + (seed ^ seed2)) & 0xff
		seed2 = (seed2 + 1) & 0x3f
		ki++
		if ki >= len(kb) {
			ki = 0
		}

		result := int(enarr[c]) - cryptOffset + count + seed
		result = ((result % cryptCharCount) + cryptCharCount) % cryptCharCount
		outByte := enarr[result+cryptOffset]
		out = append(out, outByte)
		count = ((int(c) ^ count) + seed) & 0xff
	}
	return string(out)
}

// strDecrypt is a port of strdecrypt.
func strDecrypt(data, key string) string {
	if len(data) < 2 {
		return ""
	}
	if data[0]-' ' < 1 || data[0]-' ' > 2 {
		return ""
	}
	seed2 := int(data[1] - ' ')

	seed := 0
	for _, c := range []byte(key) {
		seed = ((int(c) ^ seed) + 170) % 192
	}

	count := seed + 11
	kb := []byte(key)
	ki := 0
	out := make([]byte, 0, len(data)-2)
	for _, c := range []byte(data[2:]) {
		count = ((int(kb[ki]) ^ count) + (seed ^ seed2)) & 0xff
		ki++
		if ki >= len(kb) {
			ki = 0
		}
		seed2 = (seed2 + 1) & 0x3f

		result := int(enarr[c]) - cryptOffset - (count + seed)
		for result < 0 {
			result += cryptCharCount
		}
		outByte := enarr[result+cryptOffset]
		out = append(out, outByte)
		count = ((int(outByte) ^ count) + seed) & 0xff
	}
	return string(out)
}

// strCrypt builds STRENCRYPT and STRDECRYPT, which share the same argument
// validation.
func strCrypt(fn func(data, key string) string) primFunc {
	return func(f *Frame) (*Result, error) {
		keyV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		dataV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if dataV.Type != TypeString || keyV.Type != TypeString {
			return nil, errf("Non-string argument.")
		}
		if keyV.Str == "" {
			return nil, errf("Key cannot be a null string. (2)")
		}
		return nil, f.Push(Str(fn(dataV.Str, keyV.Str)))
	}
}

// ansiColorCodes is TEXTATTR's own attribute-tag table, from
// fuzzball/include/color.h.
var ansiColorCodes = map[string]string{
	"reset": "\x1b[0m", "normal": "\x1b[0m",
	"bold": "\x1b[1m", "dim": "\x1b[2m", "italic": "\x1b[3m",
	"uline": "\x1b[4m", "underline": "\x1b[4m",
	"flash": "\x1b[5m", "reverse": "\x1b[7m",
	"ostrike": "\x1b[9m", "overstrike": "\x1b[9m",
	"black": "\x1b[30m", "red": "\x1b[31m", "green": "\x1b[32m",
	"yellow": "\x1b[33m", "blue": "\x1b[34m", "magenta": "\x1b[35m",
	"cyan": "\x1b[36m", "white": "\x1b[37m",
	"bg_black": "\x1b[40m", "bg_red": "\x1b[41m", "bg_green": "\x1b[42m",
	"bg_yellow": "\x1b[43m", "bg_blue": "\x1b[44m", "bg_magenta": "\x1b[45m",
	"bg_cyan": "\x1b[46m", "bg_white": "\x1b[47m",
}

// textAttr is a port of prim_textattr: attrs is a comma-separated (spaces
// ignored) list of tags from ansiColorCodes, each turned into its escape
// sequence and prepended to text, which is always followed by a reset.
func textAttr(text, attrs string) (string, error) {
	var b strings.Builder
	for _, tag := range strings.Split(attrs, ",") {
		tag = strings.ReplaceAll(tag, " ", "")
		if tag == "" {
			continue
		}
		code, ok := ansiColorCodes[strings.ToLower(tag)]
		if !ok {
			return "", errf("Unrecognized attribute tag.  Try one of reset, " +
				"bold, dim, italic, underline, flash, reverse, " +
				"overstrike, black, red, green, yellow, blue, " +
				"magenta, cyan, white, bg_black, bg_red, " +
				"bg_green, bg_yellow, bg_blue, bg_magenta, " +
				"bg_cyan, or bg_white.")
		}
		b.WriteString(code)
	}
	b.WriteString(text)
	b.WriteString(ansiColorCodes["reset"])
	return b.String(), nil
}
