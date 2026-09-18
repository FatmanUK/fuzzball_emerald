package compiler

import (
	"strconv"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
)

// Result carries what a compile produced beyond the program itself.
type Result struct {
	Program *muf.Program
	// Notes are messages the compiler wants shown: a $echo, or a directive
	// that could not be honoured.
	Notes []string
	// Props are program properties a directive asked to set, such as
	// $author and $version.
	Props map[string]string
}

// finish resolves the program's entry point and assembles the result.
func (c *compiler) finish() (*muf.Program, error) {
	if len(c.procOrder) == 0 {
		return nil, &Error{Msg: "program has no procedures"}
	}

	p := &muf.Program{
		Ref:     c.opts.Ref,
		Code:    c.code,
		Vars:    c.vars,
		LVars:   c.lvars,
		Procs:   c.procs,
		Publics: c.publics,
		MLevel:  c.opts.MLevel,
	}

	// Execution starts at the last procedure defined. Upstream pushes each
	// procedure onto the head of a list and starts at the head, which comes
	// to the same thing.
	p.Start = c.procs[c.procOrder[len(c.procOrder)-1]]

	if err := c.checkJumps(); err != nil {
		return nil, err
	}
	return p, nil
}

// checkJumps verifies every branch target is inside the code. A target out of
// range means the compiler patched something wrong, and finding it here beats
// finding it as a crash at run time.
func (c *compiler) checkJumps() error {
	for i, in := range c.code {
		switch in.Type {
		case muf.TypeIf, muf.TypeJmp, muf.TypeTry, muf.TypeAddress:
			if in.Num < 0 || int(in.Num) > len(c.code) {
				return &Error{Line: in.Line,
					Msg: "internal error: instruction " +
						strconv.Itoa(i) + " branches outside the program"}
			}
		}
	}
	return nil
}

// CompileResult compiles and returns the notes and properties alongside the
// program.
func CompileResult(src string, opts Options) (*Result, error) {
	c := &compiler{
		lex:     newLexer(src),
		opts:    opts,
		procs:   map[string]int{},
		publics: map[string]*muf.Public{},
		defs:    map[string]string{},
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	if err := c.run(); err != nil {
		return nil, err
	}
	p, err := c.finish()
	if err != nil {
		return nil, err
	}

	props := make(map[string]string, len(c.props))
	for _, ps := range c.props {
		props[ps.name] = ps.value
	}
	return &Result{Program: p, Notes: c.notes, Props: props}, nil
}
