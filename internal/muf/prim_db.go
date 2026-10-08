package muf

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ansi"
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Database and property primitives.

func init() {
	register("NAME", refToStr(func(h Host, r ref.Ref) string { return h.Name(r) }))
	// SETNAME was gated at mucker 4 by the generated table, which
	// was wrong: prim_setname's rule is "(mlev < 4) &&
	// !permissions(ProgUID, ref)" — a wizard **or** whoever has
	// permissions on the object — so a mortal could not rename
	// an object they owned, and the refusal said "Permission
	// denied. Requires Wizbit." where upstream says a bare
	// "Permission denied."
	//
	// The false floor came from a *second*, bare "if (mlev < 4)"
	// nested inside "if (Typeof(ref) == TYPE_PLAYER)": the
	// extractor sees the inner condition without its enclosing
	// one. That inner check is live, but only for the one case
	// the outer test lets a mortal through — a program renaming
	// its own owner, where permissions() answers 1 because "thing
	// == player". SETNAME is in the generator's CONDITIONAL_FLOOR
	// set now and checks for itself here.
	//
	// The **order** matters too: both type tests come before the
	// permission one, so a mortal handed a non-string name is
	// told about the argument rather than about permission.
	register("SETNAME", func(f *Frame) (*Result, error) {
		nameVal, err := f.Pop()
		if err != nil {
			return nil, err
		}
		objVal, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// valid_object (interp.c:2672): a dbref that exists
		// and is not garbage.
		if objVal.Type != TypeObject ||
			!h.Valid(objVal.Ref) {
			return nil, errf("Invalid argument type (1)")
		}
		if nameVal.Type != TypeString {
			return nil, errf("Non-string argument (2)")
		}
		obj, name := objVal.Ref, nameVal.Str

		if f.MLevel() < 4 &&
			!f.permissions(h, f.progUID(h), obj) {
			return nil, errf("Permission denied.")
		}

		if h.ObjType(obj) == ref.TypePlayer {
			// Reached by a mortal only when renaming its
			// own owner, which permissions() allows and
			// this refuses.
			if f.MLevel() < 4 {
				return nil, errf("Permission denied.")
			}
			// A player's new name carries the password
			// after it, so the name is the first word and
			// everything past the whitespace is the
			// credential.
			newName, pass := splitNameAndPassword(name)
			if pass == "" {
				return nil, errf("%s", pwNeeded)
			}
			if !h.CheckPassword(obj, pass) {
				return nil, errf("%s", pwWrong)
			}
			if !ascii.EqualFold(newName, h.Name(obj)) &&
				!h.NameOK(newName, ref.TypePlayer) {
				return nil, errf("You can't give a " +
					"player that name.")
			}
			name = newName
		} else if !h.NameOK(name, h.ObjType(obj)) {
			return nil, errf("Invalid name.")
		}

		if err := h.SetName(obj, name); err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, nil
	})

	register("LOCATION", refToRef(func(h Host, r ref.Ref) ref.Ref { return h.Location(r) }))
	register("OWNER", refToRef(func(h Host, r ref.Ref) ref.Ref { return h.Owner(r) }))
	register("GETLINK", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		links := h.Links(obj)
		if len(links) == 0 {
			return nil, f.Push(Obj(ref.Nothing))
		}
		return nil, f.Push(Obj(links[0]))
	})
	register("GETLINKS", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		links := h.Links(obj)
		for _, l := range links {
			if err := f.Push(Obj(l)); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(len(links))))
	})
	register("GETLINKS_ARRAY", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(h.Links(obj))))
	})

	register("CONTENTS", chainPrim(func(h Host, r ref.Ref) []ref.Ref { return h.Contents(r) }))
	register("EXITS", chainPrim(func(h Host, r ref.Ref) []ref.Ref { return h.Exits(r) }))
	register("CONTENTS_ARRAY", chainArray(func(h Host, r ref.Ref) []ref.Ref { return h.Contents(r) }))
	register("EXITS_ARRAY", chainArray(func(h Host, r ref.Ref) []ref.Ref { return h.Exits(r) }))

	// NEXT walks a containment chain one step, which is how older
	// programs iterate before arrays existed.
	register("NEXT", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		loc := h.Location(obj)
		if !h.Valid(loc) {
			return nil, f.Push(Obj(ref.Nothing))
		}
		siblings := h.Contents(loc)
		if h.ObjType(obj) == ref.TypeExit {
			siblings = h.Exits(loc)
		}
		for i, s := range siblings {
			if s == obj && i+1 < len(siblings) {
				return nil, f.Push(Obj(siblings[i+1]))
			}
		}
		return nil, f.Push(Obj(ref.Nothing))
	})

	register("MOVETO", func(f *Frame) (*Result, error) {
		dest, err := f.popRef()
		if err != nil {
			return nil, err
		}
		victim, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, moveTo(f, h, victim, dest)
	})

	register("DBTOP", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.Top()))
	})
	register("OK?", func(f *Frame) (*Result, error) {
		r, err := f.popRef()
		if err != nil {
			return nil, err
		}
		if f.host == nil {
			return nil, f.Push(Bool(false))
		}
		return nil, f.Push(Bool(f.host.Valid(r)))
	})

	register("PLAYER?", typeOfTest(ref.TypePlayer))
	register("ROOM?", typeOfTest(ref.TypeRoom))
	register("EXIT?", typeOfTest(ref.TypeExit))
	register("PROGRAM?", typeOfTest(ref.TypeProgram))
	register("THING?", typeOfTest(ref.TypeThing))

	register("FLAG?", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(h.Flags(obj).HasNamed(name)))
	})
	register("SET", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		clear := len(name) > 0 && name[0] == '!'
		if clear {
			name = name[1:]
		}
		bit, ok := ref.FlagNamed(name)
		if !ok {
			return nil, errf("unknown flag %q", name)
		}
		flags := h.Flags(obj)
		if clear {
			flags &^= bit
		} else {
			flags |= bit
		}
		h.SetFlags(obj, flags)
		return nil, nil
	})

	register("CONTROLS", func(f *Frame) (*Result, error) {
		obj, err := f.popRef()
		if err != nil {
			return nil, err
		}
		who, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(controls(h, who, obj)))
	})

	// The three matching primitives strip ANSI from the name
	// before matching (p_db.c:765, :812, :886) and are the only
	// primitives that do. A program that has been handed a
	// coloured name — one built by another program, or read out
	// of a property — can still resolve it, which is the point:
	// the escape sequences are not part of what anything is
	// called. Note it is strip_ansi and not ANSI_STRIP's narrower
	// pattern; see internal/ansi.
	register("MATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.Match(f.Caller,
			ansi.Strip(name))))
	})
	register("PMATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		name = ansi.Strip(name)
		return nil, f.Push(Obj(h.MatchPlayer(name)))
	})

	// Property access. MUF distinguishes the three forms by what
	// they convert the stored value to, rather than by what was
	// stored.
	register("GETPROPSTR", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type == props.Int || v.Type == props.Float ||
			v.Type == props.Ref {
			// Only a string property reads as a string;
			// the others read as empty, as upstream's
			// get_property_class does.
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(v.Str))
	})
	register("GETPROPVAL", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type != props.Int {
			return nil, f.Push(Int(0))
		}
		return nil, f.Push(Int(v.Num))
	})
	register("GETPROPFVAL", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		if v.Type != props.Float {
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(v.Float))
	})
	register("GETPROP", func(f *Frame) (*Result, error) {
		v, err := f.getProp()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(fromProp(v))
	})

	register("SETPROP", func(f *Frame) (*Result, error) {
		val, err := f.Pop()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		h.SetProp(obj, path, toProp(val))
		return nil, nil
	})
	register("ADDPROP", func(f *Frame) (*Result, error) {
		// "obj path strval intval addprop": a non-empty
		// string wins, otherwise the integer is stored.
		num, err := f.popInt()
		if err != nil {
			return nil, err
		}
		str, err := f.popStr()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		if str != "" {
			h.SetProp(obj, path, props.Value{Type: props.String, Str: str})
		} else {
			h.SetProp(obj, path, props.Value{Type: props.Int, Num: num})
		}
		return nil, nil
	})
	register("REMOVE_PROP", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		h.RemoveProp(obj, path)
		return nil, nil
	})
	register("PROPDIR?", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(len(h.PropChildren(obj, path)) > 0))
	})
	register("NEXTPROP", func(f *Frame) (*Result, error) {
		// "obj path nextprop": the next name at the same
		// level.
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(nextProp(h, obj, path)))
	})
}

