package world

import (
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestIsListenPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"_listen", true},
		{"_listen/greeting", true},
		// string_prefix, not an exact match, so a longer name
		// at the root counts too. That is upstream's and is
		// what makes the flag and the property disagree.
		{"_listenup", true},
		{"~listen", true},
		{"~olisten", true},
		{"_LISTEN", true},
		{"/_listen", true},
		{"//_listen", true},

		// Only the root. A listen prop inside a propdir is
		// just a property.
		{"foo/_listen", false},
		{"_liste", false},
		{"listen", false},
		{"_desc", false},
		{"", false},
	} {
		if got := IsListenPath(tc.path); got != tc.want {
			t.Errorf("IsListenPath(%q) = %v, want %v",
				tc.path, got, tc.want)
		}
	}
}

// TestSetPropMarksListener checks the one place a non-DISKBASE build
// sets the flag, and the one-way-ness that comes with it.
func TestSetPropMarksListener(t *testing.T) {
	w := New()
	o := w.Create("radio", ref.TypeThing, ref.God)

	if o.Flags&ref.Listener != 0 {
		t.Fatal("a fresh object is already a listener")
	}
	w.SetProp(o.Ref, "_desc", props.Value{
		Type: props.String, Str: "a radio"})
	if o.Flags&ref.Listener != 0 {
		t.Error("an ordinary property made a listener")
	}

	w.SetProp(o.Ref, "_listen/hello", props.Value{
		Type: props.String, Str: "&Hi."})
	if o.Flags&ref.Listener == 0 {
		t.Error("a _listen property did not set the flag")
	}
	if !w.IsListener(o.Ref) {
		t.Error("IsListener says no with the flag and the prop set")
	}

	// Deleting the property leaves the flag, which is upstream's:
	// set_property is the only thing that touches it. IsListener
	// is what makes that invisible.
	o.Props.Delete("_listen/hello")
	if o.Flags&ref.Listener == 0 {
		t.Error("the flag was cleared on delete; upstream keeps it")
	}
	if w.IsListener(o.Ref) {
		t.Error("IsListener trusted a stale flag")
	}
}

// TestRecomputeListenersIsTheOnlyWayBack covers the load path: the
// flag is in ref.DumpMask, so it arrives cleared and has to be
// derived from the properties.
func TestRecomputeListenersIsTheOnlyWayBack(t *testing.T) {
	w := New()
	quiet := w.Create("rock", ref.TypeThing, ref.God)
	loud := w.Create("bell", ref.TypeThing, ref.God)
	wiz := w.Create("mirror", ref.TypeThing, ref.God)
	buried := w.Create("box", ref.TypeThing, ref.God)

	// Written straight onto the tree, the way a load does.
	loud.Props.SetString("_listen", "&Ding.")
	wiz.Props.SetString("~olisten", "&Reflected.")
	buried.Props.SetString("stuff/_listen", "&Nothing.")
	quiet.Flags |= ref.Listener // a stale flag from somewhere

	w.RecomputeListeners()

	for _, tc := range []struct {
		o    *Object
		want bool
	}{
		{loud, true},
		{wiz, true},
		{quiet, false},
		{buried, false},
	} {
		got := tc.o.Flags&ref.Listener != 0
		if got != tc.want {
			t.Errorf("%s: listener = %v, want %v",
				tc.o.Name, got, tc.want)
		}
	}
}

// TestHasListenPropIsExact pins the asymmetry: the flag is set by a
// prefix test and read back by an exact one, so "_listenup" makes a
// listener that @sweep will never report.
func TestHasListenPropIsExact(t *testing.T) {
	w := New()
	o := w.Create("oddity", ref.TypeThing, ref.God)
	w.SetProp(o.Ref, "_listenup", props.Value{
		Type: props.String, Str: "&Eh?"})

	if o.Flags&ref.Listener == 0 {
		t.Error("_listenup did not set the flag")
	}
	if w.HasListenProp(o.Ref) {
		t.Error("_listenup counted as a listen propqueue")
	}
	if w.IsListener(o.Ref) {
		t.Error("IsListener reported a flag with no propqueue")
	}

	// A propdir with no value of its own does count, because
	// upstream's get_property finds the node either way.
	w.SetProp(o.Ref, "_listen/deeper", props.Value{
		Type: props.String, Str: "&Yes."})
	if !w.HasListenProp(o.Ref) {
		t.Error("a _listen propdir did not count")
	}
}
