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
// Every method runs on the world goroutine, because the interpreter
// is driven from there and never from a connection's own.
type mufHost struct {
	s *Server
	w *world.World
	// caller is the player the program is running for, which MPI
	// needs as the audience for what it evaluates.
	caller ref.Ref
}

func (h *mufHost) Notify(who ref.Ref, msg string) {
	h.s.send(h.w, who, msg)
}

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

// Links returns what an object points at, which differs by type: an
// exit's destinations, a room's drop-to, or a thing's or player's
// home.
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

func (h *mufHost) Contents(obj ref.Ref) []ref.Ref {
	return h.w.Contents(obj)
}
func (h *mufHost) Exits(obj ref.Ref) []ref.Ref {
	return h.w.Exits(obj)
}

func (h *mufHost) MoveTo(what, dest ref.Ref) error {
	return h.w.MoveTo(what, dest)
}

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
	// The type bits are not a program's to change, and the
	// internal flags describe live server state.
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

// Online lists the players with a live connection, in connection
// order.
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
		if o.Type() != ref.TypePlayer ||
			!ascii.HasPrefix(o.Name, name) {
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

func (h *mufHost) Recycle(obj ref.Ref) error {
	return h.w.Recycle(obj)
}

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

// Entrances lists everything that points at an object, which needs a
// scan: nothing records the reverse direction.
//
// Exits that lead there count, and so do a thing's or player's home
// and a room's drop-to, because all three are links to the same
// place.
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
	h.s.securityLog().Warn("password changed by a program",
		"player", player.String(), "name", o.Name,
		"by", h.caller.String(), "byName", nameOf(h.w, h.caller))
	return nil
}

// mesgKind turns PARSEPROP's private flag into a mesgtyp. Public is
// the absence of private upstream, which is why there is nothing to
// return for it.
func mesgKind(private bool) mpi.MesgType {
	if private {
		return mpi.Private
	}
	return 0
}

// ParseProp evaluates a property's MPI, which is how MUF reaches the
// other language.
//
// The private flag is PARSEPROP's own argument and is upstream's
// MPI_ISPRIVATE: it was carried this far and then dropped, because
// nothing in mpi.Env could hold it until MesgType existed.
func (h *mufHost) ParseProp(obj ref.Ref, path, arg string,
	private bool) (string, error) {

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
		Type:    mesgKind(private),
		Host:    &mpiHost{s: h.s, w: h.w},
	}
	if arg != "" {
		if err := env.SetVar("arg", arg); err != nil {
			return "", err
		}
	}
	return mpi.Eval(env, v.StringValue()), nil
}

// ParsePropEx implements muf.Host for PARSEPROPEX: ParseProp with a
// caller's own variables in scope, handed back with whatever the MPI
// left in them.
func (h *mufHost) ParsePropEx(obj ref.Ref, path string,
	vars []muf.MPIVar, private bool) (
	string, []muf.MPIVar, error) {

	o := h.w.Get(obj)
	if o == nil {
		return "", vars, errMsg("no such object")
	}
	v, ok := o.Props.Get(path)
	if !ok || v.StringValue() == "" {
		// Nothing to evaluate, so the variables come back
		// untouched — upstream skips the whole block when
		// the property is empty.
		return "", vars, nil
	}

	env := &mpi.Env{
		Who:     mpi.Ref(h.caller),
		What:    mpi.Ref(obj),
		Perms:   mpi.Ref(obj),
		Blessed: v.Blessed,
		Type:    mesgKind(private),
		Host:    &mpiHost{s: h.s, w: h.w},
	}
	for _, kv := range vars {
		if err := env.SetVar(kv.Name, kv.Value); err != nil {
			return "", vars, err
		}
	}

	out := mpi.Eval(env, v.StringValue())

	// The variables are read back in the order they were given,
	// so the primitive can put them into the same dictionary keys
	// it took them from.
	result := make([]muf.MPIVar, len(vars))
	for i, kv := range vars {
		final, _ := env.Var(kv.Name)
		result[i] = muf.MPIVar{Name: kv.Name, Value: final}
	}
	return out, result, nil
}