// needHost returns the host, or an error naming the problem.
func (f *Frame) needHost() (Host, error) {
	if f.host == nil {
		return nil, errf("this primitive needs a running server")
	}
	return f.host, nil
}

// hostOrNil returns the host without insisting on one, for a
// primitive that only needs it down some of its paths — FMTSTRING's
// "%D" wants a name, and every other directive it can be handed does
// not.
func (f *Frame) hostOrNil() Host { return f.host }

// refAndHost pops a dbref and returns it with the host.
func (f *Frame) refAndHost() (ref.Ref, Host, error) {
	r, err := f.popRef()
	if err != nil {
		return ref.Nothing, nil, err
	}
	h, err := f.needHost()
	return r, h, err
}

// propTarget pops a property path and the object it is on.
func (f *Frame) propTarget() (string, ref.Ref, Host, error) {
	path, err := f.popStr()
	if err != nil {
		return "", ref.Nothing, nil, err
	}
	obj, h, err := f.refAndHost()
	return path, obj, h, err
}

// getProp reads a property, yielding the zero value when unset.
func (f *Frame) getProp() (props.Value, error) {
	path, obj, h, err := f.propTarget()
	if err != nil {
		return props.Value{}, err
	}
	v, ok := h.GetProp(obj, path)
	if !ok {
		return props.Value{}, nil
	}
	return v, nil
}

// fromProp converts a stored property to a stack value.
func fromProp(v props.Value) Value {
	switch v.Type {
	case props.String, props.Lock:
		return Str(v.Str)
	case props.Int:
		return Int(v.Num)
	case props.Float:
		return Float(v.Float)
	case props.Ref:
		return Obj(v.Ref)
	}
	return Str("")
}

// toProp converts a stack value to a stored property.
func toProp(v Value) props.Value {
	switch v.Type {
	case TypeInteger:
		return props.Value{Type: props.Int, Num: v.Num}
	case TypeFloat:
		return props.Value{Type: props.Float, Float: v.Float}
	case TypeObject:
		return props.Value{Type: props.Ref, Ref: v.Ref}
	default:
		return props.Value{Type: props.String, Str: v.String()}
	}
}

// nextProp returns the property name after path at the same level, or
// "" at the end. An empty path starts the walk at the top.
func nextProp(h Host, obj ref.Ref, path string) string {
	parent, name := splitProp(path)
	children := h.PropChildren(obj, parent)
	if name == "" {
		if len(children) == 0 {
			return ""
		}
		return join(parent, children[0])
	}
	for i, c := range children {
		if equalFoldASCII(c, name) && i+1 < len(children) {
			return join(parent, children[i+1])
		}
	}
	return ""
}

// splitProp separates a property path into its directory and last
// name.
func splitProp(path string) (dir, name string) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}

// join puts a property path back together.
func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// refList builds a MUF array of dbrefs.
func refList(refs []ref.Ref) *Array {
	vals := make([]Value, len(refs))
	for i, r := range refs {
		vals[i] = Obj(r)
	}
	return NewList(vals)
}

// chainPrim builds a primitive that pushes a chain's head, the way
// the older CONTENTS and EXITS do.
func chainPrim(fn func(Host, ref.Ref) []ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		chain := fn(h, obj)
		if len(chain) == 0 {
			return nil, f.Push(Obj(ref.Nothing))
		}
		return nil, f.Push(Obj(chain[0]))
	}
}

