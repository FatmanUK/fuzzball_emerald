package ascii

// SMatch implements MUCK pattern matching, which is not a regular
// expression. It is `equalstr` (`fbstrings.c:1757`), a one-line
// wrapper over `smatch` (`:1583`) that inverts its sense — smatch
// returns **0** on a match — so this returns true on a match and
// the inversion lives here rather than at every call site.
//
// It is read in fourteen places: `@tune`'s listing pattern, a
// listener's conditional value, `examine`'s property pattern,
// `@find`'s name, `@bless`'s path, the `file_*` family,
// `reserved_names`, MUF `SMATCH`, `FINDNEXT` and `ARRAY_FILTER_*`,
// MPI's `{smatch}` and `{listprops}`, and a `@lock` string
// comparison. It had no test at all and was not upstream's function:
// `{a|b}` was a plain alternation where upstream's is a **word**
// pattern, `{^a|b}` was not inverted, and `[a-z]` character classes
// were missing outright.
//
// The port is deliberately a transcription rather than a
// rationalisation. Four of its behaviours fall out of where the C
// leaves its cursor rather than from intent, and
// `internal/golden/smatch_test.go` pins all of them against the
// compiled original:
//
//   - A `{...}` set matches only at the start of the subject or
//     just after a space, and consumes a **whole word** — so
//     `{foo}p` does not match "foop" and `*{gold}*` does not match
//     "a golden ring".
//   - A `*` is implemented as "find the next occurrence of the
//     character after the star", so the character after a star is
//     what drives the search.
//   - A trailing `-` inside a class is a literal, because the
//     range's end character is read past the class body and comes
//     back as a NUL that compares below every printable
//     character.
//   - An **escaped** dash inside a class is not escaped at all:
//     `cmatch` steps over the backslash and then reads the range's
//     start from the byte *before* the dash, which is the
//     backslash. Reproduced, because a world's pattern sees it.
func SMatch(s, pattern string) bool {
	return smatch(pattern, s)
}

// smatch is the pattern walk. p is upstream's s1 and s its s2; the C
// mutates the pattern in place to terminate a sub-pattern, which is a
// slice here.
func smatch(p, s string) bool {
	// start is upstream's `start`: where this invocation's
	// subject began. The `*`-then-`{` branch compares the cursor
	// against it to decide whether the set is at the beginning.
	si := 0
	for pi := 0; pi < len(p); {
		switch p[pi] {
		case '\\':
			// A trailing backslash matches nothing.
			if pi+1 >= len(p) {
				return false
			}
			pi++
			if si >= len(s) ||
				lower(p[pi]) != lower(s[si]) {

				return false
			}
			pi++
			si++
		case '?':
			if si >= len(s) {
				return false
			}
			si++
			pi++
		case '*':
			// A star resolves the whole rest of the
			// pattern itself, so there is nothing after
			// it to come back to.
			return smatchStar(p, s, pi, si)
		case '[':
			end := estrchr(p[pi:], ']', '\\')
			if end < 0 {
				return false
			}
			// cmatch of a NUL is a refusal, which is what
			// a class at the end of the subject meets.
			if si >= len(s) ||
				!cmatch(p[pi+1:pi+end], s[si]) {

				return false
			}
			si++
			pi += end + 1
		case '{':
			var ok bool
			if pi, si, ok = smatchSet(p, s, pi, si); !ok {
				return false
			}
		default:
			if si >= len(s) ||
				lower(p[pi]) != lower(s[si]) {

				return false
			}
			pi++
			si++
		}
	}
	// Upstream's `tolower(*s1) - tolower(*s2)`: the pattern is
	// spent, so this matches only if the subject is too.
	return si == len(s)
}

