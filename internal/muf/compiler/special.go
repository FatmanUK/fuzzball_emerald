package compiler

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
)

// specialWords are the reserved words, from special() in
// src/compile.c. They are matched before primitives, so none of these
// can be a primitive name.
var specialWords = map[string]bool{
	":": true, ";": true,
	"if": true, "else": true, "then": true,
	"begin": true, "for": true, "foreach": true,
	"until": true, "while": true, "break": true, "continue": true,
	"repeat": true,
	"try":    true, "catch": true, "catch_detailed": true, "endcatch": true,
	"call": true, "public": true, "wizcall": true,
	"lvar": true, "var!": true, "var": true,
}

func isSpecial(word string) bool {
	return specialWords[ascii.Fold(word)]
}

// special compiles a reserved word.
func (c *compiler) special(word string) error {
	w := ascii.Fold(word)

	// Everything but a procedure header and a declaration needs
	// to be inside a procedure.
	switch w {
	case ":", "lvar", "var", "var!", "public", "wizcall":
	default:
		if c.curProc == nil {
			return c.errf("%s outside a procedure", word)
		}
	}

	switch w {
	case ":":
		return c.beginProc()
	case ";":
		return c.endProc()
	case "if":
		addr := c.emit(muf.Inst{Type: muf.TypeIf})
		c.pushCtrl(control{kind: ctrlIf, addr: addr, line: c.line})
	case "else":
		return c.doElse()
	case "then":
		return c.doThen()
	case "begin":
		c.pushCtrl(control{kind: ctrlBegin, addr: c.here(), line: c.line})
	case "for":
		return c.beginFor(muf.InFor)
	case "foreach":
		return c.beginFor(muf.InForeach)
	case "until":
		return c.closeLoop(true)
	case "repeat":
		return c.closeLoop(false)
	case "while":
		return c.doWhile()
	case "break":
		return c.doBreak()
	case "continue":
		return c.doContinue()
	case "try":
		addr := c.emit(muf.Inst{Type: muf.TypeTry})
		c.pushCtrl(control{kind: ctrlTry, addr: addr, line: c.line})
		c.noteTry(1)
	case "catch", "catch_detailed":
		return c.doCatch(w == "catch_detailed")
	case "endcatch":
		return c.doEndCatch()
	case "call":
		c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InCall)})
	case "lvar":
		return c.declare(&c.lvars, "LVAR")
	case "var":
		return c.declareVar(false)
	case "var!":
		return c.declareVar(true)
	case "public", "wizcall":
		return c.declarePublic(w == "wizcall")
	}
	return nil
}

func (c *compiler) pushCtrl(ct control) {
	c.ctrl = append(c.ctrl, ct)
}

// innermost returns the open control structure, or zero when there is
// none.
func (c *compiler) innermost() controlKind {
	if len(c.ctrl) == 0 {
		return 0
	}
	return c.ctrl[len(c.ctrl)-1].kind
}

// popCtrl removes the innermost control structure, which must be one
// of the kinds given.
func (c *compiler) popCtrl(want ...controlKind) (control, bool) {
	if len(c.ctrl) == 0 {
		return control{}, false
	}
	top := c.ctrl[len(c.ctrl)-1]
	for _, k := range want {
		if top.kind == k {
			c.ctrl = c.ctrl[:len(c.ctrl)-1]
			return top, true
		}
	}
	return control{}, false
}

// patch points a jump at the given address.
func (c *compiler) patch(at, target int) {
	c.code[at].Num = int64(target)
}

