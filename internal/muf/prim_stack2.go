package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// INTERP runs another program as a subroutine and takes back whatever
// it left on its stack, src/p_stack.c's one remaining primitive.
//
// Unlike CALL, the program runs as its own frame with its own
// variables and its own mucker level rather than borrowing the
// caller's, which is why it takes a trigger of its own: it is closer
// to the exit-triggered run of a program than to a function call.
func init() {
	register("INTERP", func(f *Frame) (*Result, error) {
		arg, err := f.Pop()
		if err != nil {
			return nil, err
		}
		trigV, err := f.Pop()
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
		if progV.Type != TypeObject || !h.Valid(progV.Ref) ||
			h.ObjType(progV.Ref) != ref.TypeProgram {
			return nil, errf("Bad program reference. (1)")
		}
		if trigV.Type != TypeObject || !h.Valid(trigV.Ref) {
			return nil, errf("Bad object. (2)")
		}
		if arg.Type != TypeString {
			return nil, errf("Expected a string. (3)")
		}
		// The trigger is what the run is attributed to, so
		// below mucker level 3 a program may only name one it
		// already owns.
		if f.MLevel() < 3 &&
			h.Owner(trigV.Ref) != h.Owner(f.Prog.Ref) {
			return nil, errf("Permission denied.")
		}
		if f.Level > 8 {
			return nil, errf("Interp call loops not allowed.")
		}
		// INTERP does not touch match_cmdname, so the called
		// program inherits the caller's COMMAND rather than
		// getting one of its own. p_stack.c:1760 sets only
		// match_args.
		v, ok := h.Interp(f.Descr, f.Level, progV.Ref,
			trigV.Ref, f.Vars[VarCommand].Str, arg.Str)
		if !ok {
			// A program that aborted, blocked or finished
			// with an empty stack yields the empty string
			// rather than failing the caller.
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(v)
	})
}
