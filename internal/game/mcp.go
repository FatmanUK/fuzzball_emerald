package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The MUF host's MCP methods. A program reaches a connection's protocol state
// through these; everything below runs on the world goroutine, and the frame
// has its own lock for the transport side.

// MCPMinLevel is the mucker level a program needs to use MCP, which is the
// mcp_muf_mlev parameter.
func (h *mufHost) MCPMinLevel() int { return int(h.w.Tune.Int("mcp_muf_mlev")) }

// MCPSupports reports the version agreed for a package on a connection.
func (h *mufHost) MCPSupports(descr int, pkg string) (int, int) {
	d := h.s.hub.Get(descr)
	if d == nil {
		return 0, 0
	}
	v := d.MCP.Supports(pkg)
	return v.Major, v.Minor
}

// MCPSend sends an out-of-band message on a connection.
func (h *mufHost) MCPSend(descr int, pkg, name string, args []muf.MCPArg) error {
	d := h.s.hub.Get(descr)
	if d == nil {
		return errMsg("Invalid descriptor number. (1)")
	}
	msg := mcp.NewMessage(pkg, name)
	for _, a := range args {
		msg.AddMultiline(a.Name, a.Lines)
	}
	if err := d.MCP.SendMessage(msg); err != nil {
		return errMsg(err.Error())
	}
	return nil
}

// MCPRegister offers a package to every connection that negotiates from now
// on.
//
// Upstream registers into one global table, so a package a program offers is
// offered to everyone. That is reproduced: a program that registers a package
// expects clients connecting later to be told about it.
func (h *mufHost) MCPRegister(pkg string, minMajor, minMinor, maxMajor, maxMinor int) error {
	return h.s.registerMCPPackage(pkg,
		mcp.Version{Major: minMajor, Minor: minMinor},
		mcp.Version{Major: maxMajor, Minor: maxMinor})
}

// MCPBind makes a program's procedure the handler for one message.
func (h *mufHost) MCPBind(prog ref.Ref, pkg, name string, addr int) error {
	h.s.bindMCP(prog, pkg, name, addr)
	return nil
}

// GUINew opens a dialog on a connection.
//
// The dialog's callbacks resume the program that opened it, which is what lets
// a program put up a dialog, wait, and act on what the user chose.
func (h *mufHost) GUINew(descr int, frame *muf.Frame) (string, error) {
	if h.s.hub.Get(descr) == nil {
		return "", errMsg("Invalid descriptor number. (1)")
	}
	d := h.s.dialogs.New(descr)
	d.OnEvent = func(dlg *mcp.Dialog, ctrl, event string, dismissed bool) {
		h.s.guiEvent(dlg, ctrl, event, dismissed)
	}
	d.OnError = func(dlg *mcp.Dialog, ctrl, code, text string) {
		h.s.guiError(dlg, ctrl, code, text)
	}
	h.s.dialogOwner[d.ID] = frame
	return d.ID, nil
}

// GUIDialog reports which connection a dialog is on.
func (h *mufHost) GUIDialog(id string) (int, bool) {
	d, ok := h.s.dialogs.Find(id)
	if !ok {
		return 0, false
	}
	return d.Descr, true
}

// GUIClose forgets a dialog.
func (h *mufHost) GUIClose(id string) bool {
	delete(h.s.dialogOwner, id)
	return h.s.dialogs.Close(id)
}

// GUIValue reads one line of a control's value.
func (h *mufHost) GUIValue(id, ctrl string, line int) (string, bool) {
	d, ok := h.s.dialogs.Find(id)
	if !ok {
		return "", false
	}
	return d.Value(ctrl, line)
}

// GUIValueLines reads every line of a control's value.
func (h *mufHost) GUIValueLines(id, ctrl string) ([]string, bool) {
	d, ok := h.s.dialogs.Find(id)
	if !ok {
		return nil, false
	}
	return d.Lines(ctrl)
}

// GUIValues reads every control's value.
func (h *mufHost) GUIValues(id string) ([]string, [][]string, bool) {
	d, ok := h.s.dialogs.Find(id)
	if !ok {
		return nil, nil, false
	}
	ctrls := d.Controls()
	values := make([][]string, len(ctrls))
	for i, name := range ctrls {
		values[i], _ = d.Lines(name)
	}
	return ctrls, values, true
}

// GUISetValue records a control's value.
func (h *mufHost) GUISetValue(id, ctrl string, lines []string) {
	if d, ok := h.s.dialogs.Find(id); ok {
		d.SetValue(ctrl, lines)
	}
}

// --- the server side ---------------------------------------------------------

// installMCPHandlers attaches this server's handling to the packages the
// session layer advertises.
//
// The list is built before the server exists — a descriptor needs one the
// moment it is created — so the handlers are filled in here rather than
// declared with the packages.
func (s *Server) installMCPHandlers() {
	list := s.hub.MCPPackageList()
	for i, p := range list {
		switch {
		case ascii.EqualFold(p.Name, mcp.GUIPackage):
			list[i].Handle = mcp.GUIHandler(s.dialogs)
		case ascii.EqualFold(p.Name, mcp.NegotiatePackage):
			// The negotiation package handles itself.
		default:
			// Everything else is offered so a program can claim
			// its messages with MCP_BIND.
			list[i].Handle = s.programPackageHandler()
		}
	}
	s.hub.SetMCPPackages(list)
}