// beginProc starts a ':' definition.
func (c *compiler) beginProc() error {
	if c.curProc != nil {
		return c.errf("definition inside a definition")
	}
	tok, ok, err := c.next()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf("unexpected end of program inside a procedure")
	}

	name := tok.text
	// "name[ a b -- c ]" declares arguments, which become scoped
	// variables.
	declaresArgs := strings.HasSuffix(name, "[")
	if declaresArgs {
		name = strings.TrimSuffix(name, "[")
		if name == "" {
			return c.errf("bad procedure name")
		}
	}
	if _, taken := c.procs[ascii.Fold(name)]; taken {
		return c.errf("procedure %s is already defined", name)
	}

	proc := &muf.Proc{Name: name}
	c.curProc = proc
	c.svars = c.svars[:0]

	addr := c.emit(muf.Inst{Type: muf.TypeFunction, Proc: proc})
	c.procStart = addr
	c.procs[ascii.Fold(name)] = addr
	c.procOrder = append(c.procOrder, ascii.Fold(name))

	if declaresArgs {
		if err := c.procArgs(proc); err != nil {
			return err
		}
	}
	return nil
}

// procArgs reads the "a b -- c ]" argument list after a "name["
// header.
//
// Names before the "--" become scoped variables holding the
// arguments; the part after it documents what the procedure returns
// and is not compiled.
func (c *compiler) procArgs(proc *muf.Proc) error {
	seenDashes := false
	for {
		tok, ok, err := c.next()
		if err != nil {
			return err
		}
		if !ok {
			return c.errf("unexpected end of program in an argument list")
		}
		switch {
		case tok.text == "]":
			proc.Args = len(c.svars)
			proc.Vars = len(c.svars)
			proc.VarNames = append([]string{}, c.svars...)
			return nil
		case tok.text == "--":
			seenDashes = true
		case seenDashes:
			// Return values are documentation only.
		default:
			// An argument may be written "type:name",
			// where the type is documentation. The name
			// is what follows the first colon; a bare
			// "type:" declares nothing.
			name := tok.text
			if _, after, found := strings.Cut(name, ":"); found {
				name = after
			}
			if name == "" {
				continue
			}
			if len(c.svars) >= muf.MaxVars {
				return c.errf("too many arguments")
			}
			if _, dup := indexOf(c.svars, name); dup {
				return c.errf("argument %s is declared twice", name)
			}
			c.svars = append(c.svars, name)
		}
	}
}

// endProc closes a ';' definition.
func (c *compiler) endProc() error {
	if c.curProc == nil {
		return c.errf("; without a procedure")
	}
	if len(c.ctrl) > 0 {
		open := c.ctrl[len(c.ctrl)-1]
		return &Error{Line: open.line,
			Msg: "unterminated " + open.kind.String() + " at the end of a procedure"}
	}

	c.curProc.Vars = len(c.svars)
	c.curProc.VarNames = append([]string{}, c.svars...)
	c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InExit)})
	c.curProc = nil
	c.svars = c.svars[:0]
	return nil
}

// doElse closes the true branch and opens the false one.
func (c *compiler) doElse() error {
	if c.innermost() != ctrlIf {
		return c.unterminated("ELSE", "ELSE without IF")
	}
	jump := c.emit(muf.Inst{Type: muf.TypeJmp})
	eef, _ := c.popCtrl(ctrlIf)
	// The IF jumps past the ELSE's jump, to the first instruction
	// of the false branch.
	c.patch(eef.addr, c.here())
	c.pushCtrl(control{kind: ctrlElse, addr: jump, line: c.line})
	return nil
}

// doThen closes an IF or an IF/ELSE.
func (c *compiler) doThen() error {
	eef, ok := c.popCtrl(ctrlIf, ctrlElse)
	if !ok {
		return c.unterminated("THEN", "THEN without IF")
	}
	c.patch(eef.addr, c.here())
	return nil
}

// beginFor opens a FOR or FOREACH loop.
//
// Both compile to the same shape: the setup primitive, then an
// iterator that pushes the next value, then a conditional jump out of
// the loop. The loop start is the iterator, so each pass re-enters
// there.
func (c *compiler) beginFor(setup int) error {
	c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(setup)})
	iter := c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InForIter)})
	exit := c.emit(muf.Inst{Type: muf.TypeIf})
	c.pushCtrl(control{kind: ctrlFor, addr: iter, line: c.line, exits: []int{exit}})
	return nil
}

