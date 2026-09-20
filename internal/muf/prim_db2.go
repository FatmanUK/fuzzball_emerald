package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// COMPILE, COMPILED?, UNCOMPILE, PROGRAM_GETLINES and NEXTENTRANCE are
// ports of the more tractable primitives left in src/p_db.c.
// NEWPLAYER, COPYPLAYER, TOADPLAYER, PNAME_HISTORY, COPYOBJ, DUMP,
// PROGRAM_SETLINES and FINDNEXT are not ported this phase: the first six
// are player-lifecycle and object-cloning features needing real design
// work (creation cost accounting, an editor-interaction check for
// PROGRAM_SETLINES); FINDNEXT needs the same init_checkflags/checkflags
// flag-matching mini-language ARRAY_FILTER_FLAGS does, not yet built.
func init() {
	register("COMPILED?", func(f *Frame) (*Result, error) {
		progV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if progV.Type != TypeObject || !h.Valid(progV.Ref) || h.ObjType(progV.Ref) != ref.TypeProgram {
			return nil, errf("Invalid program object.")
		}
		return nil, f.Push(Int(int64(h.CompiledSize(progV.Ref))))
	})

	register("COMPILE", func(f *Frame) (*Result, error) {
		verboseV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		progV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if f.MLevel() < 4 {
			return nil, errf("Permission denied.  Requires Wizbit.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if progV.Type != TypeObject || !h.Valid(progV.Ref) || h.ObjType(progV.Ref) != ref.TypeProgram {
			return nil, errf("Invalid program argument. (1)")
		}
		if verboseV.Type != TypeInteger {
			return nil, errf("No boolean integer given. (2)")
		}
		if h.Instances(progV.Ref) > 0 {
			return nil, errf("That program is currently in use.")
		}
		size, err := h.Compile(progV.Ref)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Int(int64(size)))
	})

	register("UNCOMPILE", func(f *Frame) (*Result, error) {
		progV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if f.MLevel() < 4 {
			return nil, errf("Permission denied.  Requires Wizbit.")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if progV.Type != TypeObject || !h.Valid(progV.Ref) || h.ObjType(progV.Ref) != ref.TypeProgram {
			return nil, errf("Invalid program argument. (1)")
		}
		if h.Instances(progV.Ref) > 0 {
			return nil, errf("That program is currently in use.")
		}
		h.Uncompile(progV.Ref)
		return nil, nil
	})

	register("PROGRAM_GETLINES", func(f *Frame) (*Result, error) {
		endV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		startV, err := f.Pop()
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
			return nil, errf("Invalid pRogram dbref. (1)")
		}
		if startV.Type != TypeInteger {
			return nil, errf("Expected integer. (2)")
		}
		if endV.Type != TypeInteger {
			return nil, errf("Expected integer. (3)")
		}
		start, end := startV.Num, endV.Num
		if start < 0 || end < 0 {
			return nil, errf("Line indexes must be non-negative.")
		}
		if start == 0 {
			start = 1
		}
		if end != 0 && start > end {
			return nil, errf("Illogical line range.")
		}
		if f.MLevel() < 4 && !h.Controls(f.progUID(h), progV.Ref) && h.Flags(progV.Ref)&ref.Vehicle == 0 {
			return nil, errf("Permission denied.")
		}

		lines := h.ProgramLines(progV.Ref)
		lo := int(start) - 1
		hi := len(lines)
		if end != 0 && int(end) < hi {
			hi = int(end)
		}
		var vals []Value
		if lo < len(lines) && lo < hi {
			for _, l := range lines[lo:hi] {
				vals = append(vals, Str(l))
			}
		}
		return nil, f.Push(Arr(NewList(vals)))
	})

	register("NEXTENTRANCE", func(f *Frame) (*Result, error) {
		if f.MLevel() < 3 {
			return nil, errf("Permission denied.  Requires Mucker Level 3.")
		}
		startV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		linkV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		if linkV.Type != TypeObject || (!h.Valid(linkV.Ref) && linkV.Ref != ref.Nothing && linkV.Ref != ref.Home) {
			return nil, errf("Invalid link reference object (2)")
		}
		if startV.Type != TypeObject || (!h.Valid(startV.Ref) && startV.Ref != ref.Nothing) {
			return nil, errf("Invalid reference object (1)")
		}
		linkref := linkV.Ref
		if linkref == ref.Home {
			linkref = ref.Nothing
			if home := h.Links(f.Caller); len(home) > 0 {
				linkref = home[0]
			}
		}
		found := ref.Nothing
		for i := startV.Ref + 1; i < h.Top(); i++ {
			if !h.Valid(i) {
				continue
			}
			for _, l := range h.Links(i) {
				if l == linkref {
					found = i
					break
				}
			}
			if found != ref.Nothing {
				break
			}
		}
		return nil, f.Push(Obj(found))
	})
}
