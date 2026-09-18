package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf/compiler"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// mufHost lets a running program reach the world.
//
// Every method runs on the world goroutine, because the interpreter is driven
// from there and never from a connection's own.
type mufHost struct {
	s *Server
	w *world.World
}

func (h *mufHost) Notify(who ref.Ref, msg string) { h.s.send(who, msg) }

func (h *mufHost) GetPropStr(obj ref.Ref, path string) string {
	v, ok := h.w.GetProp(obj, path)
	if !ok {
		return ""
	}
	return v.StringValue()
}

func (h *mufHost) SetPropStr(obj ref.Ref, path, val string) {
	h.w.SetProp(obj, path, props.Value{Type: props.String, Str: val})
}

func (h *mufHost) Name(obj ref.Ref) string { return nameOf(h.w, obj) }

func (h *mufHost) Location(obj ref.Ref) ref.Ref {
	if o := h.w.Get(obj); o != nil {
		return o.Location
	}
	return ref.Nothing
}

func (h *mufHost) Owner(obj ref.Ref) ref.Ref {
	if o := h.w.Get(obj); o != nil {
		return o.Owner
	}
	return ref.Nothing
}

func (h *mufHost) Valid(obj ref.Ref) bool { return h.w.Valid(obj) }

func (h *mufHost) ObjType(obj ref.Ref) ref.ObjType {
	if o := h.w.Get(obj); o != nil {
		return o.Type()
	}
	return ref.NoType
}

// compiled caches a program's compiled form, keyed by ref.
type compiled struct {
	prog *muf.Program
	err  error
}

// compileProgram compiles a program, caching the result. A program that fails
// to compile caches its error too, so a broken one is not recompiled on every
// use.
func (s *Server) compileProgram(w *world.World, r ref.Ref) (*muf.Program, error) {
	if c, ok := s.programs[r]; ok {
		return c.prog, c.err
	}

	src, ok := w.Source(r)
	if !ok {
		err := errMsg("that program has no source")
		s.programs[r] = compiled{err: err}
		return nil, err
	}

	o := w.Get(r)
	mlev := 1
	if o != nil {
		mlev = w.Get(o.Owner).Flags.MLevel()
	}

	prog, err := compiler.Compile(src, compiler.Options{
		Ref:      r,
		MLevel:   mlev,
		Defines:  s.definesFor(w, r),
		Macros:   s.macros,
		Include:  s.includerFor(w),
		MuckName: w.Tune.String("muckname"),
		Version:  Version,
	})
	s.programs[r] = compiled{prog: prog, err: err}
	return prog, err
}

// InvalidateProgram drops a program's cached compile, which an edit needs.
func (s *Server) InvalidateProgram(r ref.Ref) { delete(s.programs, r) }

// SetMacros installs the MUF editor's macro table.
func (s *Server) SetMacros(m map[string]string) { s.macros = m }

// definesFor collects the compile-time definitions a program sees: the _defs/
// propdir on #0 and on the program's owner.
func (s *Server) definesFor(w *world.World, prog ref.Ref) map[string]string {
	out := map[string]string{}
	collectDefs(w, ref.GlobalEnvironment, out)
	if o := w.Get(prog); o != nil {
		collectDefs(w, o.Owner, out)
	}
	return out
}

// collectDefs copies an object's _defs/ propdir into out.
func collectDefs(w *world.World, holder ref.Ref, out map[string]string) {
	o := w.Get(holder)
	if o == nil {
		return
	}
	for _, e := range o.Props.All() {
		if name, ok := strings.CutPrefix(e.Path, "_defs/"); ok &&
			e.Value.Type == props.String {
			out[name] = e.Value.Str
		}
	}
}

// includerFor resolves a $include target: a registered name through the _reg/
// propdir on #0, or a bare dbref.
func (s *Server) includerFor(w *world.World) func(string) (map[string]string, bool) {
	return func(target string) (map[string]string, bool) {
		var r ref.Ref
		switch {
		case strings.HasPrefix(target, "$"):
			v, ok := w.GetProp(ref.GlobalEnvironment, "_reg/"+target[1:])
			if !ok || v.Type != props.Ref {
				return nil, false
			}
			r = v.Ref
		case strings.HasPrefix(target, "#"):
			parsed, err := ref.Parse(target)
			if err != nil {
				return nil, false
			}
			r = parsed
		default:
			return nil, false
		}

		o := w.Get(r)
		if o == nil || o.Type() != ref.TypeProgram {
			return nil, false
		}
		defs := map[string]string{}
		collectDefs(w, r, defs)
		return defs, true
	}
}

// runProgram compiles and runs a program on behalf of a player.
//
// It runs to completion on the world goroutine, in instruction slices so a
// long program does not block it indefinitely. Real multitasking — running
// several programs concurrently, and suspending one on READ or SLEEP — needs
// the process queue, which is M7.
func (s *Server) runProgram(c *ctx, prog ref.Ref, trigger ref.Ref, arg string) {
	p, err := s.compileProgram(c.w, prog)
	if err != nil {
		c.tell("That program does not compile: %s", err.Error())
		s.mufLog().Warn("compile failed",
			"program", prog.String(), "error", err.Error())
		return
	}

	host := &mufHost{s: s, w: c.w}
	f := muf.NewFrame(p, host)

	me := c.w.Get(c.who)
	loc := ref.Nothing
	if me != nil {
		loc = me.Location
	}
	f.SetReserved(c.who, loc, trigger, arg)

	c.w.Used(prog)

	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			c.tell("Programmer Error: %s", err.Error())
			s.mufLog().Warn("runtime error",
				"program", prog.String(),
				"player", c.who.String(),
				"error", err.Error())
			return
		}
		switch res {
		case muf.Done:
			return
		case muf.Blocked:
			// READ and SLEEP need the process queue.
			c.tell("That program tried to wait for input, which this server cannot do yet.")
			return
		case muf.Yielded:
			// Give the world goroutine a chance to breathe between
			// slices by re-queueing rather than looping here.
			continue
		}
	}
}
