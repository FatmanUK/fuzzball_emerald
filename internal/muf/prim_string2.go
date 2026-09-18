package muf

import (
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// The remaining string primitives: ANSI handling, character conversion,
// tokenising, and regular expressions.

// ansiPattern matches an ANSI escape sequence, which the ANSI_* primitives
// treat as taking no width.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func init() {
	register("ANSI_STRIP", mapString(func(s string) string {
		return ansiPattern.ReplaceAllString(s, "")
	}))
	register("ANSI_STRLEN", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(len(ansiPattern.ReplaceAllString(s, "")))))
	})
	register("ANSI_MIDSTR", func(f *Frame) (*Result, error) {
		// Positions count visible characters, so the escapes between
		// them come along without being counted.
		v, err := f.PopN(3)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeInteger || v[2].Type != TypeInteger {
			return nil, errf("Invalid argument type.")
		}
		return nil, f.Push(Str(ansiSlice(v[0].Str, int(v[1].Num), int(v[2].Num))))
	})
	register("ANSI_STRCUT", func(f *Frame) (*Result, error) {
		at, err := f.popInt()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		i := ansiIndex(s, int(at))
		if err := f.Push(Str(s[:i])); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(s[i:]))
	})

	register("CTOI", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, f.Push(Int(0))
		}
		return nil, f.Push(Int(int64(s[0])))
	})
	register("ITOC", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		// Only printable characters convert; anything else yields the
		// empty string rather than a control code.
		if n < 32 || n > 126 {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(string(rune(n))))
	})

	register("MD5HASH", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		sum := md5.Sum([]byte(s))
		return nil, f.Push(Str(hex.EncodeToString(sum[:])))
	})

	register("INSTRING", caseIndex(false))
	register("RINSTRING", caseIndex(true))

	register("EXPLODE_ARRAY", func(f *Frame) (*Result, error) {
		sep, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		if sep == "" {
			return nil, errf("Empty string argument (2)")
		}
		parts := strings.Split(s, sep)
		vals := make([]Value, len(parts))
		for i, p := range parts {
			vals[i] = Str(p)
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("RSPLIT", func(f *Frame) (*Result, error) {
		sep, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		// Like SPLIT, but at the last occurrence.
		i := strings.LastIndex(s, sep)
		if i < 0 || sep == "" {
			if err := f.Push(Str(s)); err != nil {
				return nil, err
			}
			return nil, f.Push(Str(""))
		}
		if err := f.Push(Str(s[:i])); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(s[i+len(sep):]))
	})

	register("TOKENSPLIT", func(f *Frame) (*Result, error) {
		// "string delimiters escape tokensplit": split at the first
		// unescaped delimiter, reporting which one it was.
		escape, err := f.popStr()
		if err != nil {
			return nil, err
		}
		delims, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		before, after, found := tokenSplit(s, delims, escape)
		if err := f.Push(Str(before)); err != nil {
			return nil, err
		}
		if err := f.Push(Str(after)); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(found))
	})

	register("NOTIFY_NOLISTEN", func(f *Frame) (*Result, error) {
		// The listener propqueues arrive with the event machinery; with
		// none running, this is an ordinary notify.
		if _, err := f.Pop(); err != nil {
			return nil, err
		}
		msg, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		who, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(msg, "\r") {
			h.Notify(who, line)
		}
		return nil, nil
	})

	// Every connection is encrypted, so the insecure half of these can
	// never be reached; the secure message is always the one sent.
	register("NOTIFY_SECURE", func(f *Frame) (*Result, error) {
		secure, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		if _, err := f.popStrArg(2); err != nil {
			return nil, err
		}
		who, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(secure, "\r") {
			h.Notify(who, line)
		}
		return nil, nil
	})

	register("REGEXP", func(f *Frame) (*Result, error) {
		flags, err := f.popInt()
		if err != nil {
			return nil, err
		}
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		re, err := compileRegex(pattern, flags)
		if err != nil {
			return nil, err
		}
		m := re.FindStringSubmatchIndex(s)
		if m == nil {
			// No match: an empty array of matches and of positions.
			if err := f.Push(Arr(NewList(nil))); err != nil {
				return nil, err
			}
			return nil, f.Push(Arr(NewList(nil)))
		}
		groups := re.FindStringSubmatch(s)
		vals := make([]Value, len(groups))
		for i, g := range groups {
			vals[i] = Str(g)
		}
		// Positions come back as one-based start and length pairs.
		var spans []Value
		for i := 0; i*2 < len(m); i++ {
			start, end := m[i*2], m[i*2+1]
			if start < 0 {
				spans = append(spans, Arr(NewList([]Value{Int(0), Int(0)})))
				continue
			}
			spans = append(spans, Arr(NewList([]Value{
				Int(int64(start + 1)), Int(int64(end - start)),
			})))
		}
		if err := f.Push(Arr(NewList(vals))); err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(NewList(spans)))
	})

	register("REGSUB", func(f *Frame) (*Result, error) {
		flags, err := f.popInt()
		if err != nil {
			return nil, err
		}
		with, err := f.popStr()
		if err != nil {
			return nil, err
		}
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		re, err := compileRegex(pattern, flags)
		if err != nil {
			return nil, err
		}
		// MUF writes a capture as \1; Go writes it as ${1}.
		repl := captureRefs.ReplaceAllString(with, "${$1}")
		if flags&regexAll != 0 {
			return nil, f.Push(Str(re.ReplaceAllString(s, repl)))
		}
		done := false
		out := re.ReplaceAllStringFunc(s, func(m string) string {
			if done {
				return m
			}
			done = true
			idx := re.FindStringSubmatchIndex(s)
			return string(re.ExpandString(nil, repl, s, idx))
		})
		return nil, f.Push(Str(out))
	})

	register("REGSPLIT", regSplit(false))
	register("REGSPLIT_NOEMPTY", regSplit(true))
}

