package muf

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// CHECKARGS is prim_checkargs (p_stack.c:1143), the largest single
// primitive in the language: a parser over about twenty type
// characters that checks the argument stack against a signature and
// aborts naming the first argument that does not fit.
//
// It was the last stub in the language. Accepting the signature and
// checking nothing would have been worse than aborting, because the
// whole point of the primitive is to surface a caller's bugs.
//
// Four things about it are worth knowing before reading the code.
//
// **The scan runs right to left**, from the end of the signature
// towards the start, because the stack is examined from the top down
// and the rightmost character describes the topmost item. So a
// signature reads in the order a caller would write its arguments.
//
// **Every per-argument refusal carries a position suffix**, which is
// the ABORT_CHECKARGS macro (p_stack.c:1124): " (top)" for the
// topmost item and " (top-N)" for one N deeper. Only the refusals
// about the *signature* — an unknown character, a bad multiplier,
// an unbalanced brace — are bare. Programs match on the whole line,
// so the suffix is part of the contract and not decoration.
//
// **A number repeats whatever is to its left**, and `{ }` groups take
// their repeat count off the stack rather than from the signature.
// `MAX_COMPLEXITY` bounds the nesting of both at 18, and a multiplier
// is bounded by `STACK_SIZE` at 1024.
//
// **An empty signature checks nothing at all.** Upstream stores the
// empty string as a NULL shared_string and reads that as "no
// arguments expected", returning before the loop — so `""
// checkargs` succeeds whatever is on the stack.
func init() {
	register("CHECKARGS", primCheckargs)
}

// checkargsMaxComplexity is MAX_COMPLEXITY (config.h:115): how deep
// ranges and multipliers may nest.
const checkargsMaxComplexity = 18

// checkargsBufLen is BUFFER_LEN (config.h:131), the buffer upstream
// copies the signature into. A longer signature is silently truncated
// rather than refused, despite the function's own comment saying it
// aborts on one that is overly long.
const checkargsBufLen = 8192

// Upstream's wording for every refusal, named because a program
// matches on the exact text and because at this indentation the
// literals do not fit on the line that uses them.
//
// The first eight are about the *signature* and so carry no position
// suffix; the rest go through checkargsAbort, which appends one. Note
// that "Mismatched {" has no full stop where every other message does
// — upstream's, not a slip here.
const (
	ckComplex  = "Argument expression ridiculously complex."
	ckZeroMult = "Bad multiplier '0' in argument expression."
	ckBigMult  = "Multiplier too large in argument expression."
	ckMismatch = "Mismatched { in argument expression"
	ckMisform  = "Misformed argument expression."
	ckUnknown  = "Unknown argument type in expression."
	ckBadForm  = "Badly formed argument expression."
	ckNonStr   = "Non string argument."

	ckUnderflow = "Stack underflow."
	ckRangeType = "Expected an integer range counter."
	ckRangeNeg  = "Range counter should be non-negative."
	ckWantInt   = "Expected an integer."
	ckWantFlt   = "Expected a float."
	ckWantStr   = "Expected a string."
	ckWantFull  = "Expected a non-null string."
	ckWantRef   = "Expected a dbref."
	ckBadRef    = "Invalid dbref."
	ckWantLock  = "Expected a lock boolean expression."
	ckWantVar   = "Expected a variable."
	ckWantAddr  = "Expected a function address."
	ckWantDict  = "Expected a dictionary array."
	ckWantArr   = "Expected an array."
	ckWantList  = "Expected a non-dictionary array."
	ckWantPlay  = "Expected player dbref."
	ckWantRoom  = "Expected room dbref."
	ckWantThng  = "Expected thing dbref."
	ckWantExit  = "Expected exit dbref."
	ckWantProg  = "Expected program dbref."
)

// The two things the range stack can be holding, upstream's anonymous
// enum of itsarange and itsarepeat: a `{ }` group whose count came
// off the argument stack, and a numeric multiplier whose count was
// written in the signature.
const (
	itsarange = iota
	itsarepeat
)