// ParseMPI implements muf.Host for PARSEMPI/PARSEMPIBLESSED:
// evaluates source directly as MPI, rather than reading it from a
// property first.
func (h *mufHost) ParseMPI(who ref.Ref, source, arg string, blessed bool) (string, error) {
	if source == "" {
		return "", nil
	}
	env := &mpi.Env{
		Who:     mpi.Ref(who),
		What:    mpi.Ref(who),
		Perms:   mpi.Ref(who),
		Blessed: blessed,
		Host:    &mpiHost{s: h.s, w: h.w},
	}
	if arg != "" {
		if err := env.SetVar("arg", arg); err != nil {
			return "", err
		}
	}
	return mpi.Eval(env, source), nil
}

// BlessProp and IsPropBlessed implement muf.Host for
// BLESSPROP/UNBLESSPROP and BLESSED?.
func (h *mufHost) BlessProp(obj ref.Ref, path string, blessed bool) {
	v, ok := h.GetProp(obj, path)
	if !ok {
		return
	}
	v.Blessed = blessed
	h.SetProp(obj, path, v)
}

func (h *mufHost) IsPropBlessed(obj ref.Ref, path string) bool {
	v, ok := h.GetProp(obj, path)
	return ok && v.Blessed
}

func (h *mufHost) Now() time.Time { return h.w.Now() }

func (h *mufHost) Uptime() time.Duration {
	return h.w.Now().Sub(h.s.started)
}

func (h *mufHost) Version() string {
	return "Fuzzball Emerald " + Version
}

// compiled caches a program's compiled form, keyed by ref.
type compiled struct {
	prog *muf.Program
	err  error
}

// compileProgram compiles a program, caching the result. A program
// that fails to compile caches its error too, so a broken one is not
// recompiled on every use.
//
// The recursion guard is not decoration. $ifcancall compiles the
// program it is asking about, so two libraries that each check the
// other would compile each other for ever — on the world goroutine,
// which means the whole server. Upstream has the same shape and the
// same exposure; a cycle here fails the inner compile instead, which
// makes the condition false rather than hanging.
func (s *Server) compileProgram(w *world.World, r ref.Ref) (*muf.Program, error) {
	if c, ok := s.programs[r]; ok {
		return c.prog, c.err
	}
	if s.compiling[r] {
		return nil, errMsg("that program is already " +
			"being compiled: a compile-time check on " +
			"it would recurse")
	}
	if s.compiling == nil {
		s.compiling = map[ref.Ref]bool{}
	}
	s.compiling[r] = true
	defer delete(s.compiling, r)

	src, ok := w.Source(r)
	if !ok {
		err := errMsg("that program has no source")
		s.programs[r] = compiled{err: err}
		return nil, err
	}

	// Nobody asked for this compile, so its notes have no
	// audience; the editor's path is the one that shows them.
	prog, _, err := s.compileSource(w, r, src)
	s.programs[r] = compiled{prog: prog, err: err}
	return prog, err
}

// compileSource compiles text as if it were a program's source,
// without consulting or updating the cache. The editor needs this to
// check a buffer that has not been saved.
//
// It returns the compiler's notes for the caller to show, and applies
// the properties the directives asked for itself — $author and
// $version are documentation, but $pubdef and $libdef are how a
// library exports anything at all, so a compile that did not write
// them would leave the library callable by nobody.
func (s *Server) compileSource(w *world.World, r ref.Ref,
	src string) (*muf.Program, []string, error) {
	// A program runs at the lower of its own mucker level and its
	// owner's, which is what find_mlev computes. A programmer
	// cannot grant a program more authority than they hold by
	// setting bits on it.
	//
	// Note that a wizard with no mucker bits has level 0, so
	// programs it owns are capped there.
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

	res, err := compiler.CompileResult(src, compiler.Options{
		Ref:            r,
		MLevel:         mlev,
		Defines:        s.definesFor(w, r),
		Macros:         w.MacroTable(),
		Include:        s.includerFor(w),
		ObjVersion:     s.objVersionFor(w, r),
		CanCall:        s.canCallFor(w, r),
		MuckName:       w.Tune.String("muckname"),
		Version:        Version,
		CommentsStrict: w.Tune.Bool("muf_comments_strict"),
	})
	if err != nil {
		return nil, nil, err
	}
	applyCompileProps(w, r, res.Props)
	return res.Program, res.Notes, nil
}

