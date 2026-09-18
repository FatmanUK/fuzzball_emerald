package game

import (
	"strings"

	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
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

func (h *mufHost) NotifyExcept(room ref.Ref, except []ref.Ref, msg string) {
	h.s.notifyRoom(h.w, room, except, "%s", msg)
}

func (h *mufHost) Name(obj ref.Ref) string { return nameOf(h.w, obj) }

func (h *mufHost) SetName(obj ref.Ref, name string) error {
	return h.w.Rename(obj, name)
}

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

func (h *mufHost) Home(obj ref.Ref) ref.Ref {
	if o := h.w.Get(obj); o != nil {
		return o.Home
	}
	return ref.Nothing
}

// Links returns what an object points at, which differs by type: an exit's
// destinations, a room's drop-to, or a thing's or player's home.
func (h *mufHost) Links(obj ref.Ref) []ref.Ref {
	o := h.w.Get(obj)
	if o == nil {
		return nil
	}
	switch o.Type() {
	case ref.TypeExit:
		return o.Dest
	case ref.TypeRoom:
		if o.Dropto == ref.Nothing {
			return nil
		}
		return []ref.Ref{o.Dropto}
	case ref.TypeThing, ref.TypePlayer:
		if o.Home == ref.Nothing {
			return nil
		}
		return []ref.Ref{o.Home}
	}
	return nil
}

func (h *mufHost) Contents(obj ref.Ref) []ref.Ref { return h.w.Contents(obj) }
func (h *mufHost) Exits(obj ref.Ref) []ref.Ref    { return h.w.Exits(obj) }

func (h *mufHost) MoveTo(what, dest ref.Ref) error { return h.w.MoveTo(what, dest) }

func (h *mufHost) Valid(obj ref.Ref) bool { return h.w.Valid(obj) }

func (h *mufHost) ObjType(obj ref.Ref) ref.ObjType {
	if o := h.w.Get(obj); o != nil {
		return o.Type()
	}
	return ref.NoType
}

func (h *mufHost) Flags(obj ref.Ref) ref.Flags {
	if o := h.w.Get(obj); o != nil {
		return o.Flags
	}
	return 0
}

func (h *mufHost) SetFlags(obj ref.Ref, f ref.Flags) {
	o := h.w.Get(obj)
	if o == nil {
		return
	}
	// The type bits are not a program's to change, and the internal flags
	// describe live server state.
	o.Flags = (f &^ ref.DumpMask).WithType(o.Type())
	h.w.Modified(obj)
}

func (h *mufHost) Top() ref.Ref { return h.w.Top() }

func (h *mufHost) GetProp(obj ref.Ref, path string) (props.Value, bool) {
	return h.w.GetProp(obj, path)
}

func (h *mufHost) SetProp(obj ref.Ref, path string, v props.Value) {
	h.w.SetProp(obj, path, v)
}

func (h *mufHost) RemoveProp(obj ref.Ref, path string) {
	if o := h.w.Get(obj); o != nil {
		o.Props.Delete(path)
		h.w.Modified(obj)
	}
}

func (h *mufHost) PropChildren(obj ref.Ref, path string) []string {
	if o := h.w.Get(obj); o != nil {
		return o.Props.Children(path)
	}
	return nil
}

func (h *mufHost) Match(who ref.Ref, name string) ref.Ref {
	return match.New(h.w, who, name).Everything().Player().Result()
}

func (h *mufHost) MatchPlayer(name string) ref.Ref {
	r, ok := h.w.PlayerNamed(strings.TrimPrefix(name, "*"))
	if !ok {
		return ref.Nothing
	}
	return r
}

func (h *mufHost) Connections(player ref.Ref) int {
	return len(h.s.hub.DescriptorsFor(player))
}

func (h *mufHost) Descriptors(player ref.Ref) []int {
	ds := h.s.hub.DescriptorsFor(player)
	out := make([]int, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}

func (h *mufHost) Now() time.Time { return h.w.Now() }

func (h *mufHost) Uptime() time.Duration { return h.w.Now().Sub(h.s.started) }

func (h *mufHost) Version() string { return "Fuzzball Emerald " + Version }

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

// reportMUFError tells a player their program failed, in the shape Fuzzball
// uses.
//
// Two conditions are upstream's, not decoration. The header differs depending
// on whether the player owns the program, because a stranger cannot act on the
// message and is told whom to tell instead. And the backtrace appears only to
// someone who controls the program, since it exposes its source.
func (s *Server) reportMUFError(c *ctx, f *muf.Frame, prog ref.Ref, err error) {
	rep := f.Report(err)

	owner := ref.Nothing
	if o := c.w.Get(prog); o != nil {
		owner = o.Owner
	}
	owned := owner == c.who
	if !s.controls(c.w, c.who, prog) {
		// Without control there is no backtrace to show.
		rep.Frames = nil
	}

	progName := func(r ref.Ref) string { return nameOf(c.w, r) }
	sourceLine := func(r ref.Ref, line int) (string, bool) {
		src, ok := c.w.Source(r)
		if !ok || line < 1 {
			return "", false
		}
		lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
		if line > len(lines) {
			return "", false
		}
		return lines[line-1], true
	}

	for _, line := range rep.Render(owned, nameOf(c.w, owner), progName, sourceLine) {
		c.send(line)
	}

	s.mufLog().Warn("runtime error",
		"program", prog.String(),
		"player", c.who.String(),
		"line", rep.Line,
		"instruction", rep.Inst,
		"error", rep.Msg)
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
			s.reportMUFError(c, f, prog, err)
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
