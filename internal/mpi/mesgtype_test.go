package mpi

import (
	"strings"
	"testing"
)

// TestListenerCannotTell is the whole reason MesgType exists. A
// listen propqueue fires on any object that has the property, so
// without this gate a thing lying in a room could send a private
// message to anyone who spoke near it.
//
// Upstream tests the object *carrying* the message, not the target:
// only a room may speak this way.
func TestListenerCannotTell(t *testing.T) {
	for _, fn := range []string{"tell", "otell"} {
		for _, tc := range []struct {
			what    Ref
			kind    MesgType
			blocked bool
		}{
			{0, Listener, false}, // #0 is the stub's room
			{2, Listener, true},  // #2 is a thing
			{2, Private, false},
			{2, 0, false},
		} {
			env := newEnv(newStub())
			env.What = tc.what
			env.Type = tc.kind

			out, err := Parse(env, "{"+fn+":hello}")
			blocked := err != nil &&
				strings.Contains(err.Error(),
					"Permission denied.")
			if blocked != tc.blocked {
				t.Errorf("{%s} on %v as %v: blocked=%v, "+
					"want %v (out=%q err=%v)",
					fn, tc.what, tc.kind, blocked,
					tc.blocked, out, err)
			}
		}
	}
}

// TestPublicIsTheAbsenceOfPrivate pins how upstream spells it:
// MPI_ISPUBLIC is zero and is never tested for.
func TestPublicIsTheAbsenceOfPrivate(t *testing.T) {
	if !MesgType(0).Public() {
		t.Error("no flags at all should read as public")
	}
	if !Listener.Public() {
		t.Error("a listener message is public unless it says so")
	}
	if Private.Public() {
		t.Error("private is not public")
	}
	if !(Private | Listener).Has(Listener) {
		t.Error("Has does not see a flag beside another")
	}
}