// Regex flags, which docs/man.txt documents as $defines a program writes.
const (
	regexICase    = 1
	regexAll      = 2
	regexExtended = 4
)

// captureRefs matches MUF's \1 style capture references.
var captureRefs = regexp.MustCompile(`\\(\d)`)

// compileRegex builds a pattern with the flags MUF passes.
//
// Go's regexp is RE2, which has no backreferences or lookaround. A pattern
// using them fails to compile here rather than behaving differently, which is
// the safer of the two.
func compileRegex(pattern string, flags int64) (*regexp.Regexp, error) {
	var prefix string
	if flags&regexICase != 0 {
		prefix += "i"
	}
	if flags&regexExtended != 0 {
		prefix += "x"
	}
	if prefix != "" {
		pattern = "(?" + prefix + ")" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, errf("Malformed regexp pattern: %s", err.Error())
	}
	return re, nil
}

// regSplit builds REGSPLIT and its no-empty variant.
func regSplit(dropEmpty bool) primFunc {
	return func(f *Frame) (*Result, error) {
		flags, err := f.popInt()
		if err != nil {
			return nil, err
		}
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		re, err := compileRegex(pattern, flags)
		if err != nil {
			return nil, err
		}
		var vals []Value
		parts := re.Split(s, -1)
		// Upstream's loop runs "while (*text)", so it stops at the end
		// of the string and never appends the empty field a trailing
		// delimiter would leave. Leading and interior empties are kept.
		if s == "" {
			parts = nil
		} else if n := len(parts); n > 0 && parts[n-1] == "" {
			parts = parts[:n-1]
		}
		for _, part := range parts {
			if dropEmpty && part == "" {
				continue
			}
			vals = append(vals, Str(part))
		}
		return nil, f.Push(Arr(NewList(vals)))
	}
}

// caseIndex builds INSTRING and RINSTRING, which are the case-insensitive
// forms of INSTR and RINSTR.
func caseIndex(fromEnd bool) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeString || v[1].Type != TypeString {
			return nil, errf("Non-string argument.")
		}
		hay, needle := ascii.Fold(v[0].Str), ascii.Fold(v[1].Str)
		var i int
		if fromEnd {
			i = strings.LastIndex(hay, needle)
		} else {
			i = strings.Index(hay, needle)
		}
		return nil, f.Push(Int(int64(i + 1)))
	}
}

// ansiIndex converts a visible-character position to a byte offset, skipping
// escape sequences.
func ansiIndex(s string, visible int) int {
	seen, i := 0, 0
	for i < len(s) {
		if loc := ansiPattern.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		if seen >= visible {
			return i
		}
		seen++
		i++
	}
	return len(s)
}

// ansiSlice takes a substring by visible position, one-based, keeping the
// escapes that fall inside it.
func ansiSlice(s string, start, length int) string {
	if start < 1 || length <= 0 {
		return ""
	}
	from := ansiIndex(s, start-1)
	to := ansiIndex(s, start-1+length)
	return s[from:to]
}

// tokenSplit splits at the first unescaped delimiter, reporting which one.
func tokenSplit(s, delims, escape string) (before, after, found string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape != "" && c == escape[0] && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		if strings.IndexByte(delims, c) >= 0 {
			return b.String(), s[i+1:], string(c)
		}
		b.WriteByte(c)
	}
	return b.String(), "", ""
}
