package muf

// Connection primitives: what a program can learn about who is online.

func init() {
	register("ONLINE", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		who := h.Online()
		for _, p := range who {
			if err := f.Push(Obj(p)); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(who))))
	})
	register("ONLINE_ARRAY", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(h.Online())))
	})

	// DESCRCOUNT counts connections, which is not the same as counting
	// players: one player may be connected several times.
	register("DESCRCOUNT", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		n := 0
		for _, p := range h.Online() {
			n += h.Connections(p)
		}
		return nil, f.Push(Int(int64(n)))
	})

	register("DESCRIPTORS", func(f *Frame) (*Result, error) {
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		ds := h.Descriptors(player)
		for _, d := range ds {
			if err := f.Push(Int(int64(d))); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(ds))))
	})
	register("DESCR_ARRAY", func(f *Frame) (*Result, error) {
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		ds := h.Descriptors(player)
		vals := make([]Value, len(ds))
		for i, d := range ds {
			vals[i] = Int(int64(d))
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	// DESCR is the connection the program was started from.
	register("DESCR", func(f *Frame) (*Result, error) {
		return nil, f.Push(Int(int64(f.Descr)))
	})
	register("DESCRDBREF", func(f *Frame) (*Result, error) {
		d, err := f.popInt()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.DescrPlayer(int(d))))
	})

	register("WIDTH", descrSize(true))
	register("HEIGHT", descrSize(false))

	// Every connection is TLS here, so this is always true. The primitive
	// keeps its name because programs call it directly.
	register("DESCRSECURE?", func(f *Frame) (*Result, error) {
		if _, err := f.popInt(); err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(true))
	})
}

// descrSize builds WIDTH and HEIGHT, which report what a connection's telnet
// negotiation said about its terminal.
func descrSize(wantWidth bool) primFunc {
	return func(f *Frame) (*Result, error) {
		d, err := f.popInt()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		w, ht := h.DescrSize(int(d))
		if wantWidth {
			return nil, f.Push(Int(int64(w)))
		}
		return nil, f.Push(Int(int64(ht)))
	}
}
