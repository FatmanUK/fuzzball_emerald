package muf

// TIMER_START, TIMER_STOP and EVENT_SEND: the three primitives that put an
// event on a frame's queue from outside the instruction that asks for it.
//
// A timer is how a program waits for something with a deadline —
// EVENT_WAITFOR on both the thing it wants and the timer's own event, so
// whichever happens first wakes it. Upstream's own timed READ is compiled
// out of exactly that pair.
func init() {
	register("TIMER_START", func(f *Frame) (*Result, error) {
		idV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		delayV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if h.TimerCount(f.PID) > int(h.TuneInt("process_timer_limit")) {
			return nil, errf("Too many timers!")
		}
		if delayV.Type != TypeInteger {
			return nil, errf("Expected an integer delay time. (1)")
		}
		if idV.Type != TypeString {
			return nil, errf("Expected a string timer id. (2)")
		}
		h.TimerStart(f.PID, idV.Str, delayV.Num)
		return nil, nil
	})

	register("TIMER_STOP", func(f *Frame) (*Result, error) {
		idV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		// Upstream numbers this argument (2) despite it being the only one.
		if idV.Type != TypeString {
			return nil, errf("Expected a string timer id. (2)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		h.TimerStop(f.PID, idV.Str)
		return nil, nil
	})

	register("EVENT_SEND", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Requires Mucker level 3 or better.")
		}
		data, err := f.Pop()
		if err != nil {
			return nil, err
		}
		idV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		pidV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if pidV.Type != TypeInteger {
			return nil, errf("Expected an integer process id. (1)")
		}
		if idV.Type != TypeString {
			return nil, errf("Expected a string event id. (2)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}

		// The receiver is handed a dictionary describing where the event
		// came from, with the sent value under "data".
		env := NewDict()
		env.Set(Str("data"), deepCopy(data))
		env.Set(Str("caller_pid"), Int(int64(f.PID)))
		env.Set(Str("descr"), Int(int64(f.Descr)))
		env.Set(Str("caller_prog"), Obj(f.Prog.Ref))
		env.Set(Str("trigger"), Obj(f.Trig))
		env.Set(Str("prog_uid"), Obj(h.Owner(f.Prog.Ref)))
		env.Set(Str("player"), Obj(f.Caller))

		name := userEventName(idV.Str)
		if int(pidV.Num) == f.PID {
			// A program sending to itself never leaves the frame, so there
			// is no process to look up.
			f.AddEvent(name, Arr(env))
			return nil, nil
		}
		h.SendEvent(int(pidV.Num), name, Arr(env))
		return nil, nil
	})
}

// userEventName is upstream's "USER.%.32s": the namespace EVENT_SEND's own
// events live in, so a program cannot forge one of the server's.
func userEventName(id string) string {
	const maxEventID = 32
	if len(id) > maxEventID {
		id = id[:maxEventID]
	}
	return "USER." + id
}
