package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// PID, ISPID?, FORCE_LEVEL, INSTANCES and SUPPLICANT are ports of
// prim_pid, prim_ispidp, prim_force_level (src/p_misc.c), prim_instances
// (src/p_db.c) and prim_supplicant (src/p_db.c). All five are unconditional —
// upstream has no mlev floor on any of them — and none needs a lock or
// object argument checked, so they stay in one file rather than following
// prim_lock.go's split.
func init() {
	register("PID", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.PID)))
	})

	register("ISPID?", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeInteger {
			return nil, errf("Non-integer argument (1).")
		}

		pid := int(v.Num)
		result := pid == f.PID
		if !result {
			h, err := f.needHost()
			if err != nil {
				return nil, err
			}
			result = h.IsPID(pid)
		}
		return nil, f.Push(Bool(result))
	})

	register("FORCE_LEVEL", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Int(int64(h.ForceLevel())))
	})

	register("INSTANCES", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		if v.Type != TypeObject || !h.Valid(v.Ref) || h.ObjType(v.Ref) != ref.TypeProgram {
			return nil, errf("Invalid program object.")
		}

		return nil, f.Push(Int(int64(h.Instances(v.Ref))))
	})

	register("SUPPLICANT", func(f *Frame) (*Result, error) {
		return nil, f.Push(Obj(f.Supplicant))
	})
}

// CANCALL? is a port of prim_cancallp (src/p_misc.c). Everything but
// argument validation — compiling prog on demand, the target's own mucker
// level, ownership/Linkable, and the public's own mlev floor — lives in
// Host.CanCall, since it needs the world and the compiler cache.
func init() {
	register("CANCALL?", func(f *Frame) (*Result, error) {
		nameV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		progV, err := f.Pop()
		if err != nil {
			return nil, err
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		if progV.Type != TypeObject || !h.Valid(progV.Ref) || h.ObjType(progV.Ref) != ref.TypeProgram {
			return nil, errf("Invalid program dbref argument. (1)")
		}
		// A MUF "" literal is upstream's NULL PROG_STRING, which this check
		// rejects the same way ParseLock's own null-vs-empty case does — see
		// mufHost.ParseLock's doc comment for the general shape of this gap.
		if nameV.Type != TypeString || nameV.Str == "" {
			return nil, errf("Invalid string argument. Must be non-null. (2)")
		}

		ok := h.CanCall(f.MLevel(), f.progUID(h), progV.Ref, nameV.Str)
		return nil, f.Push(Bool(ok))
	})
}

// KILL is a port of prim_kill (src/p_misc.c). Killing the running program's
// own pid is a special case, upstream's do_abort_silent: it is not an error
// at all, just an immediate, unreported end to the program, which is why it
// is signalled with errSilentAbort rather than an ordinary *Error.
func init() {
	register("KILL", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeInteger {
			return nil, errf("Non-integer argument (1).")
		}
		pid := int(v.Num)

		if pid == f.PID {
			return nil, errSilentAbort
		}

		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		// Capitalised "Denied" is upstream's own wording here, unlike every
		// other "Permission denied." message this codebase reproduces —
		// preserved verbatim rather than normalised.
		if f.MLevel() < 3 && !h.ControlsProcess(f.progUID(h), pid) {
			return nil, errf("Permission Denied.")
		}

		return nil, f.Push(Bool(h.KillPID(pid)))
	})
}

// FORK is a port of prim_fork (src/p_misc.c). The frame-duplicating half is
// fork() in fork.go, a pure function tested on its own; this primitive is
// just the PC adjustment fork() leaves to its caller, the child's own "0"
// marker, and forwarding to the host to register it. Unlike KILL's mlev
// check, FORK's own "if (mlev < 3) abort_interp(...)" is a genuine
// unconditional floor with no ownership escape hatch, so it is left to
// primMLevel (mlev_gen.go already records "FORK": 3) and the dispatcher's
// own generic message, the same convention every other primitive with an
// unconditional floor uses — see prim.go's primitive() — rather than
// duplicated here with upstream's own differently-worded, differently-cased
// literal, which the dispatcher's gate would pre-empt before this function
// ever ran anyway.
func init() {
	register("FORK", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		child := f.fork()
		child.PC++
		if err := child.Push(Int(0)); err != nil {
			return nil, err
		}

		pid := h.Fork(child)
		if pid == 0 {
			return nil, f.Push(Int(-1))
		}
		return nil, f.Push(Int(int64(pid)))
	})
}
