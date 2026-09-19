package game

import "github.com/FatmanUK/fuzzball_emerald/internal/ref"

// ForceLevel, IsPID and Instances implement muf.Host for FORCE_LEVEL,
// ISPID? and INSTANCES.

func (h *mufHost) ForceLevel() int { return h.s.forceDepth }

func (h *mufHost) IsPID(pid int) bool { return h.s.procs.get(pid) != nil }

func (h *mufHost) Instances(prog ref.Ref) int {
	return len(h.s.procs.forProgram(prog))
}
