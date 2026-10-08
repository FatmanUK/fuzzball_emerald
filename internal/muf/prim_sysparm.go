package muf

// SYSPARM, SETSYSPARM and SYSPARM_ARRAY are ports of prim_sysparm/
// prim_setsysparm/prim_sysparm_array (src/p_misc.c), all built on
// Host.TuneGet/TuneSet/TuneList (internal/game/tune_host.go). All
// three check a parameter's own read/write mlevel against
// TUNE_MLEV(player) — the PLAYER's own object mlevel, not the
// calling program's — which is why none of the three appear in
// mlev_gen.go: their gate is a per-parameter value, not a fixed floor
// on the primitive itself.
func init() {
	register("SYSPARM", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// TUNE_MLEV, not MLevel. All three primitives here
		// use it -- p_misc.c:1217, :1322 and :1413 -- and so
		// do do_tune, MPI's {sysparm} and the MCP simpleedit
		// handler. God gets 255, which is what puts the ten
		// parameters marked MLEV_GOD on their *read* level --
		// smtp_password among them -- beyond a plain wizard.
		playerMLevel := h.TuneMLevel(f.Caller)
		if name == "" {
			return nil, f.Push(Str(""))
		}
		readMLev, ok := h.TuneReadMLevel(name)
		if !ok || playerMLevel < readMLev {
			return nil, f.Push(Str(""))
		}
		value, _ := h.TuneGet(name)
		return nil, f.Push(Str(value))
	})

	register("SETSYSPARM", func(f *Frame) (*Result, error) {
		valueV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		nameV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		if f.MLevel() < 4 {
			return nil, errf("Wizbit only primitive.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if h.ForceLevel() > 0 {
			return nil, errf("Cannot be forced.")
		}
		if nameV.Type != TypeString || nameV.Str == "" {
			return nil, errf("Invalid string argument. Must be non-null. (2)")
		}
		if valueV.Type != TypeString {
			return nil, errf("Invalid argument. (2)")
		}

		// tune_setparm does the lookup, the permission test
		// and the validation itself, and has a distinct
		// result for each failure (p_misc.c's own switch).
		// This used to do the first two by hand and then call
		// a *lax* setter -- the loader's, which accepts
		// values tune_setparm refuses -- and report every
		// failure as a bad value.
		switch h.TuneSet(nameV.Str, valueV.Str,
			h.TuneMLevel(f.Caller)) {
		case TuneSetUnknown:
			return nil, errf("Unknown parameter. (1)")
		case TuneSetSyntax:
			return nil, errf("Bad parameter syntax. (2)")
		case TuneSetBadVal:
			return nil, errf("Bad parameter value. (2)")
		case TuneSetDenied:
			return nil, errf("Permission denied. (1)")
		}
		return nil, nil
	})

	register("SYSPARM_ARRAY", func(f *Frame) (*Result, error) {
		pattern, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		playerMLevel := h.TuneMLevel(f.Caller)

		entries := h.TuneList(pattern, playerMLevel)
		vals := make([]Value, len(entries))
		for i, e := range entries {
			d := NewDict()
			d.Set(Str("group"), Str(e.Group))
			d.Set(Str("name"), Str(e.Name))
			d.Set(Str("mlev"), Int(int64(e.ReadMLev)))
			d.Set(Str("readmlev"), Int(int64(e.ReadMLev)))
			d.Set(Str("writemlev"), Int(int64(e.WriteMLev)))
			d.Set(Str("label"), Str(e.Label))
			d.Set(Str("nullable"), Bool(e.Nullable))
			d.Set(Str("active"), Bool(e.Active))
			d.Set(Str("default"), Bool(e.Default))
			d.Set(Str("type"), Str(e.Type))
			switch e.Type {
			case "string":
				d.Set(Str("value"), Str(e.ValueStr))
			case "timespan", "integer":
				d.Set(Str("value"), Int(e.ValueNum))
			case "dbref":
				d.Set(Str("objtype"), Str(e.ObjType))
				d.Set(Str("value"), Obj(e.ValueRef))
			case "boolean":
				d.Set(Str("value"), Bool(e.ValueBool))
			}
			vals[i] = Arr(d)
		}
		return nil, f.Push(Arr(NewList(vals)))
	})
}

// trimTuneReset strips SETSYSPARM's own "%name" reset-to-default
// prefix, so the write-mlev check looks up the real parameter name
// either way.
func trimTuneReset(name string) string {
	if len(name) > 0 && name[0] == '%' {
		return name[1:]
	}
	return name
}