func primCheckargs(f *Frame) (*Result, error) {
	v, err := f.Pop()
	if err != nil {
		return nil, err
	}
	if v.Type != TypeString {
		// Upstream's wording: "Non string", not "Non-string".
		return nil, errf(ckNonStr)
	}
	if v.Str == "" {
		// A NULL shared_string upstream, which it reads as
		// "no args expected" and returns on at once.
		return nil, nil
	}

	buf := []byte(v.Str)
	if len(buf) > checkargsBufLen-1 {
		buf = buf[:checkargsBufLen-1]
	}

	// rngstktyp, rngstkpos and rngstkcnt are upstream's three
	// parallel arrays: what each pending repetition is, where to
	// jump back to, and how many times round it still has to go.
	var (
		rngstktyp [checkargsMaxComplexity]int
		rngstkpos [checkargsMaxComplexity]int
		rngstkcnt [checkargsMaxComplexity]int
		rngstktop int
	)

	currpos := len(buf) - 1
	top := len(f.Stack)
	stackpos := top - 1

	// finishRepeat is the block upstream duplicates verbatim at
	// p_stack.c:1250 and :1473: having just checked an item or
	// closed a group, a pending multiplier either sends the scan
	// back to do it again or is spent. Written once here because
	// the two copies are identical and keeping them apart is only
	// a way for them to drift.
	finishRepeat := func() {
		if rngstktop > 0 &&
			rngstktyp[rngstktop-1] == itsarepeat {
			rngstkcnt[rngstktop-1]--
			if rngstkcnt[rngstktop-1] > 0 {
				currpos = rngstkpos[rngstktop-1]
			} else {
				rngstktop--
			}
		}
	}

	for currpos >= 0 {
		switch c := buf[currpos]; {
		case c >= '0' && c <= '9':
			if rngstktop >= checkargsMaxComplexity {
				return nil, errf(ckComplex)
			}
			// The digits are read right to left, so the
			// first one seen is the ones place. Upstream
			// accumulates in an int and a long enough run
			// overflows it, which is undefined behaviour
			// and the one thing here not reproduced:
			// growth stops once the value is past any
			// multiplier that could be accepted, so an
			// absurd run reports "Multiplier too large"
			// rather than whatever the wrap happened to
			// land on.
			mult, result := 1, 0
			for currpos >= 0 && buf[currpos] >= '0' &&
				buf[currpos] <= '9' {
				if result < StackSize {
					result += mult *
						int(buf[currpos]-'0')
					mult *= 10
				}
				currpos--
			}
			if result == 0 {
				return nil, errf(ckZeroMult)
			}
			if result >= StackSize {
				return nil, errf(ckBigMult)
			}
			rngstktyp[rngstktop] = itsarepeat
			rngstkcnt[rngstktop] = result
			rngstkpos[rngstktop] = currpos
			rngstktop++

		case c == '}':
			// A group's repeat count is a runtime value,
			// so closing one consumes a stack item. The
			// complexity check comes first, before either
			// stack test, which decides which refusal a
			// signature that is both too deep and short
			// of arguments gets.
			if rngstktop >= checkargsMaxComplexity {
				return nil, errf(ckComplex)
			}
			if stackpos < 0 {
				return nil, checkargsAbort(
					ckUnderflow, top, stackpos)
			}
			if f.Stack[stackpos].Type != TypeInteger {
				return nil, checkargsAbort(
					ckRangeType, top, stackpos)
			}
			result := int(f.Stack[stackpos].Num)
			if result < 0 {
				return nil, checkargsAbort(
					ckRangeNeg, top, stackpos)
			}
			rngstkpos[rngstktop] = currpos - 1
			rngstkcnt[rngstktop] = result
			rngstktyp[rngstktop] = itsarange
			rngstktop++
			currpos--

			if result == 0 {
				// Skip the group's body without
				// checking anything in it. The scan
				// does not understand nesting, so an
				// inner `{` stops it rather than the
				// group's own; and the bound is `>
				// 0`, so index 0 is never examined.
				// Both are upstream's.
				for currpos > 0 &&
					buf[currpos] != '{' {
					currpos--
				}
			}
			stackpos--

		case c == '{':
			if rngstktop <= 0 {
				return nil, errf(ckMismatch)
			}
			if rngstktyp[rngstktop-1] != itsarange {
				return nil, errf(ckMisform)
			}
			// An empty group would repeat nothing for
			// however many times the stack asked, which
			// upstream's own comment says could hang the
			// server for billions of iterations; it
			// forces the count to 1 instead. The two
			// reads of buf[currpos+1] run off the end of
			// the string in C and stop on its NUL, so
			// both need a bound here.
			next := currpos + 1
			if next < len(buf) && (buf[next] == ' ' ||
				buf[next] == '}') {
				for next < len(buf) &&
					buf[next] == ' ' {
					next++
				}
				if next < len(buf) &&
					buf[next] == '}' {
					rngstkcnt[rngstktop-1] = 1
				}
			}

			rngstkcnt[rngstktop-1]--
			if rngstkcnt[rngstktop-1] > 0 {
				currpos = rngstkpos[rngstktop-1]
			} else {
				rngstktop--
				currpos--
				finishRepeat()
			}

		default:
			if err := checkargsOne(f, c, top,
				stackpos); err != nil {
				return nil, err
			}
			currpos--
			if c != ' ' {
				// Upstream increments stackpos inside
				// the space case and decrements it
				// again below the switch, a net zero:
				// a space describes no argument.
				stackpos--
			}
			finishRepeat()
		}
	}

	if rngstktop > 0 {
		// A group or a multiplier that never found its start.
		return nil, errf(ckBadForm)
	}
	return nil, nil
}

