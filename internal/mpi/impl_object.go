package mpi

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
)

// The object and world introspection functions: what an object is,
// what it holds, who owns it, who is connected.
func init() {
	// {type} and {istype} are mesg_dbref_local, so a message
	// cannot ask the type of something far away -- and both spell
	// the refusal "Permission Denied." with a capital D, under
	// the name "TYPE" even from {istype}.
	register("TYPE", func(env *Env, _ *Func, args []string) (string, error) {
		obj, fail := env.resolveAs(matchLocal, args, 0)
		if fail == resolveUnknown {
			return "Bad", nil
		}
		if fail == resolveDenied {
			return "", errf("TYPE", "Permission Denied.")
		}
		return env.Host.TypeName(obj), nil
	})

	// {fullname} is {name} with the exit truncation taken out,
	// and upstream says so: mfn_fullname (mfuns2.c:694) is a
	// copy/paste of mfn_name with a TODO on top asking for
	// exactly the shared helper below. Its abort still says
	// "NAME", which is part of the copy/paste and is kept.
	register("FULLNAME", objectName(false))

	// {ref} answers a dbref for a name, and is the one function
	// that short-circuits the matcher: a literal "#123" is taken
	// as written, so a message can name an object it would not be
	// allowed to *match*. Anything else goes through
	// mesg_dbref_local, and a failed match is #-1 where a refusal
	// aborts.
	register("REF", func(env *Env, _ *Func, args []string) (string, error) {
		if p := strings.TrimSpace(args[0]); len(p) > 1 &&
			p[0] == '#' {
			if n, err := strconv.Atoi(p[1:]); err == nil {
				return "#" + itoa(n), nil
			}
		}
		obj, fail := env.resolveAs(matchLocal, args, 0)
		if fail == resolveDenied {
			return "", errf("REF", "Permission denied.")
		}
		if fail == resolveUnknown {
			obj = nothing
		}
		return "#" + itoa(int(obj)), nil
	})

	register("FLAGS", func(env *Env, _ *Func,
		args []string) (string, error) {

		obj, err := env.resolveLocal("FLAGS", args, 0)
		if err != nil {
			return "", err
		}
		return env.Host.FlagString(obj), nil
	})
	register("FLAG?", func(env *Env, _ *Func,
		args []string) (string, error) {

		// One message for both failures, and it is "Failed
		// match." rather than "Match failed." -- mfn_flagp
		// folds PERMDENIED in with the sentinels.
		obj, fail := env.resolveAs(matchLocal, args, 0)
		if fail != resolveOK {
			return "", errf("FLAG?", "Failed match. (arg1)")
		}
		return boolOf(env.Host.HasFlag(obj, args[1])), nil
	})

	register("CONTENTS", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolveLocal("CONTENTS", args, 0)
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
			if want != "" &&
				env.Host.TypeName(r) != want {
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
		obj, err := env.resolveMsg(matchRaw, "CONTROLS",
			args, 0, "Match failed. (arg1)",
			"Permission denied. (arg1)")
		if err != nil {
			return "", err
		}
		// The second argument names whose authority to test;
		// without one it is the object the message's
		// permissions come from.
		who := env.Host.Owner(env.Perms)
		if len(args) > 1 {
			other, err := env.resolveMsg(matchRaw,
				"CONTROLS", args, 1,
				"Match failed. (arg2)",
				"Permission denied. (arg2)")
			if err != nil {
				return "", err
			}
			who = env.Host.Owner(other)
		}
		return boolOf(env.Host.Controls(who, obj)), nil
	})

	// {contains} walks outwards from the object, so a thing
	// inside a thing inside a room still counts; {holds} tests
	// only the direct location.
	register("CONTAINS", func(env *Env, _ *Func, args []string) (string, error) {
		// The first argument is raw and the second local, and
		// the parenthesised indices carry no full stop before
		// them -- "Match failed (1)." -- where {locked} and
		// {testlock} write ". (arg1)". Both spellings are
		// upstream's.
		inner, err := env.resolveMsg(matchRaw, "CONTAINS",
			args, 0, "Match failed (1).",
			"Permission Denied (1).")
		if err != nil {
			return "", err
		}
		outer := env.Who
		if len(args) > 1 {
			outer, err = env.resolveMsg(matchLocal,
				"CONTAINS", args, 1,
				"Match failed (2).",
				"Permission Denied (2).")
			if err != nil {
				return "", err
			}
		}
		for i := 0; i < maxEnvDepth && inner != nothing &&
			inner != outer; i++ {
			inner = env.Host.Location(inner)
		}
		return boolOf(inner == outer), nil
	})
	register("HOLDS", func(env *Env, _ *Func, args []string) (string, error) {
		inner, err := env.resolveMsg(matchRaw, "HOLDS",
			args, 0, "Match failed (1).",
			"Permission Denied (1).")
		if err != nil {
			return "", err
		}
		outer := env.Who
		if len(args) > 1 {
			outer, err = env.resolveMsg(matchLocal,
				"HOLDS", args, 1,
				"Match failed (2).",
				"Permission Denied (2).")
			if err != nil {
				return "", err
			}
		}
		return boolOf(env.Host.Location(inner) == outer), nil
	})
	register("NEARBY", func(env *Env, _ *Func, args []string) (string, error) {
		a, err := env.resolveMsg(matchRaw, "NEARBY", args, 0,
			"Match failed (arg1).",
			"Permission denied (arg1).")
		if err != nil {
			return "", err
		}
		b := env.What
		if len(args) > 1 {
			b, err = env.resolveMsg(matchRaw, "NEARBY",
				args, 1, "Match failed (arg2).",
				"Permission denied (arg2).")
			if err != nil {
				return "", err
			}
		}
		// {nearby} resolves raw and carries its own locality
		// test instead, which this server did not have: short
		// of a blessed message, one of the two objects must
		// be a neighbour of the message's object or of the
		// reader, so a description cannot ask whether two
		// things on the far side of the world are together.
		// Two spaces after the full stop, as upstream writes
		// it.
		if !env.Blessed && !env.isNeighbor(a, env.What) &&
			!env.isNeighbor(b, env.What) &&
			!env.isNeighbor(a, env.Who) &&
			!env.isNeighbor(b, env.Who) {
			return "", errf("NEARBY",
				"Permission denied.  Neither "+
					"object is local.")
		}
		return boolOf(env.isNeighbor(a, b)), nil
	})

	register("DBEQ", func(env *Env, _ *Func, args []string) (string, error) {
		// Raw, and a refusal is reported as a failed match:
		// mfn_dbeq tests `UNKNOWN || PERMDENIED` together.
		a, err := env.resolveMsg(matchRaw, "DBEQ", args, 0,
			"Match failed (1).", "Match failed (1).")
		if err != nil {
			return "", err
		}
		b, err := env.resolveMsg(matchRaw, "DBEQ", args, 1,
			"Match failed (2).", "Match failed (2).")
		if err != nil {
			return "", err
		}
		return boolOf(a == b), nil
	})

	register("MONEY", func(env *Env, _ *Func, args []string) (string, error) {
		obj, err := env.resolve("MONEY", args, 0)
		if err != nil {
			return "", err
		}
		// The same tunable floor the MUF PENNIES primitive
		// carries, applied to MPI with blessing standing in
		// for mucker level (`mfuns2.c:1065`). Unread here, so
		// a world that raised it still had its balances
		// readable out of any description.
		if env.tuneInt("pennies_muf_mlev") > 1 &&
			!env.Blessed {
			return "", errf("MONEY", "Permission denied.")
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
		who, err := env.resolveMsg(matchLocal, "LOCKED",
			args, 0, "Match failed. (arg1)",
			"Permission denied. (arg1)")
		if err != nil {
			return "", err
		}
		obj, err := env.resolveMsg(matchLocal, "LOCKED",
			args, 1, "Match failed. (arg2)",
			"Permission denied. (arg2)")
		if err != nil {
			return "", err
		}
		return boolOf(env.Host.Locked(env.Descr, who, obj)), nil
	})

	register("TESTLOCK", func(env *Env, _ *Func, args []string) (string, error) {
		// Both objects resolve first and the **third**
		// argument is reported before the first, which is the
		// order mfn_testlock writes its aborts in
		// (`mfuns2.c:1532`): who, then its type, then obj.
		obj, objFail := env.resolveAs(matchLocal, args, 0)
		who, whoFail := env.Who, resolveOK
		if len(args) > 2 {
			who, whoFail = env.resolveAs(matchLocal,
				args, 2)
		}
		if err := failAs("TESTLOCK", whoFail,
			"Match failed. (arg3)",
			"Permission denied. (arg3)"); err != nil {
			return "", err
		}
		switch env.Host.TypeName(who) {
		case "Player", "Thing":
		default:
			return "", errf("TESTLOCK",
				"Invalid object type. (arg3)")
		}
		if err := failAs("TESTLOCK", objFail,
			"Match failed. (arg1)",
			"Permission denied. (arg1)"); err != nil {
			return "", err
		}
		// A lock is read out of a property, so the same
		// restrictions apply as to reading one directly --
		// but the property is **arg2**, and upstream says so:
		// only Prop_System is reported against arg1.
		if props.IsSystem(args[1]) {
			return "", errf("TESTLOCK",
				"Permission denied. (arg1)")
		}
		if !env.Blessed {
			priv := props.IsPrivate(args[1]) &&
				env.Host.Owner(env.Perms) !=
					env.Host.Owner(env.What)
			if props.IsHidden(args[1]) || priv {
				return "", errf("TESTLOCK",
					"Permission denied. (arg2)")
			}
		}
		// A **direct** read, not safegetprop: upstream reads
		// the lock with GETLOCK, so the explicit checks above
		// are the whole permission story and there is no
		// environment walk. Going through the safety layer
		// instead made ".priv" on an unlocked object refuse,
		// because the walk left the object and reached one
		// somebody else owned.
		lock := env.Host.GetPropStr(obj, args[1])
		if lock == "" {
			// A fourth argument is what to answer when
			// there is no lock at all, which this server
			// ignored. Without one the answer is **true**
			// -- `eval_boolexp` passes TRUE_BOOLEXP
			// (`boolexp.c:93`), so an unset lock lets
			// everybody through -- where this answered
			// "0" and made an unlocked object look shut.
			if len(args) > 3 {
				return args[3], nil
			}
			return "1", nil
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
		// An unknown parameter reads as empty rather than
		// failing, and so does one the reader may not see:
		// tune_get_parmstring answers "" in both cases.
		//
		// mfn_sysparm passes TUNE_MLEV(player) and player is
		// the *triggering* player, so an unblessed message
		// property is read at whoever looked at it. Calling
		// the ungated TuneGet here let a mortal read every
		// wizard-level parameter there is.
		name := strings.TrimSpace(args[0])
		v, _ := env.Host.TuneGetParm(name,
			env.Host.TuneMLevel(env.Who))
		return v, nil
	})
	register("PRONOUNS", func(env *Env, _ *Func, args []string) (string, error) {
		obj := env.Who
		if len(args) > 1 {
			var err error
			// "Permission Denied." with a capital D,
			// which only {pronouns} and {type} write.
			obj, err = env.resolveMsg(matchLocal,
				"PRONOUNS", args, 1, "Match failed.",
				"Permission Denied.")
			if err != nil {
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

// connTime builds {ontime} and {idle}, which report -1 rather than
// failing for an object that is not connected — or not an object at
// all. Both are mesg_dbref_raw, which cannot refuse, so the
// "Permission denied." each of them carries is unreachable -- and
// they test for it in opposite orders, which is how little it
// matters.
func connTime(read func(Host, Ref) int) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		obj, fail := env.resolveAs(matchRaw, args, 0)
		if fail != resolveOK {
			return "-1", nil
		}
		return itoa(read(env.Host, env.Host.Owner(obj))), nil
	}
}

// terminalSize builds {width} and {height}. A client that never told
// the server its size reports zero, and the optional argument is what
// to say instead — so "{width:79}" is the usual shape.
func terminalSize(read func(Host, Ref) int) impl {
	return func(env *Env, _ *Func, args []string) (string, error) {
		n := read(env.Host, env.Who)
		if n == 0 && len(args) > 0 {
			return args[0], nil
		}
		return itoa(n), nil
	}
}

// objectName is mfn_name (mfuns2.c:638) and mfn_fullname (:694),
// which differ in one line: NAME cuts an **exit's** name at the first
// ';', so a multi-alias action reports only the name it is known by.
// Nothing here truncated, so §4.2's multi-action -- whose whole
// point is one action with several names -- reported the whole alias
// list. Upstream's own comment on mfn_fullname says it is a
// copy/paste of mfn_name with that line removed, and asks for exactly
// this helper; its abort still says "NAME", which is part of the
// copy/paste and is kept.
//
// **Upstream's three sentinel branches are dead code.** Both
// functions test for NOTHING, AMBIGUOUS and HOME and answer
// "#NOTHING#", "#AMBIGUOUS#" and "#HOME#" -- but mesg_dbref_raw ends
// with `if (!OkObj(obj)) obj = UNKNOWN;` and OkObj requires `d >= 0`
// (`db.h:440`), so all three have already become UNKNOWN and "Match
// failed." is the only answer any of them can give. FULLNAME used to
// return "#NOTHING#" here, which is the one upstream cannot produce.
func objectName(truncateExit bool) impl {
	return func(env *Env, _ *Func, args []string) (string,
		error) {

		obj, fail := env.resolveAs(matchRaw, args, 0)
		if fail != resolveOK {
			return "", errf("NAME", "Match failed.")
		}
		name := env.Host.Name(obj)
		// Asked through TypeName rather than a new Host
		// method: {type} already needs the same answer, and
		// upstream's word for an exit is "Exit".
		if truncateExit && env.Host.TypeName(obj) == "Exit" {
			if i := strings.IndexByte(name, ';'); i >= 0 {
				name = name[:i]
			}
		}
		return name, nil
	}
}