// closeLoop compiles UNTIL or REPEAT.
func (c *compiler) closeLoop(conditional bool) error {
	word := "REPEAT"
	if conditional {
		word = "UNTIL"
	}
	loop, ok := c.popCtrl(ctrlBegin, ctrlFor)
	if !ok {
		return c.unterminated(word, "loop start not found for "+word)
	}

	if conditional {
		// UNTIL jumps back while the condition is false.
		c.emit(muf.Inst{Type: muf.TypeIf, Num: int64(loop.addr)})
	} else {
		c.emit(muf.Inst{Type: muf.TypeJmp, Num: int64(loop.addr)})
	}
	if loop.kind == ctrlFor {
		c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InForPop)})
	}
	// Everything that jumped out of the loop lands after it, past
	// the FORPOP so the iterator is cleaned up first.
	for _, at := range loop.exits {
		c.patch(at, c.here())
	}
	return nil
}

// doWhile exits the loop when the condition is false.
func (c *compiler) doWhile() error {
	loop := c.enclosingLoop()
	if loop < 0 {
		return c.errf("WHILE outside a loop")
	}
	c.unwindTrys(loop)
	exit := c.emit(muf.Inst{Type: muf.TypeIf})
	c.ctrl[loop].exits = append(c.ctrl[loop].exits, exit)
	return nil
}

// doBreak leaves the loop unconditionally.
func (c *compiler) doBreak() error {
	loop := c.enclosingLoop()
	if loop < 0 {
		return c.errf("BREAK outside a loop")
	}
	c.unwindTrys(loop)
	exit := c.emit(muf.Inst{Type: muf.TypeJmp})
	c.ctrl[loop].exits = append(c.ctrl[loop].exits, exit)
	return nil
}

// doContinue jumps back to the loop's start.
func (c *compiler) doContinue() error {
	loop := c.enclosingLoop()
	if loop < 0 {
		return c.errf("CONTINUE outside a loop")
	}
	c.unwindTrys(loop)
	c.emit(muf.Inst{Type: muf.TypeJmp, Num: int64(c.ctrl[loop].addr)})
	return nil
}

// enclosingLoop returns the index of the innermost loop, or -1.
func (c *compiler) enclosingLoop() int {
	for i := len(c.ctrl) - 1; i >= 0; i-- {
		if k := c.ctrl[i].kind; k == ctrlBegin ||
			k == ctrlFor {
			return i
		}
	}
	return -1
}

// unwindTrys emits a TRYPOP for each TRY block open inside the loop,
// so leaving the loop early does not leave a catch handler installed.
func (c *compiler) unwindTrys(loop int) {
	for i := loop + 1; i < len(c.ctrl); i++ {
		if c.ctrl[i].kind == ctrlTry ||
			c.ctrl[i].kind == ctrlCatch {
			c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InTryPop)})
		}
	}
}

// noteTry records that a TRY was opened, for the loop that encloses
// it.
func (c *compiler) noteTry(delta int) {
	if loop := c.enclosingLoop(); loop >= 0 {
		c.ctrl[loop].trys += delta
	}
}

// doCatch closes the guarded block and opens the handler.
func (c *compiler) doCatch(detailed bool) error {
	if c.innermost() != ctrlTry {
		return c.unterminated("CATCH", "no TRY found for CATCH")
	}
	// The guarded block ran without raising: discard the handler
	// and jump past it.
	c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(muf.InTryPop)})
	jump := c.emit(muf.Inst{Type: muf.TypeJmp})

	// The handler starts here, and the TRY points at it.
	prim := muf.InCatch
	if detailed {
		prim = muf.InCatchDetailed
	}
	handler := c.emit(muf.Inst{Type: muf.TypePrimitive, Num: int64(prim)})

	eef, _ := c.popCtrl(ctrlTry)
	c.patch(eef.addr, handler)
	c.pushCtrl(control{kind: ctrlCatch, addr: jump, line: c.line})
	return nil
}