// checkargsAbort is the ABORT_CHECKARGS macro (p_stack.c:1124). Every
// refusal about an *argument* names which stack position failed,
// counted down from the top; the ones about the signature itself do
// not. The offset is relative, so a caller need not know the absolute
// depth to predict it.
func checkargsAbort(msg string, top, stackpos int) error {
	if top == stackpos+1 {
		return errf("%s (top)", msg)
	}
	return errf("%s (top-%d)", msg, top-stackpos-1)
}

// checkargsOne is the type switch: one signature character against
// one stack item. It is separate from the scan because the scan is
// about repetition and this is about types, and because a pure
// function of a character and a stack is exhaustively testable.
//
// Note that the unknown-character refusal does *not* test for stack
// underflow first, so a signature with a stray letter reports the
// letter even when there is nothing on the stack to have checked.
func checkargsOne(f *Frame, c byte, top, stackpos int) error {
	// A space describes no argument, so it is the one character
	// that needs no stack item at all.
	if c == ' ' {
		return nil
	}

	fail := func(msg string) error {
		return checkargsAbort(msg, top, stackpos)
	}
	// Every remaining character but an unknown one needs an item;
	// upstream repeats this test in each case of the switch,
	// which its own @TODO complains about.
	need := func() (Value, error) {
		if stackpos < 0 {
			return Value{}, fail(ckUnderflow)
		}
		return f.Stack[stackpos], nil
	}

	switch c {
	case 'i':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeInteger {
			return fail(ckWantInt)
		}

	case 'n':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeFloat {
			return fail(ckWantFlt)
		}

	case 's', 'S':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeString {
			return fail(ckWantStr)
		}
		// Upstream's empty string is a NULL shared_string, so
		// this is the uppercase form refusing one.
		if c == 'S' && v.Str == "" {
			return fail(ckWantFull)
		}

	case 'd', 'p', 'r', 't', 'e', 'f',
		'D', 'P', 'R', 'T', 'E', 'F':
		v, err := need()
		if err != nil {
			return err
		}
		return checkargsRef(f, c, v, fail)

	case '?':
		// Any type will do, but there still has to be
		// something there.
		if _, err := need(); err != nil {
			return err
		}

	case 'l':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeLock {
			return fail(ckWantLock)
		}

	case 'v':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeVar && v.Type != TypeLVar &&
			v.Type != TypeSVar {
			return fail(ckWantVar)
		}

	case 'a':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeAddress {
			return fail(ckWantAddr)
		}

	case 'x':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeArray || v.Array == nil ||
			v.Array.IsList() {
			return fail(ckWantDict)
		}

	case 'y', 'Y':
		v, err := need()
		if err != nil {
			return err
		}
		if v.Type != TypeArray {
			return fail(ckWantArr)
		}
		// A nil array passes 'Y': upstream guards the
		// dictionary test on the pointer being non-NULL.
		if c == 'Y' && v.Array != nil &&
			!v.Array.IsList() {
			return fail(ckWantList)
		}

	default:
		return errf(ckUnknown)
	}
	return nil
}