// chainArray builds the array form of the same.
func chainArray(fn func(Host, ref.Ref) []ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(fn(h, obj))))
	}
}

// typeOfTest builds a primitive that reports an object's type.
func typeOfTest(want ref.ObjType) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(h.Valid(obj) && h.ObjType(obj) == want))
	}
}

// refToStr builds a primitive that asks the host about an object.
func refToStr(fn func(Host, ref.Ref) string) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(fn(h, obj)))
	}
}

// refToRef builds a primitive that resolves one object to another.
func refToRef(fn func(Host, ref.Ref) ref.Ref) primFunc {
	return func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(fn(h, obj)))
	}
}

// controls reports whether one object may modify another, which is
// ownership or an unquelled wizard bit.
func controls(h Host, who, what ref.Ref) bool {
	if !h.Valid(who) || !h.Valid(what) {
		return false
	}
	if h.Flags(who).IsWizard() {
		return true
	}
	owner := who
	if h.ObjType(who) != ref.TypePlayer {
		owner = h.Owner(who)
	}
	return who == what || h.Owner(what) == owner
}

// Environment-walking property lookups, reflists, and the remaining
// object queries.

func init() {
	// ENVPROP walks out through the environment tree until it
	// finds the property, which is how a world puts a default on
	// a parent room.
	register("ENVPROP", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		where, v := envProp(h, obj, path)
		if err := f.Push(Obj(where)); err != nil {
			return nil, err
		}
		return nil, f.Push(fromProp(v))
	})
	register("ENVPROPSTR", func(f *Frame) (*Result, error) {
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		where, v := envProp(h, obj, path)
		if err := f.Push(Obj(where)); err != nil {
			return nil, err
		}
		if v.Type == props.Int || v.Type == props.Float ||
			v.Type == props.Ref {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(v.Str))
	})

	// A reflist is a property holding space-separated dbrefs,
	// which older programs use where an array would serve today.
	register("REFLIST_FIND", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		list := readRefList(h, obj, path)
		for i, r := range list {
			if r == target {
				// One-based, as the primitive reports
				// it.
				return nil, f.Push(Int(int64(i + 1)))
			}
		}
		return nil, f.Push(Int(0))
	})
	register("REFLIST_ADD", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		list := readRefList(h, obj, path)
		for _, r := range list {
			if r == target {
				return nil, nil // already there
			}
		}
		writeRefList(h, obj, path, append(list, target))
		return nil, nil
	})
	register("REFLIST_DEL", func(f *Frame) (*Result, error) {
		target, err := f.popRef()
		if err != nil {
			return nil, err
		}
		path, obj, h, err := f.propTarget()
		if err != nil {
			return nil, err
		}
		v, _ := h.GetProp(obj, path)
		h.SetProp(obj, path, props.Value{
			Type: props.String,
			Str:  spliceRef(v.Str, target),
		})
		return nil, nil
	})

	register("UNPARSEOBJ", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(obj) {
			return nil, f.Push(Str("*NOTHING*"))
		}
		return nil, f.Push(Str(h.Name(obj) + "(" + obj.String() +
			h.Flags(obj).Unparse() + ")"))
	})

	register("PARSEPROP", func(f *Frame) (*Result, error) {
		// "object propname arg flags parseprop"
		//
		// The floor is checked here rather than left to the
		// generated table because the wording is its own:
		// upstream spells the requirement out instead of
		// saying "Permission denied."
		if f.MLevel() < 3 {
			return nil, errf("Mucker level 3 or greater required.")
		}
		flags, err := f.popInt()
		if err != nil {
			return nil, err
		}
		arg, err := f.popStr()
		if err != nil {
			return nil, errf("String expected. (3)")
		}
		path, err := f.popStr()
		if err != nil {
			return nil, errf("String expected. (2)")
		}
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeObject {
			return nil, errf("Non-object argument. (1)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(v.Ref) {
			return nil, errf("Invalid object. (1)")
		}
		// A non-zero flag marks the evaluation private, which
		// stops it producing messages to anyone but the
		// caller.
		out, err := h.ParseProp(v.Ref, path, arg, flags != 0)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Str(out))
	})

	register("PENNIES", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// p_db.c:379. A tunable floor, so the mucker table
		// cannot hold it; the default is 1, which is why
		// nothing noticed it was unread.
		if err := tunableFloor(f, h, "pennies_muf_mlev",
			penniesDenied); err != nil {
			return nil, err
		}
		if !isMoneyHolder(h, obj) {
			return nil, errf("%s", badMoneyArg)
		}
		v, _ := h.GetProp(obj, propValue)
		return nil, f.Push(Int(v.Num))
	})
	register("ADDPENNIES", func(f *Frame) (*Result, error) {
		amount, err := f.popInt()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// p_db.c:74 onwards. The table had an invented floor
		// of 4 here, taken from the mlev<4 block below, which
		// is arithmetic rather than permission.
		if err := tunableFloor(f, h, "addpennies_muf_mlev",
			addPenniesDenied); err != nil {
			return nil, err
		}
		if !isMoneyHolder(h, obj) {
			return nil, errf("%s", badMoneyArg)
		}
		// Level 4 is needed for a *thing*, not in general: a
		// type test, not a floor (p_db.c:88).
		if f.MLevel() < 4 &&
			h.ObjType(obj) == ref.TypeThing {
			return nil, errf("Permission denied.")
		}
		v, _ := h.GetProp(obj, propValue)
		if f.MLevel() < 4 {
			if err := penniesRange(h, v.Num,
				amount); err != nil {
				return nil, err
			}
		}
		h.SetProp(obj, propValue, props.Value{Type: props.Int, Num: v.Num + amount})
		return nil, nil
	})
}

