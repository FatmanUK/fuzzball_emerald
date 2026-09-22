package mpi

import "strings"

// The object and world introspection functions: what an object is, what it
// holds, who owns it, who is connected.
func init() {
	register("TYPE", func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.lookup(args[0])
		if !env.Host.Valid(obj) {
			return "Bad", nil
		}
		return env.Host.TypeName(obj), nil
	})

	// {name} renders a player as "*Name"; {fullname} always gives the
	// stored name, which for a player includes nothing extra but for
	// anything else is the same string.
	register("FULLNAME", func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.lookup(args[0])
		if !env.Host.Valid(obj) {
			return "#NOTHING#", nil
		}
		return env.Host.Name(obj), nil
	})

	register("REF", func(env *Env, _ *Func, args []string) (string, error) {
		return "#" + itoa(int(env.lookup(args[0]))), nil
	})

	register("FLAGS", objectText(func(env *Env, obj Ref) string {
		return env.Host.FlagString(obj)
	}))
	register("FLAG?", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("FLAG?", args, 0)
		if err != nil {
			return "", errf("FLAG?", "Failed match. (arg1)")
		}
		return boolOf(env.Host.HasFlag(obj, args[1])), nil
	})

	register("CONTENTS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("CONTENTS", args, 0)
		if err != nil {
			return "", err
		}
		want := ""
		if len(args) > 1 {
			switch strings.ToLower(strings.TrimSpace(args[1])) {
			case "room":
				want = "Room"
			case "player":
				want = "Player"
			case "program":
				want = "Program"
			case "thing":
				want = "Thing"
			case "exit":
				return "", errf("CONTENTS", "Use {exits:obj} for a list of exits.")
			default:
				return "", errf("CONTENTS",
					"Type must be 'player', 'room', 'thing', or 'program'. (arg2)")
			}
		}
		var out []Ref
		for _, r := range env.Host.Contents(obj) {
			if want != "" && env.Host.TypeName(r) != want {
				continue
			}
			out = append(out, r)
		}
		return env.renderList(out), nil
	})

	register("EXITS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("EXITS", args, 0)
		if err != nil {
			return "", err
		}
		return env.renderList(env.Host.Exits(obj)), nil
	})

	register("LINKS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("LINKS", args, 0)
		if err != nil {
			return "", err
		}
		return env.renderList(env.Host.Links(obj)), nil
	})

	register("CONTROLS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("CONTROLS", args, 0)
		if err != nil {
			return "", errf("CONTROLS", "Match failed. (arg1)")
		}
		// The second argument names whose authority to test; without one
		// it is the object the message's permissions come from.
		who := env.Host.Owner(env.Perms)
		if len(args) > 1 {
			other, err := env.resolve("CONTROLS", args, 1)
			if err != nil {
				return "", errf("CONTROLS", "Match failed. (arg2)")
			}
			who = env.Host.Owner(other)
		}
		return boolOf(env.Host.Controls(who, obj)), nil
	})

	// {contains} walks outwards from the object, so a thing inside a thing
	// inside a room still counts; {holds} tests only the direct location.
	register("CONTAINS", func(env *Env, _ *Func, args []string) (string, error) {
		inner, err := env.resolve("CONTAINS", args, 0)
		if err != nil {
			return "", errf("CONTAINS", "Match failed (1).")
		}
		outer := env.Who
		if len(args) > 1 {
			if outer, err = env.resolve("CONTAINS", args, 1); err != nil {
				return "", errf("CONTAINS", "Match failed (2).")
			}
		}
		for i := 0; i < maxEnvDepth && inner != nothing && inner != outer; i++ {
			inner = env.Host.Location(inner)
		}
		return boolOf(inner == outer), nil
	})
	register("HOLDS", func(env *Env, _ *Func, args []string) (string, error) {
		inner, err := env.resolve("HOLDS", args, 0)
		if err != nil {
			return "", errf("HOLDS", "Match failed (1).")
		}
		outer := env.Who
		if len(args) > 1 {
			if outer, err = env.resolve("HOLDS", args, 1); err != nil {
				return "", errf("HOLDS", "Match failed (2).")
			}
		}
		return boolOf(env.Host.Location(inner) == outer), nil
	})
	register("NEARBY", func(env *Env, _ *Func, args []string) (string, error) {
		a, err := env.resolve("NEARBY", args, 0)
		if err != nil {
			return "", errf("NEARBY", "Match failed (arg1).")
		}
		b := env.What
		if len(args) > 1 {
			if b, err = env.resolve("NEARBY", args, 1); err != nil {
				return "", errf("NEARBY", "Match failed (arg2).")
			}
		}
		return boolOf(neighbours(env, a, b)), nil
	})

	register("DBEQ", func(env *Env, _ *Func, args []string) (string, error) {
		a, b := env.lookup(args[0]), env.lookup(args[1])
		if !env.Host.Valid(a) {
			return "", errf("DBEQ", "Match failed (1).")
		}
		if !env.Host.Valid(b) {
			return "", errf("DBEQ", "Match failed (2).")
		}
		return boolOf(a == b), nil
	})

	register("MONEY", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("MONEY", args, 0)
		if err != nil {
			return "", err
		}
		switch env.Host.TypeName(obj) {
		case "Thing", "Player":
			return itoa(env.Host.Value(obj)), nil
		}
		return "0", nil
	})

	register("CREATED", timestamp(func(c, m, u int64, n int) int64 { return c }))
	register("MODIFIED", timestamp(func(c, m, u int64, n int) int64 { return m }))
	register("LASTUSED", timestamp(func(c, m, u int64, n int) int64 { return u }))
	register("USECOUNT", timestamp(func(c, m, u int64, n int) int64 { return int64(n) }))

	register("LOCKED", func(env *Env, _ *Func, args []string) (string, error) {
		who, err := env.resolve("LOCKED", args, 0)
		if err != nil {
			return "", errf("LOCKED", "Match failed. (arg1)")
		}
		obj, err := env.resolve("LOCKED", args, 1)
		if err != nil {
			return "", errf("LOCKED", "Match failed. (arg2)")
		}
		return boolOf(env.Host.Locked(env.Descr, who, obj)), nil
	})

	register("TESTLOCK", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("TESTLOCK", args, 0)
		if err != nil {
			return "", errf("TESTLOCK", "Match failed. (arg1)")
		}
		who := env.Who
		if len(args) > 2 {
			if who, err = env.resolve("TESTLOCK", args, 2); err != nil {
				return "", errf("TESTLOCK", "Match failed. (arg3)")
			}
		}
		// A lock is read out of a property, so the same restrictions apply
		// as to reading one directly: system properties never, and hidden
		// ones only for a blessed message.
		if hasPropPrefix(args[1], "@__sys__") ||
			(!env.Blessed && propSegmentStarts(args[1], '@')) {
			return "", errf("TESTLOCK", "Permission denied. (arg1)")
		}
		lock := env.getProp(obj, args[1])
		if lock == "" {
			return "0", nil
		}
		ok, err := env.Host.TestLock(env.Descr, who, obj, lock)
		if err != nil {
			return "", errf("TESTLOCK", "%s", err.Error())
		}
		return boolOf(ok), nil
	})

	// Connections.
	register("ONLINE", func(env *Env, _ *Func, _ []string) (string, error) {
		if !env.Blessed {
			return "", errf("ONLINE", "Permission denied.")
		}
		return env.renderList(env.Host.OnlinePlayers()), nil
	})
	register("ONTIME", connTime(func(h Host, obj Ref) int { return h.OnTime(obj) }))
	register("IDLE", connTime(func(h Host, obj Ref) int { return h.Idle(obj) }))

	register("DESCR", func(env *Env, _ *Func, _ []string) (string, error) {
		return itoa(env.Descr), nil
	})
	register("WIDTH", terminalSize(func(h Host, obj Ref) int { return h.Width(obj) }))
	register("HEIGHT", terminalSize(func(h Host, obj Ref) int { return h.Height(obj) }))

	register("MUCKNAME", func(env *Env, _ *Func, _ []string) (string, error) {
		return env.Host.MuckName(), nil
	})
	register("SYSPARM", func(env *Env, _ *Func, args []string) (string, error) {
		// An unknown parameter reads as empty rather than failing, which is
		// what tune_get_parmstring gives for one the reader may not see.
		v, _ := env.Host.TuneGet(strings.TrimSpace(args[0]))
		return v, nil
	})
	register("PRONOUNS", func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.Who
		if len(args) > 1 {
			var err error
			if obj, err = env.resolve("PRONOUNS", args, 1); err != nil {
				return "", err
			}
		}
		return env.Host.PronounSub(obj, args[0]), nil
	})
}

