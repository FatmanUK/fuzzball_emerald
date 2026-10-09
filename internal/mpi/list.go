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
func (env *Env) listItem(fn string, obj Ref, name string,
	n int) (string, error) {

	return env.getProp(fn, obj,
		trimListName(name)+"#/"+strconv.Itoa(n))
}

// listCount is how many items a list holds.
//
// The count property is looked for under two spellings before the
// list is measured by walking it, which is upstream's order: "name#"
// first, then "name/#".
func (env *Env) listCount(fn string, obj Ref,
	name string) (int, error) {

	name = trimListName(name)
	for _, path := range [2]string{name + "#", name + "/#"} {
		v, err := env.getProp(fn, obj, path)
		if err != nil {
			return 0, err
		}
		if v != "" {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n, nil
		}
	}
	for i := 1; i < maxListLen; i++ {
		v, err := env.listItem(fn, obj, name, i)
		if err != nil {
			return 0, err
		}
		if v == "" {
			return i - 1, nil
		}
	}
	return maxListLen, nil
}

// listItems reads a whole list.
func (env *Env) listItems(fn string, obj Ref,
	name string) ([]string, error) {

	cnt, err := env.listCount(fn, obj, name)
	if err != nil {
		return nil, err
	}
	if cnt > maxListLen {
		cnt = maxListLen
	}
	out := make([]string, 0, max(cnt, 0))
	for i := 1; i <= cnt; i++ {
		v, err := env.listItem(fn, obj, name, i)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
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