// The three money primitives' refusals. Upstream's wording is oddly
// literal and inconsistently capitalised -- "Permission Denied" for
// addpennies and pennies against "Permission denied" for movepennies
// -- and programs match on it, so it is reproduced rather than
// tidied. prim_misc2.go's USERLOG is the same shape.
const (
	penniesDenied = "Permission Denied " +
		"(mlev < tp_pennies_muf_mlev)"
	addPenniesDenied = "Permission Denied " +
		"(mlev < tp_addpennies_muf_mlev)"
	movePenniesDenied = "Permission denied " +
		"(mlev < tp_movepennies_muf_mlev)"
	badMoneyArg = "Invalid player or thing argument."
)

// tunableFloor refuses when the frame is below a floor named by an
// @tune parameter.
//
// A map[string]int cannot express a gate a world can move at runtime,
// so these five live in the primitives rather than in mlev_gen.go --
// and gen_mlev.py now names them in TUNABLE_FLOOR so that dropping
// one is a generator failure rather than a silent hole, which is how
// all three of these came to have no gate at all.
func tunableFloor(f *Frame, h Host, parm, msg string) error {
	if f.MLevel() < int(h.TuneInt(parm)) {
		return errf("%s", msg)
	}
	return nil
}

// isMoneyHolder is the "player or thing" test all three money
// primitives make on their object argument.
func isMoneyHolder(h Host, obj ref.Ref) bool {
	t := h.ObjType(obj)
	return h.Valid(obj) &&
		(t == ref.TypePlayer || t == ref.TypeThing)
}

// penniesRange is ADDPENNIES's four refusals below mucker 4
// (p_db.c:96 onwards): the signed overflow tests upstream writes as
// comparisons that only make sense on a wrapping int, plus the
// max_pennies ceiling and the floor at zero.
func penniesRange(h Host, have, add int64) error {
	if add > 0 {
		if have > have+add {
			return errf("Would roll over player's score.")
		}
		if have+add > h.TuneInt("max_pennies") {
			return errf("Would exceed MAX_PENNIES.")
		}
		return nil
	}
	if have < have+add {
		return errf("Would roll over player's score.")
	}
	if have+add < 0 {
		return errf("Result would be negative.")
	}
	return nil
}

// moveTo is prim_moveto (p_db.c:150), and it is a type switch rather
// than a move.
//
// This server used to be a bare h.MoveTo(victim, dest) -- the raw
// store move, which refuses only self-containment -- with a floor of
// 3 in the generated mucker table standing in for all of it. The
// floor was not even real: its "if ((mlev < 3))" at p_db.c:234 opens
// a block of extra mortal-only restrictions rather than refusing. So
// every M1 and M2 program was refused outright, and every M3 and M4
// one got an unvalidated move: no enter_room for a player, so no
// announcement, no autolook and no arrive propqueue; no loop check;
// no exit re-sourcing; no room reparenting.
//
// Thirteen of its tests are conditional on mucker level and none of
// them is a floor. The structure is upstream's, including two
// fall-throughs that are load-bearing: a PLAYER falls into the THING
// case for the loop check and the mortal-only block, then leaves
// through enter_room before the PROGRAM case; and a THING falls into
// the PROGRAM case for the matchroom rule and the move itself.
func moveTo(f *Frame, h Host, victim, dest ref.Ref) error {
	if f.Level > h.MaxInterpRecursion() {
		return errf("Interp call loops not allowed.")
	}
	if !h.Valid(victim) {
		return errf("Non-object argument. (2)")
	}
	// HOME is a legal destination and not an object.
	if !h.Valid(dest) && dest != ref.Home {
		return errf("Non-object argument. (1)")
	}
	if h.ObjType(dest) == ref.TypeExit {
		return errf("Destination argument is an exit.")
	}
	mlev := f.MLevel()
	uid := f.progUID(h)

	// Two conditional tests before the switch. Neither is a
	// floor: the first is a *type* test and the second an
	// ownership escape hatch, which is why gen_mlev.py skips both
	// and why the block at :234 was the one it misread.
	if h.ObjType(victim) == ref.TypeExit && mlev < 3 {
		return errf("Permission denied.")
	}
	if h.Flags(victim)&ref.JumpOK == 0 &&
		!f.permissions(h, uid, victim) && mlev < 3 {
		return errf("Object can't be moved.")
	}

	switch h.ObjType(victim) {
	case ref.TypePlayer, ref.TypeThing:
		if err := moveToCreature(f, h, victim, dest, mlev,
			uid); err != nil {
			return err
		}
		if h.ObjType(victim) == ref.TypePlayer {
			h.EnterRoom(f.Descr, victim, dest, f.Prog.Ref)
			return nil
		}
		h.LastUsed(victim)
		return moveToContained(f, h, victim, dest, mlev, uid)

	case ref.TypeProgram:
		return moveToContained(f, h, victim, dest, mlev, uid)

	case ref.TypeExit:
		return moveToExit(f, h, victim, dest, mlev, uid)

	case ref.TypeRoom:
		return moveToRoom(f, h, victim, dest, mlev, uid)
	}
	return nil
}

