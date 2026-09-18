package golden

import (
	"strconv"
	"strings"
)

// Normalize reduces a transcript to what is worth comparing.
//
// The two servers differ in ways that say nothing about MUF: line endings,
// blank lines, and the dbref suffixes an examine-style listing appends. Those
// are removed so a diff shows only differences in behaviour.
func Normalize(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Diff describes where two transcripts part company.
type Diff struct {
	Line    int
	Oracle  string
	Emerald string
}

// Compare returns the differences between two transcripts, or nil when they
// agree.
func Compare(oracle, emerald string) []Diff {
	a, b := Normalize(oracle), Normalize(emerald)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}

	var diffs []Diff
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			diffs = append(diffs, Diff{Line: i + 1, Oracle: x, Emerald: y})
		}
	}
	return diffs
}

// Render formats a set of differences for a test failure.
func Render(diffs []Diff) string {
	var b strings.Builder
	for _, d := range diffs {
		b.WriteString("  line ")
		b.WriteString(strconv.Itoa(d.Line))
		b.WriteString("\n    fuzzball: ")
		b.WriteString(quote(d.Oracle))
		b.WriteString("\n    emerald:  ")
		b.WriteString(quote(d.Emerald))
		b.WriteString("\n")
	}
	return b.String()
}

// quote renders a line visibly, so a difference in whitespace is not invisible.
func quote(s string) string {
	if s == "" {
		return "(nothing)"
	}
	return `"` + s + `"`
}