// checkargsRef is the inner switch over the twelve dbref characters.
// Six types, each with a lowercase form and an uppercase one that
// also insists the ref really exists and really is of that type.
//
// What the lowercase forms accept is wider than it looks, and is the
// part of this primitive most likely to surprise. The outer test is
// `ref >= db_top || ref < HOME`, so NOTHING (#-1), AMBIGUOUS (#-2)
// and HOME (#-3) all survive it — and then each lowercase case asks
// its type question only of a *non-negative* ref. So `p` accepts #-1
// and #-2 as readily as a real player, and refuses only HOME; `r`
// refuses nothing negative at all, because Typeof(HOME) is TYPE_ROOM
// (db.h:423) and the explicit HOME refusal the other five carry is
// missing from the room case.
func checkargsRef(f *Frame, c byte, v Value,
	fail func(string) error) error {
	if v.Type != TypeObject {
		return fail(ckWantRef)
	}
	r := v.Ref

	// db_top. A frame with no host at all is only reachable from
	// a unit test; reporting an empty database there beats
	// panicking, and leaves the negative refs behaving as they
	// would anywhere.
	dbTop := ref.Ref(0)
	if f.host != nil {
		dbTop = f.host.Top()
	}
	if r >= dbTop || r < ref.Home {
		return fail(ckBadRef)
	}

	// typeOf is C's Typeof (db.h:423), which answers TYPE_ROOM
	// for HOME rather than reading a record that is not there.
	typeOf := func(r ref.Ref) ref.ObjType {
		if r == ref.Home {
			return ref.TypeRoom
		}
		if f.host == nil {
			return ref.NoType
		}
		return f.host.ObjType(r)
	}

	// Each uppercase form falls through to its lowercase one in
	// the C, so the strict test is the extra one and the shared
	// test is below it.
	switch c {
	case 'D', 'd':
		if c == 'D' {
			if r < 0 && r != ref.Home {
				return fail(ckBadRef)
			}
			if typeOf(r) == ref.TypeGarbage {
				return fail(ckBadRef)
			}
		}
		if r < ref.Home {
			return fail(ckBadRef)
		}

	case 'P', 'p':
		const msg = ckWantPlay
		if c == 'P' && r < 0 {
			return fail(msg)
		}
		if r >= 0 && typeOf(r) != ref.TypePlayer {
			return fail(msg)
		}
		if r == ref.Home {
			return fail(msg)
		}

	case 'R', 'r':
		const msg = ckWantRoom
		if c == 'R' && r < 0 && r != ref.Home {
			return fail(msg)
		}
		// No HOME refusal here, unlike the other five.
		if r >= 0 && typeOf(r) != ref.TypeRoom {
			return fail(msg)
		}

	case 'T', 't':
		const msg = ckWantThng
		if c == 'T' && r < 0 {
			return fail(msg)
		}
		if r >= 0 && typeOf(r) != ref.TypeThing {
			return fail(msg)
		}
		if r == ref.Home {
			return fail(msg)
		}

	case 'E', 'e':
		const msg = ckWantExit
		if c == 'E' && r < 0 {
			return fail(msg)
		}
		if r >= 0 && typeOf(r) != ref.TypeExit {
			return fail(msg)
		}
		if r == ref.Home {
			return fail(msg)
		}

	case 'F', 'f':
		const msg = ckWantProg
		if c == 'F' && r < 0 {
			return fail(msg)
		}
		if r >= 0 && typeOf(r) != ref.TypeProgram {
			return fail(msg)
		}
		if r == ref.Home {
			return fail(msg)
		}
	}
	return nil
}