// moveToCreature is the PLAYER and THING head of the switch: the
// destination test a player gets, the loop check both get, and the
// block of mortal-only restrictions that the false floor was taken
// from.
func moveToCreature(f *Frame, h Host, victim, dest ref.Ref,
	mlev int, uid ref.Ref) error {

	if h.ObjType(victim) == ref.TypePlayer {
		if h.ObjType(dest) != ref.TypeRoom &&
			!(h.ObjType(dest) == ref.TypeThing &&
				h.Flags(dest)&ref.Vehicle != 0) {
			return errf("Bad destination.")
		}
	}
	if h.ParentLoopCheck(victim, dest) {
		return errf("Things can't contain themselves.")
	}
	if mlev >= 3 {
		return nil
	}
	// p_db.c:234. Four refusals, each with its own sentence, and
	// every one of them unreachable while the table carried a
	// floor of 3.
	if h.Flags(h.Location(victim))&ref.JumpOK == 0 &&
		!f.permissions(h, uid, h.Location(victim)) {
		return errf("Source not JUMP_OK.")
	}
	if dest != ref.Home && h.Flags(dest)&ref.JumpOK == 0 &&
		!f.permissions(h, uid, dest) {
		return errf("Destination not JUMP_OK.")
	}
	if h.ObjType(dest) == ref.TypeThing &&
		h.Location(victim) != h.Location(dest) {
		return errf("Not in same location as vehicle.")
	}
	if h.Flags(victim)&ref.Guest != 0 &&
		h.Flags(dest)&ref.Guest != 0 &&
		h.ObjType(dest) == ref.TypeRoom {
		return errf("Destination doesn't accept guests.")
	}
	return nil
}

// moveToContained is the THING tail and the PROGRAM case, which a
// thing reaches by falling through.
//
// The vehicle and zombie refusals are a thing's alone -- a player has
// already left through enter_room -- and both read oddly:
// "(FLAGS(dest) & VEHICLE) && Typeof(dest) != TYPE_THING" can only
// hold for a *room* flagged VEHICLE, which is how upstream spells "a
// vehicle room". Reproduced as written.
func moveToContained(f *Frame, h Host, victim, dest ref.Ref,
	mlev int, uid ref.Ref) error {

	if h.ObjType(victim) == ref.TypeThing {
		if mlev < 3 && h.Flags(victim)&ref.Vehicle != 0 &&
			h.Flags(dest)&ref.Vehicle != 0 &&
			h.ObjType(dest) != ref.TypeThing {
			return errf("Destination doesn't accept " +
				"vehicles.")
		}
		if mlev < 3 && h.Flags(victim)&ref.Zombie != 0 &&
			h.Flags(dest)&ref.Zombie != 0 &&
			h.ObjType(dest) != ref.TypeThing {
			return errf("Destination doesn't accept " +
				"zombies.")
		}
	}
	t := h.ObjType(dest)
	if t != ref.TypeRoom && t != ref.TypePlayer &&
		t != ref.TypeThing {
		return errf("Bad destination.")
	}
	if mlev < 3 {
		// matchroom is the *last* of the two tests that
		// holds, not the first: upstream assigns twice
		// without an else, so the victim's location wins when
		// both are controlled.
		matchroom := ref.Nothing
		if f.permissions(h, uid, dest) {
			matchroom = dest
		}
		if f.permissions(h, uid, h.Location(victim)) {
			matchroom = h.Location(victim)
		}
		if matchroom != ref.Nothing &&
			h.Flags(matchroom)&ref.JumpOK == 0 &&
			!f.permissions(h, uid, victim) {
			return errf("Permission denied.")
		}
	}
	// A thing moves noisily when the world has asked for it or
	// when it is a puppet; everything else is the silent move.
	if h.ObjType(victim) == ref.TypeThing &&
		(h.TuneBool("secure_thing_movement") ||
			h.Flags(victim)&ref.Zombie != 0) {
		h.EnterRoom(f.Descr, victim, dest, f.Prog.Ref)
		return nil
	}
	if err := h.MoveTo(victim, dest); err != nil {
		return errf("%s", err.Error())
	}
	return nil
}

// moveToExit re-sources an exit, which is the one branch that is not
// a move at all: unset_source then set_source, which World.MoveTo
// already is for an exit because chainHead picks the Exits list.
//
// SetMLevel(victim, 0) goes with it. An exit's mucker bits are its
// *priority*, so re-pointing one resets how hard it competes -- which
// is the same reset @unlink reports as "Action priority Level reset
// to 0."
func moveToExit(f *Frame, h Host, victim, dest ref.Ref,
	mlev int, uid ref.Ref) error {

	if mlev < 3 && (!f.permissions(h, uid, victim) ||
		!f.permissions(h, uid, dest)) {
		return errf("Permission denied.")
	}
	t := h.ObjType(dest)
	if (t != ref.TypeRoom && t != ref.TypeThing &&
		t != ref.TypePlayer) || dest == ref.Home {
		return errf("Bad destination object.")
	}
	if err := h.MoveTo(victim, dest); err != nil {
		return errf("%s", err.Error())
	}
	h.SetFlags(victim, h.Flags(victim).SetMLevel(0))
	return nil
}

// moveToRoom reparents a room, and its two permission rules differ:
// reparenting to #0 wants control of the room **or** of its parent,
// where reparenting anywhere else wants control of the room **and**
// somewhere it can link to.
//
// The first test is upstream's and is not about mucker level at all:
// without secure_thing_movement a room may only be reparented to
// another room, and with it anywhere the later tests allow.
func moveToRoom(f *Frame, h Host, victim, dest ref.Ref,
	mlev int, uid ref.Ref) error {

	if !h.TuneBool("secure_thing_movement") &&
		h.ObjType(dest) != ref.TypeRoom {
		return errf("Bad destination.")
	}
	if victim == ref.GlobalEnvironment {
		return errf("Permission denied.")
	}
	if dest == ref.Home {
		if mlev < 3 && !f.permissions(h, uid, victim) &&
			!f.permissions(h, uid, h.Location(victim)) {
			return errf("Permission denied.")
		}
		dest = ref.GlobalEnvironment
	} else {
		if mlev < 3 && (!f.permissions(h, uid, victim) ||
			!h.CanTeleportTo(f.Descr, uid, dest)) {
			return errf("Permission denied.")
		}
		if h.ParentLoopCheck(victim, dest) {
			return errf("Parent room would create a " +
				"loop.")
		}
	}
	h.LastUsed(victim)
	if err := h.MoveTo(victim, dest); err != nil {
		return errf("%s", err.Error())
	}
	return nil
}

