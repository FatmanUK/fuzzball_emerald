package props

import (
	"reflect"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestGetSet(t *testing.T) {
	tr := New()
	tr.SetString("_/de", "You see nothing special.")
	v, ok := tr.Get("_/de")
	if !ok || v.Str != "You see nothing special." {
		t.Fatalf("Get(_/de) = %+v, %v", v, ok)
	}
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1", tr.Len())
	}
	if _, ok := tr.Get("_/nope"); ok {
		t.Error("Get on a missing property should report false")
	}
}

func TestLookupIsCaseInsensitiveButPreservesCase(t *testing.T) {
	tr := New()
	tr.SetString("_/DE", "described")

	for _, p := range []string{"_/de", "_/De", "_/DE", "_/dE"} {
		if v, ok := tr.Get(p); !ok || v.Str != "described" {
			t.Errorf("Get(%q) = %+v, %v; lookup should be case-insensitive", p, v, ok)
		}
	}
	// Writing through a different case updates the same property
	// and leaves the original spelling alone, as the AVL tree
	// does upstream.
	tr.SetString("_/de", "updated")
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1: differing case must not create a second property", tr.Len())
	}
	if got := tr.Children("_"); !reflect.DeepEqual(got, []string{"DE"}) {
		t.Errorf("Children(_) = %v, want [DE]: the original case should survive", got)
	}
}

func TestFoldIsASCIIOnly(t *testing.T) {
	// strcasecmp in the C locale does not fold non-ASCII, so
	// these are distinct properties even though Unicode folding
	// would merge them.
	tr := New()
	tr.SetString("Ä", "a")
	tr.SetString("ä", "b")
	if tr.Len() != 2 {
		t.Errorf("Len() = %d, want 2: non-ASCII must not be case-folded", tr.Len())
	}
}

func TestPathNormalisation(t *testing.T) {
	tr := New()
	tr.SetString("/a/b", "v")
	for _, p := range []string{"a/b", "/a/b", "//a//b", "a/b/", "/a/b:2:junk"} {
		if v, ok := tr.Get(p); !ok || v.Str != "v" {
			t.Errorf("Get(%q) = %+v, %v; should normalise to a/b", p, v, ok)
		}
	}
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1", tr.Len())
	}
}

func TestEmptyValueDeletes(t *testing.T) {
	// Fuzzball treats setting an empty value as an unset, which
	// is surprising but load-bearing for existing MUF.
	cases := []struct {
		name string
		val  Value
	}{
		{"empty string", Value{Type: String, Str: ""}},
		{"zero int", Value{Type: Int, Num: 0}},
		{"zero float", Value{Type: Float, Float: 0}},
		{"nothing ref", Value{Type: Ref, Ref: ref.Nothing}},
	}
	for _, c := range cases {
		tr := New()
		tr.SetString("p", "set")
		tr.Set("p", c.val)
		if _, ok := tr.Get("p"); ok {
			t.Errorf("%s: property should have been deleted", c.name)
		}
		if tr.Len() != 0 {
			t.Errorf("%s: Len() = %d, want 0", c.name, tr.Len())
		}
	}
}

func TestDeleteKeepsDirectoryWithChildren(t *testing.T) {
	tr := New()
	tr.SetString("a", "value")
	tr.SetString("a/b", "child")

	if !tr.Delete("a") {
		t.Fatal("Delete(a) should report a removal")
	}
	if _, ok := tr.Get("a"); ok {
		t.Error("a should no longer carry a value")
	}
	if v, ok := tr.Get("a/b"); !ok || v.Str != "child" {
		t.Error("a/b should have survived: a is still a directory")
	}
	if !tr.IsDir("a") {
		t.Error("a should still be a directory")
	}
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1", tr.Len())
	}
}

func TestDeletePrunesEmptyParents(t *testing.T) {
	tr := New()
	tr.SetString("x/y/z", "v")
	tr.Delete("x/y/z")
	if tr.Exists("x") || tr.Exists("x/y") {
		t.Error("directories left empty by a delete should be pruned")
	}
	if tr.Len() != 0 {
		t.Errorf("Len() = %d, want 0", tr.Len())
	}
}