// renderList writes a list of objects the way MPI does, one per line.
func (env *Env) renderList(objs []Ref) string {
	if len(objs) > maxListLen {
		objs = objs[:maxListLen]
	}
	out := make([]string, len(objs))
	for i, r := range objs {
		out[i] = env.render(r)
	}
	return strings.Join(out, "\r")
}

// timestamp builds the four functions that read an object's clocks.
func timestamp(pick func(created, modified, used int64, count int) int64) impl {
	return func(env *Env, f *Func, args []string) (string, error) {
		obj, err := env.resolve(f.Name, args, 0)
		if err != nil {
			return "", err
		}
		c, m, u, n := env.Host.Timestamps(obj)
		return itoa(int(pick(c, m, u, n))), nil
	}
}

// connTime builds {ontime} and {idle}, which report -1 rather than failing
// for an object that is not connected — or not an object at all.
func connTime(read func(Host, Ref) int) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.lookup(args[0])
		if !env.Host.Valid(obj) {
			return "-1", nil
		}
		return itoa(read(env.Host, env.Host.Owner(obj))), nil
	}
}

// terminalSize builds {width} and {height}. A client that never told the
// server its size reports zero, and the optional argument is what to say
// instead — so "{width:79}" is the usual shape.
func terminalSize(read func(Host, Ref) int) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		n := read(env.Host, env.Who)
		if n == 0 && len(args) > 0 {
			return args[0], nil
		}
		return itoa(n), nil
	}
}

// neighbours reports whether two objects can see each other: in the same
// room, or one inside the other.
func neighbours(env *Env, a, b Ref) bool {
	if a == b {
		return true
	}
	locA, locB := env.Host.Location(a), env.Host.Location(b)
	return locA == b || locB == a || (locA == locB && locA != nothing)
}