// setownMortalRules is the block SETOWN's "if (mlev < 4)" opens
// (p_db.c:1891). It is not a floor: a mortal program may chown an
// object marked CHOWN_OK to its own owner, and the table's recorded 4
// forbade exactly the case the flag exists for.
//
// Four refusals, each with its own sentence, and the order is
// upstream's: the new owner must be the caller, the object must be
// CHOWN_OK *and* pass its @chlock, a room must be stood in, and a
// thing must be carried.
func setownMortalRules(f *Frame, h Host, obj, owner ref.Ref) error {
	if owner != f.Caller {
		return errf("Permission denied. (2)")
	}
	if h.Flags(obj)&ref.ChownOK == 0 ||
		!h.ChownLockPasses(f.Descr, f.Caller, obj) {
		return errf("Permission denied. (1)")
	}
	if h.ObjType(obj) == ref.TypeRoom &&
		h.Location(f.Caller) != obj {
		return errf("Permission denied: not in room. (1)")
	}
	if h.ObjType(obj) == ref.TypeThing &&
		h.Location(obj) != f.Caller {
		return errf("Permission denied: object not " +
			"carried. (1)")
	}
	return nil
}

// movePenniesRange is MOVEPENNIES's four refusals below mucker 4
// (p_db.c:2459 onwards). They are the same shape as ADDPENNIES's but
// each carries an argument number, and the pair is checked in one
// direction only: the amount is already known to be non-negative.
func movePenniesRange(h Host, from, to, amount int64) error {
	if from < from-amount {
		return errf("Would roll over player's score. (1)")
	}
	if from-amount < 0 {
		return errf("Result would be negative. (1)")
	}
	if to > to+amount {
		return errf("Would roll over player's score. (2)")
	}
	if to+amount > h.TuneInt("max_pennies") {
		return errf("Would exceed MAX_PENNIES. (2)")
	}
	return nil
}

// propValue is where an object's currency is kept, from include/db.h.
const propValue = "@/value"

// envProp looks a property up on an object and then on each of its
// containers in turn, returning where it was found.
func envProp(h Host, obj ref.Ref, path string) (ref.Ref, props.Value) {
	// Bounded, so a cycle in a damaged environment tree
	// terminates.
	for i := 0; obj != ref.Nothing && i <= maxEnvDepth; i++ {
		if v, ok := h.GetProp(obj, path); ok {
			return obj, v
		}
		obj = h.Location(obj)
	}
	return ref.Nothing, props.Value{}
}

// maxEnvDepth bounds an environment walk.
const maxEnvDepth = 256

// readRefList parses a space-separated dbref property.
func readRefList(h Host, obj ref.Ref, path string) []ref.Ref {
	v, _ := h.GetProp(obj, path)
	var out []ref.Ref
	for _, field := range strings.Fields(v.Str) {
		r, err := ref.Parse(field)
		if err == nil {
			out = append(out, r)
		}
	}
	return out
}

// spliceRef removes one dbref from a reflist by cutting it out of the
// text.
//
// Upstream does the same, which leaves the separator that preceded
// the removed entry: deleting #1 from "#1 #0" gives " #0", not "#0".
// That leading space is observable, so it is reproduced rather than
// tidied away.
func spliceRef(list string, target ref.Ref) string {
	want := target.String()
	for i := 0; i < len(list); i++ {
		if list[i] != '#' {
			continue
		}
		end := i + len(want)
		if end > len(list) || list[i:end] != want {
			continue
		}
		// The match must end at a separator or the end of the
		// list, so "#1" does not match inside "#12".
		if end < len(list) && list[end] != ' ' {
			continue
		}
		return list[:i] + list[end:]
	}
	return list
}

// writeRefList stores a reflist, removing the property when it
// empties.
func writeRefList(h Host, obj ref.Ref, path string, list []ref.Ref) {
	if len(list) == 0 {
		h.RemoveProp(obj, path)
		return
	}
	parts := make([]string, len(list))
	for i, r := range list {
		parts[i] = r.String()
	}
	h.SetProp(obj, path, props.Value{Type: props.String, Str: strings.Join(parts, " ")})
}

// Object creation, ownership, links and locks.