// doEndCatch closes the handler.
func (c *compiler) doEndCatch() error {
	eef, ok := c.popCtrl(ctrlCatch)
	if !ok {
		return c.unterminated("ENDCATCH", "no CATCH found for ENDCATCH")
	}
	c.patch(eef.addr, c.here())
	c.noteTry(-1)
	return nil
}

// unterminated reports the specific mismatch upstream would, naming
// the block that is actually open rather than only the word that
// failed.
func (c *compiler) unterminated(word, fallback string) error {
	switch c.innermost() {
	case ctrlTry:
		return c.errf("unterminated TRY-CATCH block at %s", word)
	case ctrlCatch:
		return c.errf("unterminated CATCH-ENDCATCH block at %s", word)
	case ctrlFor, ctrlBegin:
		return c.errf("unterminated loop at %s", word)
	case ctrlIf, ctrlElse:
		return c.errf("unterminated IF-THEN at %s", word)
	}
	return c.errf("%s", fallback)
}

// declare reads a variable name after LVAR.
func (c *compiler) declare(table *[]string, word string) error {
	tok, ok, err := c.next()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf("unexpected end of program after %s", word)
	}
	if _, dup := indexOf(*table, tok.text); dup {
		return c.errf("%s is already declared", tok.text)
	}
	if len(*table) >= muf.MaxVars {
		return c.errf("too many variables")
	}
	*table = append(*table, tok.text)
	return nil
}

// declareVar handles VAR and VAR!.
//
// Inside a procedure both declare a *scoped* variable, private to
// that procedure, which is why two procedures may each declare a
// "pos". Only at the top level does VAR create a program global, and
// VAR! is an error there because it has nothing to store.
//
// VAR! additionally stores the top of the stack into the new
// variable, which is the idiom for naming a procedure's arguments.
func (c *compiler) declareVar(store bool) error {
	tok, ok, err := c.next()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf("unexpected end of program after a variable declaration")
	}
	name := tok.text

	if c.curProc == nil {
		if store {
			return c.errf("VAR! used outside a procedure")
		}
		if _, dup := indexOf(c.vars, name); dup {
			return c.errf("%s is already declared", name)
		}
		if len(c.vars) >= muf.MaxVars {
			return c.errf("too many variables")
		}
		c.vars = append(c.vars, name)
		return nil
	}

	if _, dup := indexOf(c.svars, name); dup {
		return c.errf("%s is already declared", name)
	}
	if len(c.svars) >= muf.MaxVars {
		return c.errf("too many variables")
	}
	slot := len(c.svars)
	c.svars = append(c.svars, name)
	if store {
		c.emit(muf.Inst{Type: muf.TypeSVarBang, Num: int64(slot)})
	}
	return nil
}

// declarePublic exposes the last-defined procedure under a name other
// programs can CALL.
func (c *compiler) declarePublic(wizOnly bool) error {
	tok, ok, err := c.next()
	if err != nil {
		return err
	}
	if !ok {
		return c.errf("Subroutine unknown in PUBLIC or WIZCALL declaration.")
	}
	addr, known := c.procs[ascii.Fold(tok.text)]
	if !known {
		return c.errf("Subroutine unknown in PUBLIC or WIZCALL declaration.")
	}
	mlev := 1
	if wizOnly {
		mlev = 4
	}
	name := ascii.Fold(tok.text)
	if _, already := c.publics[name]; already {
		return c.errf("Function already declared public.")
	}
	c.publicOrder = append(c.publicOrder, name)
	c.publics[name] = &muf.Public{
		Name:   tok.text,
		Addr:   addr,
		MLevel: mlev,
		Proc:   c.code[addr].Proc,
	}
	return nil
}
