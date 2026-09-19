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