func init() {
	register("NEWOBJECT", create(ref.TypeThing))
	register("NEWROOM", create(ref.TypeRoom))
	register("NEWEXIT", create(ref.TypeExit))
	// NEWPROGRAM is prim_newprogram (p_db.c:3077). Three things
	// about it differ from the NEWOBJECT family beside it.
	//
	// Its type error is "Expected string argument." with **no**
	// argument index — the only one of the four uses of that
	// wording upstream that carries none (p_db.c:3090 against
	// :2521, :2525 and p_strings.c:774), so popStrArg's
	// "Non-string argument (N)" is the wrong helper here.
	//
	// It creates as **ProgUID**, not as the caller. The shared
	// create helper below passes f.Caller, which is a divergence
	// of its own and is not copied: who a program acts as is
	// find_uid's answer, and it differs from the caller whenever
	// the program is STICKY, HAVEN, SetUID or HardUID — which
	// is most launch sites.
	//
	// And it has no CHECKOFLOW and does not touch
	// fr->already_created, so a mucker-4 program may make as many
	// programs in one run as it likes. The limiter the NEWOBJECT
	// family carries is deliberately absent.
	//
	// The mucker-4 gate is the dispatcher's: mlev_gen.go records
	// the floor and prim.go emits "Permission denied. Requires
	// Wizbit." before the body runs.
	register("NEWPROGRAM", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if v.Type != TypeString {
			return nil, errf("Expected string argument.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		r, err := h.CreateProgram(f.progUID(h), v.Str)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Obj(r))
	})

	register("RECYCLE", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// p_db.c:2200 is
		//   (mlev < 3) || ((mlev < 4) && !permissions(...))
		// -- two independent disjuncts. The generator's skip
		// regex matched "permissions(" anywhere in the
		// condition and discarded the whole thing, so the
		// unconditional floor of 3 was lost and this
		// primitive had no level check at all: a mucker-1
		// program could recycle anything. The 3 is back in
		// the table (gen_mlev.py splits on top-level "||"
		// now); the second disjunct has to be here, because
		// it needs the argument.
		if f.MLevel() < 4 &&
			!f.permissions(h, f.progUID(h), obj) {
			return nil, errf("Permission denied.")
		}
		// Five refusals follow the permission test
		// (p_db.c:2206-2232), and until now this primitive
		// made **none** of them: it went straight to
		// World.Recycle, which guards only nil-and-garbage.
		// So a mucker-4 program could recycle the global
		// environment and turn the whole world into garbage,
		// or recycle a player -- both of which @recycle, the
		// command, refuses. The last tranche recorded these
		// as "still missing" without noticing that one of
		// them is catastrophic.
		if obj == ref.GlobalEnvironment {
			return nil, errf("Cannot recycle the " +
				"global environment.")
		}
		if h.ObjType(obj) == ref.TypePlayer {
			return nil, errf("Cannot recycle a player.")
		}
		// Recycling what a dbref parameter names would leave
		// the server pointing at garbage. do_recycle makes
		// the same test and words it differently.
		if h.TuneNamesObject(obj) {
			return nil, errf("Cannot currently recycle " +
				"that object.")
		}
		if obj == f.Prog.Ref {
			return nil, errf("Cannot recycle currently " +
				"running program.")
		}
		// The fifth, "Cannot recycle active program.", is
		// **not reproducible**. Upstream walks fr->caller,
		// whose entries are the program dbrefs execution has
		// passed through (interp.c:690-692); Emerald's
		// f.calls is {pc, scopeBase} -- return addresses
		// within one program, with no program refs on it at
		// all. The check above covers what it would for any
		// run that is not nested through INTERP, because
		// upstream's own stack holds the running program at
		// index 1; what is lost is a program recycling one
		// further out in an INTERP chain. Recorded in
		// docs/upstream-coverage.md with the ProgUID case
		// that has the same cause.
		if err := h.Recycle(obj); err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, nil
	})

	register("SETOWN", func(f *Frame) (*Result, error) {
		owner, err := f.popRef()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// p_db.c:1880 onwards. The table recorded a floor of
		// 4 from the "if (mlev < 4)" below, which opens a
		// block of mortal-only restrictions rather than
		// refusing -- so a mortal program could not chown
		// anything, including an object its owner had marked
		// CHOWN_OK for exactly that purpose.
		if !h.Valid(obj) ||
			h.ObjType(obj) == ref.TypePlayer {
			return nil, errf("Invalid argument (1)")
		}
		if !h.Valid(owner) ||
			h.ObjType(owner) != ref.TypePlayer {
			return nil, errf("Invalid argument (2)")
		}
		if f.MLevel() < 4 {
			if err := setownMortalRules(f, h, obj,
				owner); err != nil {
				return nil, err
			}
		}
		// OWNER(oper1) rather than oper1: for a player those
		// are the same object, and valid_player above is what
		// makes that so.
		h.SetOwner(obj, owner)
		return nil, nil
	})

	register("SETLINK", func(f *Frame) (*Result, error) {
		dest, err := f.popRef()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// SETLINK needs a real object; there is no unlinking
		// through it.
		if !h.Valid(dest) {
			return nil, errf("Invalid object. (2)")
		}
		h.SetLinks(obj, []ref.Ref{dest})
		return nil, nil
	})
	register("SETLINKS_ARRAY", func(f *Frame) (*Result, error) {
		a, err := f.popArray()
		if err != nil {
			return nil, err
		}
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		var dests []ref.Ref
		for _, v := range a.Values() {
			if v.Type != TypeObject {
				return nil, errf("Argument not an array of dbrefs.")
			}
			dests = append(dests, v.Ref)
		}
		h.SetLinks(obj, dests)
		return nil, nil
	})

	register("MLEVEL", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// #-1 asks about the running program rather than an
		// object.
		if obj == ref.Nothing {
			return nil, f.Push(Int(int64(f.MLevel())))
		}
		if !h.Valid(obj) {
			return nil, errf("Invalid object.")
		}
		return nil, f.Push(Int(int64(h.Flags(obj).RawMLevel())))
	})

	register("TIMESTAMPS", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		created, modified, used, count := h.Timestamps(obj)
		// Created, last used, modified, then the use count.
		for _, t := range []int64{created, used, modified} {
			if err := f.Push(Int(t)); err != nil {
				return nil, err
			}
		}
		return nil, f.Push(Int(int64(count)))
	})

	register("MOVEPENNIES", func(f *Frame) (*Result, error) {
		amount, err := f.popInt()
		if err != nil {
			return nil, err
		}
		to, err := f.popRef()
		if err != nil {
			return nil, err
		}
		from, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// p_db.c:2433. Another tunable floor, default 2,
		// where the table had an invented 4.
		if err := tunableFloor(f, h, "movepennies_muf_mlev",
			movePenniesDenied); err != nil {
			return nil, err
		}
		// Both object arguments are tested
		//   Typeof(x) != TYPE_PLAYER
		//       || Typeof(x) == TYPE_THING
		// which is upstream's and is odd twice over: the
		// second disjunct cannot hold when the first does
		// not, so it is dead, and the effect is that
		// MOVEPENNIES takes *players only* despite every
		// message saying "player or thing". That in turn
		// makes the "mlev < 4 && Typeof == THING" check below
		// it unreachable, and the "Typeof(ref) ==
		// TYPE_PLAYER"
		// guard around the range tests always true. All three
		// are reproduced as written rather than simplified,
		// because what a program sees is the refusal.
		if !h.Valid(from) ||
			h.ObjType(from) != ref.TypePlayer {
			return nil, errf("Invalid player or thing " +
				"argument (1)")
		}
		if !h.Valid(to) || h.ObjType(to) != ref.TypePlayer {
			return nil, errf("Invalid player or thing " +
				"argument (2)")
		}
		if amount < 0 {
			return nil, errf("Argument must be a " +
				"non-negative integer. (3)")
		}
		if f.MLevel() < 4 &&
			h.ObjType(from) == ref.TypeThing {
			return nil, errf("Permission denied. (2)")
		}
		fromVal, _ := h.GetProp(from, propValue)
		toVal, _ := h.GetProp(to, propValue)
		if f.MLevel() < 4 &&
			h.ObjType(from) == ref.TypePlayer {
			if err := movePenniesRange(h, fromVal.Num,
				toVal.Num, amount); err != nil {
				return nil, err
			}
		}
		h.SetProp(from, propValue, props.Value{Type: props.Int, Num: fromVal.Num - amount})
		h.SetProp(to, propValue, props.Value{Type: props.Int, Num: toVal.Num + amount})
		return nil, nil
	})

	register("PART_PMATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(h.MatchPlayerPrefix(name)))
	})

	register("RMATCH", func(f *Frame) (*Result, error) {
		name, err := f.popStr()
		if err != nil {
			return nil, err
		}
		around, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		// A remote match looks only at what the given object
		// holds, and strips ANSI from the name first as the
		// other two matching primitives do.
		name = ansi.Strip(name)
		for _, r := range append(h.Contents(around), h.Exits(around)...) {
			if ascii.EqualFold(h.Name(r), name) {
				return nil, f.Push(Obj(r))
			}
		}
		return nil, f.Push(Obj(ref.Nothing))
	})

	register("CHECKPASSWORD", func(f *Frame) (*Result, error) {
		pass, err := f.popStr()
		if err != nil {
			return nil, err
		}
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(h.CheckPassword(player, pass)))
	})
	register("NEWPASSWORD", func(f *Frame) (*Result, error) {
		pass, err := f.popStr()
		if err != nil {
			return nil, err
		}
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if err := h.SetPassword(player, pass); err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, nil
	})

	register("ENTRANCES_ARRAY", func(f *Frame) (*Result, error) {
		target, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Arr(refList(h.Entrances(target))))
	})

	register("NEXTOWNED", func(f *Frame) (*Result, error) {
		obj, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		owner := h.Owner(obj)
		for r := obj + 1; r < h.Top(); r++ {
			if h.Valid(r) && h.Owner(r) == owner {
				return nil, f.Push(Obj(r))
			}
		}
		return nil, f.Push(Obj(ref.Nothing))
	})

	register("OBJMEM", func(f *Frame) (*Result, error) {
		// A byte count of an object's storage, which means
		// nothing here: the shape is Go's, not the C's, so
		// any number would be fiction. Report zero, which
		// upstream also does for an object with nothing
		// loaded.
		if _, _, err := f.refAndHost(); err != nil {
			return nil, err
		}
		return nil, f.Push(Int(0))
	})
}