// applyCompileProps writes what the documentation and export
// directives asked for onto the program object.
//
// Upstream writes each one as the directive is read, so a compile
// that fails later still leaves the earlier properties behind. These
// are applied only on success instead: a half-written _defs propdir
// is worse than none, because $include would then read an export list
// describing a program that does not compile.
func applyCompileProps(w *world.World, r ref.Ref,
	writes []compiler.PropWrite) {
	o := w.Get(r)
	if o == nil || len(writes) == 0 {
		return
	}
	// A program is recompiled whenever its cache is cold, so most
	// of these writes change nothing. Only a real change marks
	// the object, or every boot would rewrite every library.
	changed := false
	for _, pw := range writes {
		switch {
		case pw.Delete:
			// Upstream's remove_property frees the whole
			// subtree, which is what "$pubdef :" is for:
			// clearing everything the library exported.
			if o.Props.DeleteDir(pw.Path) > 0 {
				changed = true
			}
		case pw.KeepExisting && o.Props.Exists(pw.Path):
		default:
			if old, ok := o.Props.Get(pw.Path); !ok ||
				old.StringValue() != pw.Value {
				o.Props.SetString(pw.Path, pw.Value)
				changed = true
			}
		}
	}
	if changed {
		w.Touch(r)
	}
}

// InvalidateProgram drops a program's cached compile, which an edit
// needs.
func (s *Server) InvalidateProgram(r ref.Ref) {
	delete(s.programs, r)
}

// cacheProgram installs a compile result, so a program checked in the
// editor runs without being compiled again.
func (s *Server) cacheProgram(r ref.Ref, prog *muf.Program, err error) {
	s.programs[r] = compiled{prog: prog, err: err}
}

// definesFor collects the compile-time definitions a program sees:
// the _defs/ propdir on #0 and on the program's owner.
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

// includerFor resolves a $include target: a registered name through
// the _reg/ propdir on #0, or a bare dbref.
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

// reportMUFError tells a player their program failed, in the shape
// Fuzzball uses.
//
// Two conditions are upstream's, not decoration. The header differs
// depending on whether the player owns the program, because a
// stranger cannot act on the message and is told whom to tell
// instead. And the backtrace appears only to someone who controls the
// program, since it exposes its source.
func (s *Server) reportMUFError(c *ctx, f *muf.Frame, prog ref.Ref, err error) {
	s.reportMUFErrorTo(c.w, c.who, f, prog, err)
}

// runProgram compiles and runs a program on behalf of a player.
//
// It runs to completion on the world goroutine, in instruction slices
// so a long program does not block it indefinitely. Real multitasking
// — running several programs concurrently, and suspending one on
// READ or SLEEP — needs the process queue, which is M7.
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
	// A command or an exit runs its program REGUID, which is the
	// zero value; said out loud because the other launch sites do
	// not, and silence here would look like an oversight.
	// move.c:686.
	f.Perms = muf.RegUID

	me := c.w.Get(c.who)
	loc := ref.Nothing
	if me != nil {
		loc = me.Location
	}
	// COMMAND is the verb and the pushed argument is the rest of
	// the line — upstream's match_cmdname and match_args, two
	// different strings (game.c:695) that SetReserved's own
	// convention makes one. This used to pass arg for both, so a
	// program asking "command @" was told its own argument.
	f.SetReserved(c.who, loc, trigger, c.verb)
	f.Stack[len(f.Stack)-1] = muf.Str(arg)
	f.Descr = c.d.ID

	c.w.Used(prog)

	proc := &process{
		frame:      f,
		player:     c.who,
		program:    prog,
		trigger:    trigger,
		descr:      c.d.ID,
		command:    c.verb,
		started:    c.w.Now(),
		calledData: "FOREGROUND",
	}
	f.PID = s.procs.add(proc)
	f.Started = proc.started
	s.step(c.w, proc)
}

