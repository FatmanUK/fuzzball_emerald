package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// The player- and database-management half of src/p_db.c: NEWPLAYER,
// COPYPLAYER, TOADPLAYER, PNAME_HISTORY, COPYOBJ, PROGRAM_SETLINES
// and DUMP.
//
// Every one of these is mlev 4 with upstream's own generic "Requires
// Wizbit." wording, so mlev_gen.go's generated floor gates them
// before these run and none needs an inline check — except
// TOADPLAYER, whose refusal to run from inside a @force is its own
// and has no generated equivalent.
func init() {
	register("NEWPLAYER", func(f *Frame) (*Result, error) {
		pass, err := f.popStrArg(1)
		if err != nil {
			return nil, err
		}
		name, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		r, err := h.NewPlayer(name, pass)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Obj(r))
	})

	register("COPYPLAYER", func(f *Frame) (*Result, error) {
		pass, err := f.popStrArg(3)
		if err != nil {
			return nil, err
		}
		name, err := f.popStrArg(2)
		if err != nil {
			return nil, err
		}
		src, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !isPlayer(h, src) {
			return nil, errf("Player dbref expected. (1)")
		}
		r, err := h.CopyPlayer(src, name, pass)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Obj(r))
	})

	register("TOADPLAYER", func(f *Frame) (*Result, error) {
		victim, err := f.popRef()
		if err != nil {
			return nil, err
		}
		recipient, err := f.popRef()
		if err != nil {
			return nil, err
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		// Unlike every other wizard primitive here, this one
		// refuses to run from inside a @force at all: a
		// wizard tricked into forcing a program should not be
		// able to delete a player by doing so.
		if h.ForceLevel() > 0 {
			return nil, errf("Cannot be forced.")
		}
		if !isPlayer(h, victim) {
			return nil, errf("Player dbref expected for player to be toaded (2)")
		}
		if !isPlayer(h, recipient) {
			return nil, errf("Player dbref expected for recipient (1)")
		}
		if victim == recipient {
			return nil, errf("Victim and recipient must be different players.")
		}
		if v, ok := h.GetProp(victim, noRecycleProp); ok &&
			!v.IsEmpty() {
			return nil, errf("That player is precious.")
		}
		if victim == ref.God {
			return nil, errf("God may not be toaded. (2)")
		}
		if h.Flags(victim).IsTrueWizard() {
			return nil, errf("You can't toad a wizard.")
		}
		if h.TuneRefersTo(victim) {
			return nil, errf("That player cannot currently be @toaded.")
		}
		h.ToadPlayer(victim, recipient)
		return nil, nil
	})

	register("PNAME_HISTORY", func(f *Frame) (*Result, error) {
		player, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !isPlayer(h, player) {
			return nil, errf("Non-player argument (1).")
		}
		d := NewDict()
		// The history is recorded whatever this parameter
		// says; the parameter only decides whether a program
		// may read it back.
		if h.TuneBool("pname_history_reporting") {
			for _, name := range h.PropChildren(player, pnameHistoryDir) {
				v, ok := h.GetProp(player, pnameHistoryDir+"/"+name)
				if !ok {
					continue
				}
				d.Set(Str(name), Str(v.StringValue()))
			}
		}
		return nil, f.Push(Arr(d))
	})

	register("COPYOBJ", func(f *Frame) (*Result, error) {
		src, h, err := f.refAndHost()
		if err != nil {
			return nil, err
		}
		if !h.Valid(src) {
			return nil, errf("Invalid object.")
		}
		if f.MLevel() < 3 && f.alreadyCreated > 0 {
			return nil, errf("An object was already created this program run.")
		}
		if h.ObjType(src) != ref.TypeThing {
			return nil, errf("Invalid object type.")
		}
		if f.MLevel() < 3 &&
			h.Owner(src) != h.Owner(f.Prog.Ref) {
			return nil, errf("Permission denied.")
		}
		f.alreadyCreated++
		// Only a wizard's copy carries the source's hidden
		// properties over.
		r, err := h.CopyObject(src, f.MLevel() == 4)
		if err != nil {
			return nil, errf("%s", err.Error())
		}
		return nil, f.Push(Obj(r))
	})

	register("PROGRAM_SETLINES", func(f *Frame) (*Result, error) {
		linesV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		progV, err := f.Pop()
		if err != nil {
			return nil, err
		}
		if progV.Type != TypeObject {
			return nil, errf("Non-object argument. (1)")
		}
		if linesV.Type != TypeArray {
			return nil, errf("Non-array argument. (2)")
		}
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		prog := progV.Ref
		if !h.Valid(prog) ||
			h.ObjType(prog) != ref.TypeProgram {
			return nil, errf("Invalid program object. (1)")
		}
		if !linesV.Array.IsList() {
			return nil, errf("Array list type required. (2)")
		}
		lines := make([]string, 0, linesV.Array.Len())
		for _, v := range linesV.Array.Values() {
			if v.Type != TypeString {
				return nil, errf("Argument not an array of strings. (2)")
			}
			// An empty line is stored as a single space,
			// so a saved program never has a line that
			// reads as end-of-text.
			if v.Str == "" {
				lines = append(lines, " ")
				continue
			}
			lines = append(lines, v.Str)
		}
		if !h.Controls(h.Owner(f.Prog.Ref), prog) {
			return nil, errf("Permission denied.")
		}
		if h.Flags(prog).Has(ref.Internal) {
			return nil, errf("Program already being edited.")
		}
		h.SetProgramLines(prog, lines)
		return nil, nil
	})

	register("DUMP", func(f *Frame) (*Result, error) {
		h, err := f.needHost()
		if err != nil {
			return nil, err
		}
		h.DumpNow()
		// Upstream reports 0 when a dump is already running
		// and this one was skipped. Emerald's flush is a
		// background write that never blocks the world, so
		// there is nothing to collide with and no reason to
		// refuse: this is always the "started" answer.
		return nil, f.Push(Int(1))
	})
}

// isPlayer is upstream's valid_player.
func isPlayer(h Host, r ref.Ref) bool {
	return h.Valid(r) && h.ObjType(r) == ref.TypePlayer
}

// pnameHistoryDir is upstream's PNAME_HISTORY_PROPDIR, and
// noRecycleProp its NO_RECYCLE_PROP.
const (
	pnameHistoryDir = "@__sys__/name"
	noRecycleProp   = "@/precious"
)