// create builds the NEWOBJECT family, which take a parent and a name.
func create(t ref.ObjType) primFunc {
	return func(f *Frame) (*Result, error) {
		name, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		parent, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if t == ref.TypeThing &&
			!validNewObjectParent(h, parent) {
			return nil, errf("Invalid player or room object (1)")
		}
		if !h.Valid(parent) {
			return nil, errf("Invalid object (1)")
		}
		r, err := h.Create(t, name, parent, f.Caller)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Obj(r))
	}
}

// validNewObjectParent reproduces NEWOBJECT's check on where a thing
// may be created, which is not what its error message says.
//
// The C reads:
//
//	Typeof(x) != TYPE_PLAYER && Typeof(x) == TYPE_ROOM
//
// so it rejects rooms and accepts anything else, when the "!=" in the
// second half was plainly meant. The message still says "player or
// room".
//
// This is reproduced rather than corrected. A program written against
// the real server works here; a program written against a corrected
// version would fail there, and a world author testing on Emerald
// would not find out until they deployed.
func validNewObjectParent(h Host, parent ref.Ref) bool {
	if !h.Valid(parent) {
		return false
	}
	t := h.ObjType(parent)
	return !(t != ref.TypePlayer && t == ref.TypeRoom)
}

// equalFoldASCII compares two names the way property lookup does.
func equalFoldASCII(a, b string) bool { return ascii.EqualFold(a, b) }

// prim_setname's two password refusals, named because they do not fit
// on the line that uses them at this indentation.
const (
	pwNeeded = "Player namechange requires password."
	pwWrong  = "Incorrect password."
)

// permissions is interp.c:2706's permissions(), which is **not**
// controls(): it has no wizard escape at all, and it answers false
// for a player who is not the asker.
//
//   - the object itself, or HOME, is always permitted
//   - a PLAYER never is, unless it *is* the asker
//   - an EXIT is, when the owners match or it has no owner
//   - a ROOM, THING or PROGRAM is, when the owners match
//   - anything else, garbage included, is not
//
// Several primitives pair it with "mlev < 4" to mean "a wizard or the
// owner", which is the shape the mucker-level generator has to skip
// rather than read as a floor.
func (f *Frame) permissions(h Host, who, thing ref.Ref) bool {
	if thing == who || thing == ref.Home {
		return true
	}
	switch h.ObjType(thing) {
	case ref.TypePlayer:
		return false
	case ref.TypeExit:
		owner := h.Owner(thing)
		return owner == h.Owner(who) || owner == ref.Nothing
	case ref.TypeRoom, ref.TypeThing, ref.TypeProgram:
		return h.Owner(thing) == h.Owner(who)
	}
	return false
}

// splitNameAndPassword cuts a player's new name from the password
// after it, which is how prim_setname reads its argument: the name is
// the leading run of non-space characters and the credential is what
// follows the whitespace.
func splitNameAndPassword(s string) (name, pass string) {
	i := 0
	for i < len(s) && !isSpaceByte(s[i]) {
		i++
	}
	name = s[:i]
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	return name, s[i:]
}
