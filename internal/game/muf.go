package game

import (
	"strings"

	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf/compiler"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
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
	// caller is the player the program is running for, which MPI needs as
	// the audience for what it evaluates.
	caller ref.Ref
}

func (h *mufHost) Notify(who ref.Ref, msg string) { h.s.send(h.w, who, msg) }

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

// Online lists the players with a live connection, in connection order.
func (h *mufHost) Online() []ref.Ref {
	seen := map[ref.Ref]bool{}
	var out []ref.Ref
	for _, d := range h.s.hub.Connected() {
		if !seen[d.Player] {
			seen[d.Player] = true
			out = append(out, d.Player)
		}
	}
	return out
}

func (h *mufHost) DescrPlayer(descr int) ref.Ref {
	if d := h.s.hub.Get(descr); d != nil && d.Connected {
		return d.Player
	}
	return ref.Nothing
}

func (h *mufHost) DescrSize(descr int) (int, int) {
	if d := h.s.hub.Get(descr); d != nil {
		return d.Width, d.Height
	}
	return 80, 24
}

func (h *mufHost) MatchPlayerPrefix(name string) ref.Ref {
	name = strings.TrimPrefix(name, "*")
	if r, ok := h.w.PlayerNamed(name); ok {
		return r
	}
	// No exact match, so accept a unique prefix.
	found := ref.Nothing
	h.w.Each(func(o *world.Object) bool {
		if o.Type() != ref.TypePlayer || !ascii.HasPrefix(o.Name, name) {
			return true
		}
		if found != ref.Nothing {
			found = ref.Ambiguous
			return false
		}
		found = o.Ref
		return true
	})
	if found == ref.Ambiguous {
		return ref.Nothing
	}
	return found
}

func (h *mufHost) Create(t ref.ObjType, name string, parent, owner ref.Ref) (ref.Ref, error) {
	if !h.w.Valid(parent) && t != ref.TypeRoom {
		return ref.Nothing, errMsg("that parent does not exist")
	}
	o := h.w.Create(name, t, owner)
	switch t {
	case ref.TypeRoom:
		o.Dropto = ref.Nothing
	case ref.TypeThing:
		o.Home = parent
	}
	if h.w.Valid(parent) {
		if err := h.w.MoveTo(o.Ref, parent); err != nil {
			return o.Ref, err
		}
	}
	return o.Ref, nil
}

func (h *mufHost) Recycle(obj ref.Ref) error { return h.w.Recycle(obj) }

func (h *mufHost) SetOwner(obj, owner ref.Ref) {
	if o := h.w.Get(obj); o != nil {
		o.Owner = owner
		h.w.Modified(obj)
	}
}

// SetLinks writes what an object points at, which differs by type.
func (h *mufHost) SetLinks(obj ref.Ref, dests []ref.Ref) {
	o := h.w.Get(obj)
	if o == nil {
		return
	}
	switch o.Type() {
	case ref.TypeExit:
		o.Dest = dests
	case ref.TypeRoom:
		o.Dropto = firstOr(dests, ref.Nothing)
	case ref.TypeThing, ref.TypePlayer:
		o.Home = firstOr(dests, ref.Nothing)
	}
	h.w.Modified(obj)
}

func firstOr(refs []ref.Ref, fallback ref.Ref) ref.Ref {
	if len(refs) == 0 {
		return fallback
	}
	return refs[0]
}

func (h *mufHost) Timestamps(obj ref.Ref) (int64, int64, int64, int32) {
	o := h.w.Get(obj)
	if o == nil {
		return 0, 0, 0, 0
	}
	return o.Created.Unix(), o.Modified.Unix(), o.LastUsed.Unix(), o.UseCount
}

// Entrances lists everything that points at an object, which needs a scan:
// nothing records the reverse direction.
//
// Exits that lead there count, and so do a thing's or player's home and a
// room's drop-to, because all three are links to the same place.
func (h *mufHost) Entrances(target ref.Ref) []ref.Ref {
	var out []ref.Ref
	h.w.Each(func(o *world.Object) bool {
		switch o.Type() {
		case ref.TypeExit:
			for _, d := range o.Dest {
				if d == target {
					out = append(out, o.Ref)
					return true
				}
			}
		case ref.TypeThing, ref.TypePlayer:
			if o.Home == target {
				out = append(out, o.Ref)
			}
		case ref.TypeRoom:
			if o.Dropto == target {
				out = append(out, o.Ref)
			}
		}
		return true
	})
	return out
}

func (h *mufHost) CheckPassword(player ref.Ref, pass string) bool {
	o := h.w.Get(player)
	if o == nil || o.Type() != ref.TypePlayer {
		return false
	}
	return password.Verify(o.PasswordHash, pass).OK
}

