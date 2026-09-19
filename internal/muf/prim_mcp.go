package muf

// MCP and MCP-GUI primitives: how a program talks to a client out of band, and
// draws dialogs on the ones that can show them.
//
// These are gated at the mucker level the mcp_muf_mlev parameter names, which
// the generated table applies, with one addition upstream makes: a program's
// own caller may use them regardless, so a library can offer a dialog to a
// program that could not open one itself.

func init() {
	register("MCP_SUPPORTS", func(f *Frame) (*Result, error) {
		pkg, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		descr, h, err := f.descrAndHost()
		if err != nil {
			return nil, err
		}
		major, minor := h.MCPSupports(descr, pkg)
		return nil, f.Push(Float(version(major, minor)))
	})

	register("MCP_REGISTER", func(f *Frame) (*Result, error) {
		maxVer, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		minVer, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		pkg, err := f.popStrArg(1)
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		minMaj, minMin := splitVersion(minVer)
		maxMaj, maxMin := splitVersion(maxVer)
		if err := h.MCPRegister(pkg, minMaj, minMin, maxMaj, maxMin); err != nil {
			return nil, err
		}
		return nil, nil
	})

	register("MCP_BIND", func(f *Frame) (*Result, error) {
		addr, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if addr.Type != TypeAddress {
			return nil, errf("Function address expected. (3)")
		}
		name, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		pkg, err := f.popStrArg(1)
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		return nil, h.MCPBind(f.Prog.Ref, pkg, name, int(addr.Num))
	})

	register("MCP_SEND", func(f *Frame) (*Result, error) {
		args, err := f.popArrayArg(4, "Dictionary of arguments expected.")
		if err != nil {
			return nil, err
		}
		name, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		pkg, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		descr, h, err := f.descrAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		list, err := mcpArgsFrom(args)
		if err != nil {
			return nil, err
		}
		return nil, h.MCPSend(descr, pkg, name, list)
	})

	// MCP_REGISTER_EVENT offers the package a program uses to receive
	// events rather than messages. Emerald has no event mechanism of its
	// own yet, so it registers the package and nothing more.
	register("MCP_REGISTER_EVENT", func(f *Frame) (*Result, error) {
		maxVer, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		minVer, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		pkg, err := f.popStrArg(1)
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		minMaj, minMin := splitVersion(minVer)
		maxMaj, maxMin := splitVersion(maxVer)
		return nil, h.MCPRegister(pkg, minMaj, minMin, maxMaj, maxMin)
	})

	register("GUI_AVAILABLE", func(f *Frame) (*Result, error) {
		descr, h, err := f.descrAndHost()
		if err != nil {
			return nil, err
		}
		major, minor := h.MCPSupports(descr, guiPackage)
		return nil, f.Push(Float(version(major, minor)))
	})

	register("GUI_DLOG_CREATE", func(f *Frame) (*Result, error) {
		args, err := f.popArrayArg(4, "Window options array expected.")
		if err != nil {
			return nil, err
		}
		title, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		winType, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		descr, h, err := f.descrAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		if major, _ := h.MCPSupports(descr, guiPackage); major == 0 {
			return nil, errf("The MCP GUI package is not supported for this connection.")
		}
		if winType == "" {
			winType = "simple"
		}

		id, err := h.GUINew(descr, f)
		if err != nil {
			return nil, err
		}
		list, err := mcpArgsFrom(args)
		if err != nil {
			return nil, err
		}
		// The dialog's own fields are set last so a caller cannot
		// override them from the options dictionary.
		list = replaceArg(list, "title", title)
		list = replaceArg(list, "type", winType)
		list = replaceArg(list, "dlogid", id)
		if err := h.MCPSend(descr, guiPackage, "dlog-create", list); err != nil {
			return nil, err
		}
		return nil, f.Push(Str(id))
	})

	register("GUI_DLOG_SHOW", func(f *Frame) (*Result, error) {
		return nil, f.guiDialogCommand("dlog-show", false)
	})
	register("GUI_DLOG_CLOSE", func(f *Frame) (*Result, error) {
		return nil, f.guiDialogCommand("dlog-close", true)
	})

	register("GUI_VALUE_GET", func(f *Frame) (*Result, error) {
		ctrl, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		id, h, err := f.dialogAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		lines, _ := h.GUIValueLines(id, ctrl)
		vals := make([]Value, len(lines))
		for i, l := range lines {
			vals[i] = Str(l)
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("GUI_VALUES_GET", func(f *Frame) (*Result, error) {
		id, h, err := f.dialogAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		ctrls, values, ok := h.GUIValues(id)
		if !ok {
			return nil, errf("Invalid dialog ID.")
		}
		out := NewDict()
		for i, name := range ctrls {
			vals := make([]Value, len(values[i]))
			for j, l := range values[i] {
				vals[j] = Str(l)
			}
			out.Set(Str(name), Arr(NewList(vals)))
		}
		return nil, f.Push(Arr(out))
	})

	register("GUI_VALUE_SET", func(f *Frame) (*Result, error) {
		value, err := f.Pop()
		if err != nil {
			return nil, err
		}
		ctrl, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		id, h, err := f.dialogAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		descr, ok := h.GUIDialog(id)
		if !ok {
			return nil, errf("Invalid dialog ID.")
		}

		lines, err := valueLines(value)
		if err != nil {
			return nil, err
		}
		// Recorded here as well as sent, so a program reads back what
		// it just set rather than waiting for the client to echo it.
		h.GUISetValue(id, ctrl, lines)
		return nil, h.MCPSend(descr, guiPackage, "ctrl-value", []MCPArg{
			{Name: "dlogid", Lines: []string{id}},
			{Name: "id", Lines: []string{ctrl}},
			{Name: "value", Lines: lines},
		})
	})

	register("GUI_CTRL_CREATE", func(f *Frame) (*Result, error) {
		args, err := f.popArrayArg(4, "Dictionary of arguments expected.")
		if err != nil {
			return nil, err
		}
		ctrl, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		ctrlType, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		id, h, err := f.dialogAndHost()
		if err != nil {
			return nil, err
		}
		if err := f.checkMCPPerm(h); err != nil {
			return nil, err
		}
		descr, ok := h.GUIDialog(id)
		if !ok {
			return nil, errf("Invalid dialog ID.")
		}

		list, err := mcpArgsFrom(args)
		if err != nil {
			return nil, err
		}
		// A control's initial value is remembered locally too, for the
		// same reason GUI_VALUE_SET does it.
		if lines, ok := argLines(list, "value"); ok {
			h.GUISetValue(id, ctrl, lines)
		}
		list = replaceArg(list, "dlogid", id)
		list = replaceArg(list, "id", ctrl)
		return nil, h.MCPSend(descr, guiPackage, "ctrl-"+ctrlType, list)
	})

	// GUI_CTRL_COMMAND is the one of these upstream does not
	// permission-check: a control command can only reach a dialog that
	// already exists, and opening one needs the permission.
	register("GUI_CTRL_COMMAND", func(f *Frame) (*Result, error) {
		args, err := f.popArrayArg(4, "Dictionary of arguments expected.")
		if err != nil {
			return nil, err
		}
		command, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		ctrl, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		id, h, err := f.dialogAndHost()
		if err != nil {
			return nil, err
		}
		descr, ok := h.GUIDialog(id)
		if !ok {
			return nil, errf("Invalid dialog ID.")
		}
		list, err := mcpArgsFrom(args)
		if err != nil {
			return nil, err
		}
		list = replaceArg(list, "dlogid", id)
		list = replaceArg(list, "id", ctrl)
		return nil, h.MCPSend(descr, guiPackage, "ctrl-command-"+command, list)
	})
}

// guiPackage is the MCP package that draws dialogs, from include/mcpgui.h.
const guiPackage = "org-fuzzball-gui"

// checkMCPPerm applies upstream's CHECKMCPPERM.
//
// The floor is a @tune parameter rather than a constant, so it cannot go in
// the generated mucker-level table and is checked here instead. A program
// below the floor may still use these when the player running it owns it,
// which is what lets somebody drive their own dialogs without a mucker bit.
func (f *Frame) checkMCPPerm(h Host) error {
	if f.MLevel() >= h.MCPMinLevel() {
		return nil
	}
	if f.Prog != nil && h.Owner(f.Prog.Ref) == f.Caller {
		return nil
	}
	return errf("Permission denied!!!")
}

// guiDialogCommand sends a message naming only a dialog, and optionally
// forgets the dialog afterwards.
func (f *Frame) guiDialogCommand(name string, closing bool) error {
	id, h, err := f.dialogAndHost()
	if err != nil {
		return err
	}
	if err := f.checkMCPPerm(h); err != nil {
		return err
	}
	descr, ok := h.GUIDialog(id)
	if !ok {
		return errf("Invalid dialog ID.")
	}
	if err := h.MCPSend(descr, guiPackage, name, []MCPArg{
		{Name: "dlogid", Lines: []string{id}},
	}); err != nil {
		return err
	}
	if closing {
		h.GUIClose(id)
	}
	return nil
}

// dialogAndHost pops a dialog id and returns it with the host.
func (f *Frame) dialogAndHost() (string, Host, error) {
	id, err := f.popStr()
	if err != nil {
		return "", nil, err
	}
	if id == "" {
		return "", nil, errf("Invalid dialog ID.")
	}
	h, err := f.needHost()
	return id, h, err
}

// descrAndHost pops a descriptor number and returns it with the host.
func (f *Frame) descrAndHost() (int, Host, error) {
	n, err := f.popInt()
	if err != nil {
		return 0, nil, errf("Integer descriptor number expected. (1)")
	}
	h, err := f.needHost()
	return int(n), h, err
}

// version renders a package version the way GUI_AVAILABLE and MCP_SUPPORTS
// report one: the major number plus the minor in thousandths, so 1.3 is 1.003.
func version(major, minor int) float64 {
	return float64(major) + float64(minor)/1000.0
}

// splitVersion reads that form back.
func splitVersion(v float64) (major, minor int) {
	major = int(v)
	return major, int((v-float64(major))*1000.0 + 0.5)
}

// mcpArgsFrom turns a MUF dictionary into message arguments.
//
// A list value becomes a multi-line argument, which is how a program sends
// something like a program's source.
func mcpArgsFrom(a *Array) ([]MCPArg, error) {
	if a == nil {
		return nil, nil
	}
	var out []MCPArg
	for _, k := range a.Keys() {
		if k.Type != TypeString {
			return nil, errf("Args dictionary can only have string keys. (4)")
		}
		if k.Str == "" {
			return nil, errf("Args dictionary cannot have a null string key. (4)")
		}
		v, _ := a.Get(k)
		lines, err := valueLines(v)
		if err != nil {
			return nil, err
		}
		out = append(out, MCPArg{Name: k.Str, Lines: lines})
	}
	return out, nil
}

// valueLines renders a MUF value as the lines of an argument.
func valueLines(v Value) ([]string, error) {
	switch v.Type {
	case TypeString:
		return []string{v.Str}, nil
	case TypeInteger, TypeFloat, TypeObject:
		return []string{v.String()}, nil
	case TypeArray:
		if v.Array == nil {
			return nil, nil
		}
		if !v.Array.IsList() {
			return nil, errf("Unsupported value type in args dictionary. (4)")
		}
		out := make([]string, 0, v.Array.Len())
		for _, item := range v.Array.Values() {
			switch item.Type {
			case TypeString, TypeInteger, TypeFloat, TypeObject:
				out = append(out, item.String())
			default:
				return nil, errf("Unsupported value type in list value. (4)")
			}
		}
		return out, nil
	}
	return nil, errf("Unsupported value type in args dictionary. (4)")
}

// replaceArg sets an argument, removing any the caller supplied under the same
// name.
func replaceArg(args []MCPArg, name, value string) []MCPArg {
	kept := args[:0]
	for _, a := range args {
		if !equalName(a.Name, name) {
			kept = append(kept, a)
		}
	}
	return append(kept, MCPArg{Name: name, Lines: []string{value}})
}

// argLines finds an argument's lines.
func argLines(args []MCPArg, name string) ([]string, bool) {
	for _, a := range args {
		if equalName(a.Name, name) {
			return a.Lines, true
		}
	}
	return nil, false
}

func equalName(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
