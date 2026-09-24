package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Controls implements muf.Host for PROGRAM_GETLINES's own controls()
// check.
func (h *mufHost) Controls(who, target ref.Ref) bool {
	return h.s.controls(h.w, who, target)
}

// CompiledSize, Compile and Uncompile implement muf.Host for
// COMPILED?, COMPILE and UNCOMPILE.
func (h *mufHost) CompiledSize(prog ref.Ref) int {
	c, ok := h.s.programs[prog]
	if !ok || c.prog == nil {
		return 0
	}
	return len(c.prog.Code)
}

func (h *mufHost) Compile(prog ref.Ref) (int, error) {
	h.s.InvalidateProgram(prog)
	p, err := h.s.compileProgram(h.w, prog)
	if err != nil {
		return 0, err
	}
	return len(p.Code), nil
}

func (h *mufHost) Uncompile(prog ref.Ref) {
	h.s.InvalidateProgram(prog)
}

// ProgramLines implements muf.Host for PROGRAM_GETLINES.
func (h *mufHost) ProgramLines(prog ref.Ref) []string {
	src, ok := h.w.Source(prog)
	if !ok || src == "" {
		return nil
	}
	return strings.Split(src, "\n")
}