func TestDeleteMissing(t *testing.T) {
	tr := New()
	if tr.Delete("nothing/here") {
		t.Error("deleting a missing property should report false")
	}
}

func TestDeleteDir(t *testing.T) {
	tr := New()
	tr.SetString("d", "top")
	tr.SetString("d/a", "1")
	tr.SetString("d/b/c", "2")
	tr.SetString("keep", "me")

	if n := tr.DeleteDir("d"); n != 3 {
		t.Errorf("DeleteDir(d) = %d, want 3", n)
	}
	if tr.Exists("d") {
		t.Error("d should be gone entirely")
	}
	if _, ok := tr.Get("keep"); !ok {
		t.Error("DeleteDir removed an unrelated property")
	}
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1", tr.Len())
	}
}

func TestChildrenAreSortedCaseInsensitively(t *testing.T) {
	tr := New()
	for _, n := range []string{"zebra", "Apple", "mango", "Banana"} {
		tr.SetString("d/"+n, "v")
	}
	got := tr.Children("d")
	want := []string{"Apple", "Banana", "mango", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Children(d) = %v, want %v", got, want)
	}
	if tr.Children("missing") != nil {
		t.Error("Children of a missing path should be nil")
	}
}

func TestWalkOrder(t *testing.T) {
	tr := New()
	tr.SetString("b", "2")
	tr.SetString("a", "1")
	tr.SetString("a/y", "ay")
	tr.SetString("a/x", "ax")

	var got []string
	tr.Walk(func(e Entry) bool {
		got = append(got, e.Path)
		return true
	})
	// Parents come before their children; siblings in nextprop
	// order.
	want := []string{"a", "a/x", "a/y", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Walk order = %v, want %v", got, want)
	}
}

func TestWalkStopsEarly(t *testing.T) {
	tr := New()
	for _, n := range []string{"a", "b", "c"} {
		tr.SetString(n, "v")
	}
	count := 0
	tr.Walk(func(Entry) bool {
		count++
		return count < 2
	})
	if count != 2 {
		t.Errorf("Walk visited %d entries, want 2 before stopping", count)
	}
}

func TestDirectoryWithoutValueIsNotCounted(t *testing.T) {
	tr := New()
	tr.SetString("a/b", "v")
	if tr.Len() != 1 {
		t.Errorf("Len() = %d, want 1: the implicit directory a is not a property", tr.Len())
	}
	if _, ok := tr.Get("a"); ok {
		t.Error("a exists only as a directory and should carry no value")
	}
	if !tr.Exists("a") {
		t.Error("a should exist as a directory")
	}
	if all := tr.All(); len(all) != 1 || all[0].Path != "a/b" {
		t.Errorf("All() = %v, want just a/b", all)
	}
}

func TestClonesAreIndependent(t *testing.T) {
	tr := New()
	tr.SetString("a/b", "original")
	c := tr.Clone()

	tr.SetString("a/b", "changed")
	tr.SetString("a/new", "added")

	if v, _ := c.Get("a/b"); v.Str != "original" {
		t.Errorf("clone saw a mutation: a/b = %q", v.Str)
	}
	if c.Exists("a/new") {
		t.Error("clone saw a property added after the copy")
	}
	if c.Len() != 1 {
		t.Errorf("clone Len() = %d, want 1", c.Len())
	}
}

func TestValueRendering(t *testing.T) {
	cases := []struct {
		v    Value
		want string
	}{
		{Value{Type: String, Str: "hi"}, "hi"},
		{Value{Type: Int, Num: -42}, "-42"},
		{Value{Type: Ref, Ref: ref.Ref(7)}, "#7"},
		{Value{Type: Float, Float: 1.5}, "1.5"},
		{Value{Type: Lock, Str: "me|#1"}, "me|#1"},
	}
	for _, c := range cases {
		if got := c.v.StringValue(); got != c.want {
			t.Errorf("Value(%v).StringValue() = %q, want %q", c.v.Type, got, c.want)
		}
	}
}

func TestSetEmptyPathIsNoop(t *testing.T) {
	tr := New()
	tr.SetString("", "v")
	tr.SetString("/", "v")
	tr.SetString(":junk", "v")
	if tr.Len() != 0 {
		t.Errorf("Len() = %d, want 0: an empty path sets nothing", tr.Len())
	}
}