// reportMUFErrorTo is reportMUFError for a process, which has no
// command context to report through.
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

// muf version properties, from include/props.h.
const (
	propMufVersion    = "_version"
	propMufLibVersion = "_lib-version"
)

// compileTargetFor resolves the object a compiler conditional names.
//
// The matcher is narrower than the one $include uses and narrower
// again than match_everything: match_registered, match_absolute and
// — for $ifver only — match_me. "this" is the program being
// compiled, which $ifver spells specially and $ifcancall does not
// accept.
func compileTargetFor(w *world.World, prog ref.Ref, target string,
	allowMe bool) (ref.Ref, bool) {

	if ascii.EqualFold(target, "this") {
		return prog, true
	}
	owner := ownerOf(w, prog)
	m := match.New(w, owner, target).Registered().Absolute()
	if allowMe {
		m = m.Me()
	}
	r := m.Result()
	if r == ref.Nothing || r == ref.Ambiguous || !w.Valid(r) {
		return ref.Nothing, false
	}
	return r, true
}

// objVersionFor answers $ifver and $iflibver.
//
// An object with no version property reads as "0.0" rather than
// failing, which is what makes "$ifver $lib 1.0" false against a
// library that never declared one instead of refusing to compile.
func (s *Server) objVersionFor(w *world.World,
	prog ref.Ref) func(string, bool) (string, bool) {

	return func(target string, lib bool) (string, bool) {
		r, ok := compileTargetFor(w, prog, target, true)
		if !ok {
			return "", false
		}
		path := propMufVersion
		if lib {
			path = propMufLibVersion
		}
		v, found := w.GetProp(r, path)
		if !found || v.Type != props.String || v.Str == "" {
			return "0.0", true
		}
		return v.Str, true
	}
}

// canCallFor answers $ifcancall.
//
// This is *not* CANCALL?'s test, though they read alike. The
// primitive (p_misc.c:1274) weighs the target program's own mucker
// level and the running frame's effective one; the directive
// (compile.c:3837) weighs both *owners'* levels, because at compile
// time there is no frame to have a level. Sharing one function
// between them would make one of the two wrong.
//
// The target is compiled if it has to be, which upstream does too:
// the public functions it exports are not known otherwise.
func (s *Server) canCallFor(w *world.World,
	prog ref.Ref) func(string, string) (bool, bool) {

	return func(target, function string) (bool, bool) {
		r, ok := compileTargetFor(w, prog, target, false)
		if !ok {
			return false, false
		}
		o := w.Get(r)
		if o == nil || o.Type() != ref.TypeProgram {
			// Resolving to something that is not a
			// program is not an error: the condition is
			// simply false, since a non-program exports
			// nothing.
			return false, true
		}

		// Both levels are the *owners'*.
		targetOwner := mlevelOf(w, o.Owner)
		callerOwner := mlevelOf(w, ownerOf(w, prog))
		if targetOwner <= 0 {
			return false, true
		}
		if callerOwner < 4 && o.Owner != ownerOf(w, prog) &&
			!linkable(o.Flags, o.Type()) {
			return false, true
		}

		p, err := s.compileProgram(w, r)
		if err != nil {
			return false, true
		}
		pub, found := p.Publics[ascii.Fold(function)]
		if !found {
			return false, true
		}
		return callerOwner >= pub.MLevel, true
	}
}

// mlevelOf is MLevel(x): the mucker level of an object, which for a
// player is what their own flags say.
func mlevelOf(w *world.World, r ref.Ref) int {
	o := w.Get(r)
	if o == nil {
		return 0
	}
	return o.Flags.MLevel()
}
