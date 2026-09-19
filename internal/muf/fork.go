package muf

// fork builds an independent copy of f for FORK, upstream's frame-duplicating
// half of prim_fork. It is a pure function — no process queue, no pid, no
// world — so it can be tested without either; wiring the copy into the
// scheduler is FORK's own job in prim_proc.go.
//
// Every array value is deep-copied, not merely re-referenced, the way
// upstream's deep_copyinst is used here (and nowhere else a plain DUP would
// use copyinst's cheaper link-count bump instead): the two frames must run
// on independent state from this point on, and Go's arrays are ordinary
// shared *Array pointers between Values until something decouples them —
// see deepCopy's own doc, used by DEEP_COPY for the same reason.
//
// PC is copied unadvanced, matching the instruction FORK is still on; the
// caller is responsible for moving it past FORK, the way prim_fork's own
// "tmpfr->pc = pc; tmpfr->pc++;" does two steps rather than one. Instructions
// starts fresh at zero — upstream's calloc'd tmpfr never inherits a
// instruction count either. PID is left zero: it is assigned once the host
// registers the copy as a process, the same as any other frame's.
func (f *Frame) fork() *Frame {
	child := &Frame{
		Prog:       f.Prog,
		PC:         f.PC,
		Stack:      deepCopyValues(f.Stack),
		Vars:       deepCopyValues(f.Vars),
		LVars:      deepCopyValues(f.LVars),
		scopes:     make([][]Value, len(f.scopes)),
		calls:      append([]callSite(nil), f.calls...),
		fors:       make([]forLoop, len(f.fors)),
		trys:       append([]tryBlock(nil), f.trys...),
		ErrorFlags: f.ErrorFlags,
		Caller:     f.Caller,
		Trig:       f.Trig,
		Descr:      f.Descr,
		Level:      f.Level,
		Supplicant: f.Supplicant,
		// A forked frame is backgrounded from birth, upstream's
		// tmpfr->multitask = BACKGROUND.
		Mode: ModeBackground,
		host: f.host,
	}
	for i, s := range f.scopes {
		child.scopes[i] = deepCopyValues(s)
	}
	for i, loop := range f.fors {
		child.fors[i] = forLoop{
			keys:     deepCopyValues(loop.keys),
			vals:     deepCopyValues(loop.vals),
			idx:      loop.idx,
			cur:      loop.cur,
			end:      loop.end,
			step:     loop.step,
			counting: loop.counting,
		}
	}
	return child
}

// deepCopyValues deep-copies every element of a Value slice, preserving nil
// vs. empty so a copied nil slice field stays nil rather than becoming a
// zero-length one.
func deepCopyValues(vs []Value) []Value {
	if vs == nil {
		return nil
	}
	out := make([]Value, len(vs))
	for i, v := range vs {
		out[i] = deepCopy(v)
	}
	return out
}
