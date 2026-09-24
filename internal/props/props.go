// Package props implements the property tree hanging off every
// database object.
//
// Fuzzball stores properties in a per-directory AVL tree keyed with
// strcasecmp, so lookup and ordering are case-insensitive while the
// name keeps whatever case it was first created with. This package
// keeps those semantics with a sorted child slice per node, which is
// observably identical and much easier to persist.
package props

import (
	"sort"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Type is a property's value type. The values are stored in legacy
// database dumps and must not be renumbered.
type Type uint8

const (
	Dir    Type = 0x0 // a directory with no value of its own
	String Type = 0x2
	Int    Type = 0x3
	Lock   Type = 0x4
	Ref    Type = 0x5
	Float  Type = 0x6

	TypeMask Type = 0x7
)

func (t Type) String() string {
	switch t {
	case Dir:
		return "dir"
	case String:
		return "string"
	case Int:
		return "integer"
	case Lock:
		return "lock"
	case Ref:
		return "dbref"
	case Float:
		return "float"
	default:
		return "unknown"
	}
}

// FlagBlessed marks a property whose MPI evaluates with wizard
// permissions. It is the one persisted flag; everything else in the
// upstream flag word describes diskbase state that Emerald does not
// have.
const FlagBlessed = 0x1000

// Value is a property's contents. Only the field matching Type is
// meaningful.
//
// A lock is held as its unparsed boolean expression, which is also
// how dumps store it; boolexp parsing happens at the point of use.
type Value struct {
	Type    Type
	Str     string
	Num     int64
	Float   float64
	Ref     ref.Ref
	Blessed bool
}

// IsEmpty reports whether setting this value should delete the
// property instead of storing it. Fuzzball treats an empty string, a
// zero number and a NOTHING dbref as a request to unset.
func (v Value) IsEmpty() bool {
	switch v.Type {
	case String, Lock:
		return v.Str == ""
	case Int:
		return v.Num == 0
	case Float:
		return v.Float == 0
	case Ref:
		return v.Ref == ref.Nothing
	default:
		return true
	}
}

// StringValue renders a value the way a dump stores it and MUF prints
// it.
func (v Value) StringValue() string {
	switch v.Type {
	case String, Lock:
		return v.Str
	case Int:
		return itoa(v.Num)
	case Float:
		return ftoa(v.Float)
	case Ref:
		return v.Ref.String()
	default:
		return ""
	}
}

// node is one level of the property tree.
type node struct {
	name string // as first created, case preserved
	fold string // ASCII-folded, the lookup and sort key
	val  Value
	has  bool // does this node carry a value, or is it only a directory?
	kids []*node
}

// Tree is an object's property tree.
type Tree struct {
	root node
	n    int // number of value-bearing properties
}

// New returns an empty tree.
func New() *Tree { return &Tree{} }

// split normalises a property path into its segments. Leading and
// repeated slashes are dropped, and the path is truncated at the
// first ':', which is the delimiter dumps use between a name and its
// flags.
func split(path string) []string {
	if i := strings.IndexByte(path, ':'); i >= 0 {
		path = path[:i]
	}
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// find locates a child by folded name, returning its index. The bool
// reports an exact match; otherwise the index is where it would be
// inserted.
func (n *node) find(f string) (int, bool) {
	i := sort.Search(len(n.kids), func(i int) bool { return n.kids[i].fold >= f })
	return i, i < len(n.kids) && n.kids[i].fold == f
}

// child returns the named child, or nil.
func (n *node) child(f string) *node {
	if i, ok := n.find(f); ok {
		return n.kids[i]
	}
	return nil
}

// makeChild returns the named child, creating it if needed.
func (n *node) makeChild(name string) *node {
	f := ascii.Fold(name)
	i, ok := n.find(f)
	if ok {
		return n.kids[i]
	}
	c := &node{name: name, fold: f}
	n.kids = append(n.kids, nil)
	copy(n.kids[i+1:], n.kids[i:])
	n.kids[i] = c
	return c
}

// lookup walks to a path without creating anything.
func (t *Tree) lookup(path string) *node {
	n := &t.root
	for _, seg := range split(path) {
		if n = n.child(ascii.Fold(seg)); n == nil {
			return nil
		}
	}
	return n
}

// Get returns the value at path. The bool reports whether a value is
// present; a path that exists only as a directory reports false.
func (t *Tree) Get(path string) (Value, bool) {
	n := t.lookup(path)
	if n == nil || !n.has {
		return Value{}, false
	}
	return n.val, true
}

// Exists reports whether anything lives at path, value or directory.
func (t *Tree) Exists(path string) bool {
	return t.lookup(path) != nil
}

// IsDir reports whether path has children.
func (t *Tree) IsDir(path string) bool {
	n := t.lookup(path)
	return n != nil && len(n.kids) > 0
}

// Set stores a value. Following Fuzzball, storing an empty value
// unsets the property instead: the node becomes a plain directory,
// and is removed entirely if it has no children.
func (t *Tree) Set(path string, v Value) {
	segs := split(path)
	if len(segs) == 0 {
		return
	}
	if v.IsEmpty() {
		t.Delete(path)
		return
	}
	n := &t.root
	for _, seg := range segs {
		n = n.makeChild(seg)
	}
	if !n.has {
		t.n++
	}
	n.val, n.has = v, true
}

// SetString is shorthand for storing a string property.
func (t *Tree) SetString(path, s string) {
	t.Set(path, Value{Type: String, Str: s})
}

// Delete removes the value at path. A node with children survives as
// a directory; one without is pruned, along with any parents left
// empty. It reports whether anything was removed.
func (t *Tree) Delete(path string) bool {
	segs := split(path)
	if len(segs) == 0 {
		return false
	}

	// Walk down, remembering the path so empty parents can be
	// pruned.
	chain := make([]*node, 0, len(segs)+1)
	n := &t.root
	chain = append(chain, n)
	for _, seg := range segs {
		n = n.child(ascii.Fold(seg))
		if n == nil {
			return false
		}
		chain = append(chain, n)
	}

	removed := n.has
	if removed {
		t.n--
	}
	n.val, n.has = Value{}, false

	// Prune upwards while nodes carry neither a value nor
	// children.
	for i := len(chain) - 1; i > 0; i-- {
		c := chain[i]
		if c.has || len(c.kids) > 0 {
			break
		}
		parent := chain[i-1]
		if j, ok := parent.find(c.fold); ok {
			parent.kids = append(parent.kids[:j], parent.kids[j+1:]...)
		}
	}
	return removed
}

// DeleteDir removes path and everything beneath it, reporting how
// many value-bearing properties went with it.
func (t *Tree) DeleteDir(path string) int {
	segs := split(path)
	if len(segs) == 0 {
		return 0
	}
	parent := &t.root
	for _, seg := range segs[:len(segs)-1] {
		if parent = parent.child(ascii.Fold(seg)); parent == nil {
			return 0
		}
	}
	f := ascii.Fold(segs[len(segs)-1])
	i, ok := parent.find(f)
	if !ok {
		return 0
	}
	n := countValues(parent.kids[i])
	parent.kids = append(parent.kids[:i], parent.kids[i+1:]...)
	t.n -= n
	return n
}

func countValues(n *node) int {
	c := 0
	if n.has {
		c = 1
	}
	for _, k := range n.kids {
		c += countValues(k)
	}
	return c
}

// Children lists the names directly under path, in the order MUF's
// nextprop walks them. Names keep the case they were created with.
func (t *Tree) Children(path string) []string {
	n := t.lookup(path)
	if n == nil {
		return nil
	}
	out := make([]string, len(n.kids))
	for i, k := range n.kids {
		out[i] = k.name
	}
	return out
}

// Len returns the number of properties that carry a value.
// Directories that exist only to hold children are not counted.
func (t *Tree) Len() int { return t.n }

// Entry is one property, as produced by Walk.
type Entry struct {
	Path  string
	Value Value
}

// Walk visits every value-bearing property in depth-first order,
// parents before children and siblings in nextprop order.
func (t *Tree) Walk(fn func(Entry) bool) {
	walk(&t.root, "", fn)
}

func walk(n *node, prefix string, fn func(Entry) bool) bool {
	for _, k := range n.kids {
		path := k.name
		if prefix != "" {
			path = prefix + "/" + k.name
		}
		if k.has && !fn(Entry{Path: path, Value: k.val}) {
			return false
		}
		if !walk(k, path, fn) {
			return false
		}
	}
	return true
}

// All returns every value-bearing property in Walk order.
func (t *Tree) All() []Entry {
	out := make([]Entry, 0, t.n)
	t.Walk(func(e Entry) bool {
		out = append(out, e)
		return true
	})
	return out
}

// Clone returns a deep copy, so a snapshot can be handed to the
// persister while the world keeps mutating the original.
func (t *Tree) Clone() *Tree {
	return &Tree{root: *cloneNode(&t.root), n: t.n}
}

func cloneNode(n *node) *node {
	c := &node{name: n.name, fold: n.fold, val: n.val, has: n.has}
	if len(n.kids) > 0 {
		c.kids = make([]*node, len(n.kids))
		for i, k := range n.kids {
			c.kids[i] = cloneNode(k)
		}
	}
	return c
}
