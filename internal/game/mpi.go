package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// mpiHost lets MPI reach the world. Like the MUF host, every method runs on
// the world goroutine.
type mpiHost struct {
	s *Server
	w *world.World
}

func (h *mpiHost) Name(obj mpi.Ref) string { return nameOf(h.w, ref.Ref(obj)) }

func (h *mpiHost) GetPropStr(obj mpi.Ref, path string) string {
	v, ok := h.w.GetProp(ref.Ref(obj), path)
	if !ok {
		return ""
	}
	return v.StringValue()
}

func (h *mpiHost) SetPropStr(obj mpi.Ref, path, val string) {
	h.w.SetProp(ref.Ref(obj), path, props.Value{Type: props.String, Str: val})
}

func (h *mpiHost) Location(obj mpi.Ref) mpi.Ref {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return mpi.Ref(o.Location)
	}
	return mpi.Ref(ref.Nothing)
}

func (h *mpiHost) Owner(obj mpi.Ref) mpi.Ref {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return mpi.Ref(o.Owner)
	}
	return mpi.Ref(ref.Nothing)
}

func (h *mpiHost) Contents(obj mpi.Ref) []mpi.Ref {
	got := h.w.Contents(ref.Ref(obj))
	out := make([]mpi.Ref, len(got))
	for i, r := range got {
		out[i] = mpi.Ref(r)
	}
	return out
}

func (h *mpiHost) Valid(obj mpi.Ref) bool { return h.w.Valid(ref.Ref(obj)) }

func (h *mpiHost) IsPlayer(obj mpi.Ref) bool {
	o := h.w.Get(ref.Ref(obj))
	return o != nil && o.Type() == ref.TypePlayer
}

func (h *mpiHost) Online(obj mpi.Ref) bool {
	return h.s.hub.Online(ref.Ref(obj))
}

func (h *mpiHost) Match(who mpi.Ref, name string) mpi.Ref {
	return mpi.Ref(match.New(h.w, ref.Ref(who), name).Everything().Player().Result())
}

func (h *mpiHost) Notify(obj mpi.Ref, msg string) { h.s.send(h.w, ref.Ref(obj), msg) }

func (h *mpiHost) Now() int64 { return h.w.Now().Unix() }

// evalMPI evaluates a property's text for a viewer.
//
// Only text that actually contains a call is parsed, so an ordinary
// description costs nothing and cannot be changed by a stray brace.
func (s *Server) evalMPI(w *world.World, viewer, what ref.Ref, text string, blessed bool) string {
	if !strings.ContainsRune(text, '{') {
		return text
	}
	if !w.Tune.Bool("do_mpi_parsing") {
		return text
	}

	env := &mpi.Env{
		Who:     mpi.Ref(viewer),
		What:    mpi.Ref(what),
		Perms:   mpi.Ref(what),
		Blessed: blessed,
		Host:    &mpiHost{s: s, w: w},
	}
	// Eval reports a failure to the viewer and yields empty text rather than
	// propagating, so a broken description cannot break the look.
	return mpi.Eval(env, text)
}

// mesgProp reads a message property and evaluates any MPI in it.
func (s *Server) mesgProp(w *world.World, viewer, obj ref.Ref, path string) string {
	o := w.Get(obj)
	if o == nil {
		return ""
	}
	v, ok := o.Props.Get(path)
	if !ok || v.Type != props.String {
		return ""
	}
	return s.evalMPI(w, viewer, obj, v.Str, v.Blessed)
}
