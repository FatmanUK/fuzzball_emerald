package mpi

import (
	"strconv"
	"strings"
)

// An MPI "list" is a property list: a count under "name#" (or
// "name/#") and the items under "name#/1", "name#/2" and so on. It is
// what {list}, {lexec} and the {l...} set all read, and what a MUF
// program's own array-to-property helpers write.
//
// A list with no count property is measured by walking its items
// until one comes back empty, which is how a list written by hand
// without a count still works.

// maxListLen is upstream's MAX_MFUN_LIST_LEN.
const maxListLen = 512

// numberToken is upstream's NUMBER_TOKEN, the '#' a list name ends
// with.
const numberToken = '#'

// getProp reads a property, walking outwards through the environment
// until something answers — upstream's safegetprop, as against the
// strict form that looks only at the object named.
//
// An unset property and an empty one are not distinguished, which is
// upstream's own: the walk continues past either.
func (env *Env) getProp(obj Ref, path string) string {
	for i := 0; i < maxEnvDepth && obj != nothing; i++ {
		if v := env.Host.GetPropStr(obj, path); v != "" {
			return v
		}
		obj = env.Host.Parent(obj)
	}
	return ""
}

// maxEnvDepth bounds the environment walk, as internal/world's own
// does: a damaged world can hold a cycle that getparent's detection
// misses.
const maxEnvDepth = 128

// nothing is #-1.
const nothing Ref = -1

// trimListName drops the '#' a caller may have written on the end of
// a list name, so "{list:foo#}" and "{list:foo}" name the same list.
func trimListName(name string) string {
	if n := len(name); n > 0 && name[n-1] == numberToken {
		return name[:n-1]
	}
	return name
}

// listItem reads one item, counting from one. An index outside the
// list reads as empty rather than failing.
func (env *Env) listItem(obj Ref, name string, n int) string {
	return env.getProp(obj, trimListName(name)+"#/"+strconv.Itoa(n))
}

// listCount is how many items a list holds.
//
// The count property is looked for under two spellings before the
// list is measured by walking it, which is upstream's order: "name#"
// first, then "name/#".
func (env *Env) listCount(obj Ref, name string) int {
	name = trimListName(name)
	for _, path := range [2]string{name + "#", name + "/#"} {
		if v := env.getProp(obj, path); v != "" {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	for i := 1; i < maxListLen; i++ {
		if env.listItem(obj, name, i) == "" {
			return i - 1
		}
	}
	return maxListLen
}

// listItems reads a whole list.
func (env *Env) listItems(obj Ref, name string) []string {
	cnt := env.listCount(obj, name)
	if cnt > maxListLen {
		cnt = maxListLen
	}
	out := make([]string, 0, max(cnt, 0))
	for i := 1; i <= cnt; i++ {
		out = append(out, env.listItem(obj, name, i))
	}
	return out
}

// Concatenation modes, upstream's get_concat_list: 0 joins with
// carriage returns, 1 with spaces (two after a sentence ending), 2
// with nothing.
const (
	concatLines = iota
	concatSentences
	concatTight
)

// concatList joins a list the way one of the three modes says.
func concatList(items []string, mode int) string {
	var b strings.Builder
	for _, item := range items {
		if mode != concatLines {
			item = strings.TrimSpace(item)
		}
		if b.Len() > 0 {
			switch mode {
			case concatLines:
				b.WriteByte('\r')
			case concatSentences:
				// A second space after something that
				// ends a sentence.
				if last := b.String()[b.Len()-1]; last == '.' ||
					last == '?' || last == '!' {
					b.WriteByte(' ')
				}
				b.WriteByte(' ')
			}
		}
		b.WriteString(item)
	}
	return b.String()
}

// splitLines is how a list-shaped argument that is plain text rather
// than a property name is read: the {l...} set take one, and upstream
// splits them on carriage returns.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\r")
}
