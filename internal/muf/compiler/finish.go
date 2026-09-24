package compiler

import (
	"strconv"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
)

// Result carries what a compile produced beyond the program itself.
type Result struct {
	Program *muf.Program
	// Notes are messages the compiler wants shown: a $echo, a
	// $pragma warning, or a directive that could not be honoured.
	Notes []string
	// Props are program properties the directives asked for, in
	// the order they were written. They are a list rather than a
	// map because $pubdef may ask for the same path twice and the
	// last one wins, as it would have upstream.
	Props []PropWrite
}

// PropWrite is one property change a directive asked for on the
// program object.
type PropWrite struct {
	Path  string
	Value string
	// Delete removes the property instead of setting it.
	Delete bool
	// KeepExisting leaves a property that is already set alone.
	KeepExisting bool
}

// finish resolves the program's entry point and assembles the result.
func (c *compiler) finish() (*muf.Program, error) {
	if len(c.procOrder) == 0 {
		return nil, &Error{Msg: "program has no procedures"}
	}

	p := &muf.Program{
		Ref:         c.opts.Ref,
		Code:        c.code,
		Vars:        c.vars,
		LVars:       c.lvars,
		Procs:       c.procs,
		Publics:     c.publics,
		PublicOrder: c.publicOrder,
		MLevel:      c.opts.MLevel,
	}

	// Execution starts at the last procedure defined. Upstream
	// pushes each procedure onto the head of a list and starts at
	// the head, which comes to the same thing. $entrypoint names
	// a different one, and set_start prefers it.
	p.Start = c.procs[c.procOrder[len(c.procOrder)-1]]
	if c.altStart >= 0 {
		p.Start = c.altStart
	}

	if err := c.checkJumps(); err != nil {
		return nil, err
	}
	return p, nil
}

// checkJumps verifies every branch target is inside the code. A
// target out of range means the compiler patched something wrong, and
// finding it here beats finding it as a crash at run time.
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

// CompileResult compiles and returns the notes and properties
// alongside the program.
func CompileResult(src string, opts Options) (*Result, error) {
	c := newCompiler(src, opts)
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

	props := make([]PropWrite, 0, len(c.props))
	for _, ps := range c.props {
		props = append(props, PropWrite{
			Path:         ps.path,
			Value:        ps.value,
			Delete:       ps.delete,
			KeepExisting: ps.keepExisting,
		})
	}
	return &Result{Program: p, Notes: c.notes, Props: props}, nil
}
