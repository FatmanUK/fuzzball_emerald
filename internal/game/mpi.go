package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// mpiHost lets MPI reach the world. Like the MUF host, every method
// runs on the world goroutine.
type mpiHost struct {
	s *Server
	w *world.World
}

func (h *mpiHost) Name(obj mpi.Ref) string {
	return nameOf(h.w, ref.Ref(obj))
}

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

func (h *mpiHost) DelProp(obj mpi.Ref, path string) {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		o.Props.Delete(path)
		h.w.Modified(ref.Ref(obj))
	}
}

func (h *mpiHost) PropChildren(obj mpi.Ref, path string) []string {
	if o := h.w.Get(ref.Ref(obj)); o != nil {
		return o.Props.Children(path)
	}
	return nil
}

func (h *mpiHost) BlessProp(obj mpi.Ref, path string, blessed bool) {
	v, ok := h.w.GetProp(ref.Ref(obj), path)
	if !ok {
		return
	}
	v.Blessed = blessed
	h.w.SetProp(ref.Ref(obj), path, v)
}

func (h *mpiHost) Parent(obj mpi.Ref) mpi.Ref {
	return mpi.Ref(h.w.Parent(ref.Ref(obj)))
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

func (h *mpiHost) Valid(obj mpi.Ref) bool {
	return h.w.Valid(ref.Ref(obj))
}

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

func (h *mpiHost) Notify(obj mpi.Ref, msg string) {
	h.s.send(h.w, ref.Ref(obj), msg)
}

func (h *mpiHost) Now() int64 { return h.w.Now().Unix() }

// evalMPI evaluates a property's text for a viewer.
//
// Only text that actually contains a call is parsed, so an ordinary
// description costs nothing and cannot be changed by a stray brace.
func (s *Server) evalMPI(w *world.World, descr int,
	viewer, what ref.Ref, text, how string, blessed bool,
	kind mpi.MesgType) string {

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
		Type:    kind,
		Descr:   descr,
		Host:    &mpiHost{s: s, w: w},
	}
	// do_parse_mesg_2 (msgparse.c:1919) allocates three variables
	// for every evaluation, and all three have to exist or
	// referring to one is an MPI *error* rather than an empty
	// string.
	//
	// {&how} is the caller context — "(@Desc)", "(@Succ)" —
	// the same whatcalled string exec_or_notify passes. MPI's
	// {muf} reads it back to build the COMMAND variable it hands
	// a program, so leaving it out was visible from inside MUF.
	//
	// {&cmd} and {&arg} are match_cmdname and match_args, and
	// both read **empty** here. That was measured against the
	// oracle rather than derived: a description or a @succ
	// reached by looking reports them empty on every command
	// tried, and the golden case pins it. If a world ever finds a
	// path where upstream's are not empty, this is the line to
	// revisit — Emerald has no ambient equivalent of those two
	// globals to fill them from.
	_ = env.SetVar("how", how)
	_ = env.SetVar("cmd", "")
	_ = env.SetVar("arg", "")

	// Eval reports a failure to the viewer and yields empty text
	// rather than propagating, so a broken description cannot
	// break the look.
	return mpi.Eval(env, text)
}