// smatchStar is the `*` case, which resolves the rest of the pattern
// itself and therefore never returns to the caller's loop. It reports
// whether the whole remainder matched.
func smatchStar(p, s string, pi, si int) bool {
	// A run of stars collapses, and a '?' among them eats one
	// character of the subject.
	for pi < len(p) &&
		(p[pi] == '*' || (p[pi] == '?' && si < len(s))) {

		if p[pi] == '?' {
			si++
		}
		pi++
	}
	// A '?' left over is a '?' the subject ran out for.
	if pi < len(p) && p[pi] == '?' {
		return false
	}
	// A star at the end of the pattern absorbs the rest.
	if pi >= len(p) {
		return true
	}

	switch p[pi] {
	case '{':
		// The set is tried at the start of the subject only
		// when nothing has been consumed, and then at each
		// position after a space.
		if si == 0 && smatch(p[pi:], s[si:]) {
			return true
		}
		for k := si; k < len(s); k++ {
			if s[k] != ' ' {
				continue
			}
			if smatch(p[pi:], s[k+1:]) {
				return true
			}
		}
		return false
	case '[':
		for k := si; k < len(s); k++ {
			if smatch(p[pi:], s[k:]) {
				return true
			}
		}
		return false
	}

	// Otherwise the character *after* the star is what drives the
	// search, taken through one level of escaping.
	ch := p[pi]
	if ch == '\\' && pi+1 < len(p) {
		ch = p[pi+1]
	}
	for k := si; k < len(s); k++ {
		if lower(s[k]) != lower(ch) {
			continue
		}
		if smatch(p[pi:], s[k:]) {
			return true
		}
	}
	return false
}

// smatchSet is the `{...}` case: a word pattern.
func smatchSet(p, s string, pi, si int) (int, int, bool) {
	// Only at the start of the subject, or just after a space.
	if si != 0 && s[si-1] != ' ' {
		return pi, si, false
	}
	neg := pi+1 < len(p) && p[pi+1] == '^'
	end := estrchr(p[pi:], '}', '\\')
	if end < 0 {
		return pi, si, false
	}
	off := 1
	if neg {
		off = 2
	}
	var matched bool
	matched, si = wmatch(p[pi+off:pi+end], s, si)
	// Upstream's arithmetic: the set fails when a match is what
	// it was told to refuse, and when a refusal is what it was
	// told to match.
	if matched == neg {
		return pi, si, false
	}
	return pi + end + 1, si, true
}

// wmatch is `wmatch` (`fbstrings.c:1487`): take the next whole word
// of the subject and try it against each `|`-separated alternative.
//
// The cursor advances past the word **whether or not it matched**,
// which is what makes a `{...}` set consume a word rather than a
// prefix.
func wmatch(wlist, s string, si int) (bool, int) {
	if si >= len(s) {
		return false, si
	}
	word := s[si:]
	next := si + len(word)
	for k := si; k < len(s); k++ {
		if s[k] == ' ' {
			word = s[si:k]
			next = k
			break
		}
	}
	for {
		bar := estrchr(wlist, '|', '\\')
		alt := wlist
		if bar >= 0 {
			alt = wlist[:bar]
		}
		if smatch(alt, word) {
			return true, next
		}
		if bar < 0 {
			return false, next
		}
		wlist = wlist[bar+1:]
	}
}

// cmatch is `cmatch` (`fbstrings.c:1432`): one character against a
// class body, with `^` inverting and `a-z` a range. It reports
// whether the character is accepted.
func cmatch(body string, c byte) bool {
	c = lower(c)
	// truthval is upstream's: what a hit returns. Inverted, a hit
	// is a refusal and running out is an acceptance.
	hit := true
	if len(body) > 0 && body[0] == '^' {
		body = body[1:]
		hit = false
	}
	// A leading '-' is a literal, because there is nothing before
	// it to start a range.
	i := 0
	if i < len(body) && body[i] == '-' {
		if lower(body[i]) == c {
			return hit
		}
		i++
	}
	at := func(k int) byte {
		if k < 0 || k >= len(body) {
			return 0
		}
		return body[k]
	}
	for i < len(body) {
		if body[i] == '\\' && i+1 < len(body) {
			i++
		}
		if body[i] != '-' {
			if lower(body[i]) == c {
				return hit
			}
			i++
			continue
		}
		// A range, whose ends are read *around* the dash —
		// so a trailing dash has a NUL for its end and comes
		// out a literal, and an escaped dash takes the
		// backslash as its start.
		from, to := at(i-1), at(i+1)
		if from > to {
			if lower(body[i]) == c {
				return hit
			}
			i++
			continue
		}
		for b := from; b <= to; b++ {
			if lower(b) == c {
				return hit
			}
		}
		i += 2
	}
	return !hit
}

// estrchr is `estrchr` (`fbstrings.c`): the offset of the first
// unescaped c in s, or -1.
func estrchr(s string, c, e byte) int {
	for i := 0; i < len(s); {
		if s[i] == c {
			return i
		}
		if s[i] == e {
			i++
		}
		if i < len(s) {
			i++
		}
	}
	return -1
}