func (h *mufHost) SetPassword(player ref.Ref, pass string) error {
	o := h.w.Get(player)
	if o == nil || o.Type() != ref.TypePlayer {
		return errMsg("that is not a player")
	}
	hashed, err := password.Hash(pass)
	if err != nil {
		return err
	}
	o.PasswordHash = hashed
	h.w.Modified(player)
	return nil
}

// ParseProp evaluates a property's MPI, which is how MUF reaches the other
// language.
func (h *mufHost) ParseProp(obj ref.Ref, path, arg string, private bool) (string, error) {
	o := h.w.Get(obj)
	if o == nil {
		return "", errMsg("no such object")
	}
	v, ok := o.Props.Get(path)
	if !ok {
		return "", nil
	}

	env := &mpi.Env{
		Who:     mpi.Ref(h.caller),
		What:    mpi.Ref(obj),
		Perms:   mpi.Ref(obj),
		Blessed: v.Blessed,
		Host:    &mpiHost{s: h.s, w: h.w},
	}
	if arg != "" {
		if err := env.SetVar("arg", arg); err != nil {
			return "", err
		}
	}
	return mpi.Eval(env, v.StringValue()), nil
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

	prog, err := s.compileSource(w, r, src)
	s.programs[r] = compiled{prog: prog, err: err}
	return prog, err
}

// compileSource compiles text as if it were a program's source, without
// consulting or updating the cache. The editor needs this to check a buffer
// that has not been saved.
func (s *Server) compileSource(w *world.World, r ref.Ref, src string) (*muf.Program, error) {
	// A program runs at the lower of its own mucker level and its owner's,
	// which is what find_mlev computes. A programmer cannot grant a program
	// more authority than they hold by setting bits on it.
	//
	// Note that a wizard with no mucker bits has level 0, so programs it
	// owns are capped there.
	o := w.Get(r)
	mlev := 1
	if o != nil {
		mlev = o.Flags.MLevel()
		if owner := w.Get(o.Owner); owner != nil {
			if lim := owner.Flags.MLevel(); lim < mlev {
				mlev = lim
			}
		}
	}

	return compiler.Compile(src, compiler.Options{
		Ref:      r,
		MLevel:   mlev,
		Defines:  s.definesFor(w, r),
		Macros:   w.MacroTable(),
		Include:  s.includerFor(w),
		MuckName: w.Tune.String("muckname"),
		Version:  Version,
	})
}

// InvalidateProgram drops a program's cached compile, which an edit needs.
func (s *Server) InvalidateProgram(r ref.Ref) { delete(s.programs, r) }

// cacheProgram installs a compile result, so a program checked in the editor
// runs without being compiled again.
func (s *Server) cacheProgram(r ref.Ref, prog *muf.Program, err error) {
	s.programs[r] = compiled{prog: prog, err: err}
}

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
	s.reportMUFErrorTo(c.w, c.who, f, prog, err)
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

	host := &mufHost{s: s, w: c.w, caller: c.who}
	f := muf.NewFrame(p, host)

	me := c.w.Get(c.who)
	loc := ref.Nothing
	if me != nil {
		loc = me.Location
	}
	f.SetReserved(c.who, loc, trigger, arg)
	f.Descr = c.d.ID

	c.w.Used(prog)

	proc := &process{
		frame:   f,
		player:  c.who,
		program: prog,
		trigger: trigger,
		descr:   c.d.ID,
		command: c.verb,
		started: c.w.Now(),
	}
	f.PID = s.procs.add(proc)
	s.step(c.w, proc)
}

// reportMUFErrorTo is reportMUFError for a process, which has no command
// context to report through.
func (s *Server) reportMUFErrorTo(w *world.World, who ref.Ref, f *muf.Frame,
	prog ref.Ref, err error) {

	rep := f.Report(err)

	owner := ref.Nothing
	if o := w.Get(prog); o != nil {
		owner = o.Owner
	}
	owned := owner == who
	if !s.controls(w, who, prog) {
		rep.Frames = nil
	}

	progName := func(r ref.Ref) string { return nameOf(w, r) }
	sourceLine := func(r ref.Ref, line int) (string, bool) {
		src, ok := w.Source(r)
		if !ok || line < 1 {
			return "", false
		}
		lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
		if line > len(lines) {
			return "", false
		}
		return lines[line-1], true
	}

	for _, line := range rep.Render(owned, nameOf(w, owner), progName, sourceLine) {
		s.send(w, who, line)
	}

	s.mufLog().Warn("runtime error",
		"program", prog.String(),
		"player", who.String(),
		"line", rep.Line,
		"instruction", rep.Inst,
		"error", rep.Msg)
}