// registerMCPPackage adds a package to what every new connection is offered.
func (s *Server) registerMCPPackage(name string, minVer, maxVer mcp.Version) error {
	if name == "" {
		return errMsg("Package name expected. (1)")
	}
	list := s.hub.MCPPackageList()
	for i, p := range list {
		if ascii.EqualFold(p.Name, name) {
			list[i].MinVer = minVer
			list[i].MaxVer = maxVer
			s.hub.SetMCPPackages(list)
			return nil
		}
	}
	s.hub.SetMCPPackages(append(list, mcp.Package{
		Name: name, MinVer: minVer, MaxVer: maxVer,
		Handle: s.programPackageHandler(),
	}))
	return nil
}

// bindMCP records which program procedure handles a message.
func (s *Server) bindMCP(prog ref.Ref, pkg, name string, addr int) {
	s.mcpBindings[mcpBinding{pkg: ascii.Fold(pkg), name: ascii.Fold(name)}] =
		mcpTarget{prog: prog, addr: addr}
}

// mcpBinding names one message a program has claimed.
type mcpBinding struct{ pkg, name string }

// mcpTarget is the procedure that handles it.
type mcpTarget struct {
	prog ref.Ref
	addr int
}

// programPackageHandler runs whatever program has bound the message.
//
// The handler is called from the connection's goroutine, so the work is
// handed to the world goroutine rather than done here: everything a program
// touches belongs to the world.
func (s *Server) programPackageHandler() func(*mcp.Frame, *mcp.Message, mcp.Version) {
	return func(f *mcp.Frame, msg *mcp.Message, _ mcp.Version) {
		key := mcpBinding{pkg: ascii.Fold(msg.Package), name: ascii.Fold(msg.Name)}
		_ = s.engine.Go(func(w *world.World) {
			target, bound := s.mcpBindings[key]
			if !bound {
				return
			}
			d := s.descriptorFor(f)
			if d == nil || !d.Connected {
				return
			}
			s.runBound(w, d, target, msg)
		})
	}
}

// descriptorFor finds the connection a frame belongs to.
//
// The frame does not name its descriptor — it is given a function to write
// with and nothing else — so the hub is searched. Connection counts are small
// enough that this costs nothing.
func (s *Server) descriptorFor(f *mcp.Frame) *session.Descriptor {
	for _, d := range s.hub.All() {
		if d.MCP == f {
			return d
		}
	}
	return nil
}

// runBound starts a program at the procedure that claimed a message.
//
// The procedure is called with the descriptor and a dictionary of the
// message's arguments: a single-line value is a string, a multi-line one a
// list of strings, and one with no value at all an empty string.
func (s *Server) runBound(w *world.World, d *session.Descriptor,
	target mcpTarget, msg *mcp.Message) {

	prog, err := s.compileProgram(w, target.prog)
	if err != nil {
		s.mufLog().Warn("a program bound to an MCP message does not compile",
			"program", target.prog.String(), "error", err.Error())
		return
	}
	if target.addr < 0 || target.addr >= len(prog.Code) {
		s.mufLog().Warn("an MCP binding points outside its program",
			"program", target.prog.String(), "address", target.addr)
		return
	}

	host := &mufHost{s: s, w: w, caller: d.Player}
	f := muf.NewFrame(prog, host)
	loc := ref.Nothing
	if me := w.Get(d.Player); me != nil {
		loc = me.Location
	}
	f.SetReserved(d.Player, loc, target.prog, "")
	f.Descr = d.ID
	f.PC = target.addr

	args := muf.NewDict()
	for _, a := range msg.Args {
		args.Set(muf.Str(a.Name), mcpArgValue(a))
	}
	if err := f.Push(muf.Int(int64(d.ID))); err != nil {
		return
	}
	if err := f.Push(muf.Arr(args)); err != nil {
		return
	}

	w.Used(target.prog)
	proc := &process{
		frame:   f,
		player:  d.Player,
		program: target.prog,
		trigger: target.prog,
		descr:   d.ID,
		command: msg.Package + "-" + msg.Name,
		started: w.Now(),
	}
	s.procs.add(proc)
	s.step(w, proc)
}

// mcpArgValue renders one message argument as the MUF value a bound procedure
// receives.
func mcpArgValue(a mcp.Arg) muf.Value {
	switch len(a.Lines) {
	case 0:
		return muf.Str("")
	case 1:
		return muf.Str(a.Lines[0])
	}
	vals := make([]muf.Value, len(a.Lines))
	for i, l := range a.Lines {
		vals[i] = muf.Str(l)
	}
	return muf.Arr(muf.NewList(vals))
}

// guiEvent tells a suspended program that the user did something.
func (s *Server) guiEvent(d *mcp.Dialog, ctrl, event string, dismissed bool) {
	_ = s.engine.Go(func(w *world.World) {
		s.log.Debug("gui event",
			"dialog", d.ID, "control", ctrl, "event", event,
			"dismissed", dismissed)
		if dismissed {
			delete(s.dialogOwner, d.ID)
			s.dialogs.Close(d.ID)
		}
	})
}

// guiError tells a program that the client could not do what it asked.
func (s *Server) guiError(d *mcp.Dialog, ctrl, code, text string) {
	_ = s.engine.Go(func(w *world.World) {
		s.log.Info("gui error reported by a client",
			"dialog", d.ID, "control", ctrl, "code", code, "text", text)
	})
}

// closeDialogsFor forgets every dialog on a connection that has gone away.
func (s *Server) closeDialogsFor(descr int) {
	for _, id := range s.dialogs.CloseDescr(descr) {
		delete(s.dialogOwner, id)
	}
}
