package game

import (
	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// ForceLevel, IsPID and Instances implement muf.Host for FORCE_LEVEL,
// ISPID? and INSTANCES.

func (h *mufHost) ForceLevel() int { return h.s.forceDepth }

func (h *mufHost) IsPID(pid int) bool { return h.s.procs.get(pid) != nil }

func (h *mufHost) Instances(prog ref.Ref) int {
	return len(h.s.procs.forProgram(prog))
}

// CanCall implements muf.Host for CANCALL?.
func (h *mufHost) CanCall(callerLevel int, callerUID ref.Ref, prog ref.Ref, name string) bool {
	p, err := h.s.compileProgram(h.w, prog)
	if err != nil || p.MLevel <= 0 {
		return false
	}

	if callerLevel < 4 && h.Owner(prog) != callerUID && !linkable(h.Flags(prog), h.ObjType(prog)) {
		return false
	}

	pub, ok := p.Publics[ascii.Fold(name)]
	if !ok {
		return false
	}
	return callerLevel >= pub.MLevel
}

// linkable is upstream's Linkable macro: a room or thing needs ABODE, anything
// else — a player, exit or program — needs LINK_OK. The two flags share no
// bit, so the check genuinely differs by type rather than being the same test
// worded twice.
func linkable(f ref.Flags, t ref.ObjType) bool {
	if t == ref.TypeRoom || t == ref.TypeThing {
		return f&ref.Abode != 0
	}
	return f&ref.LinkOK != 0
}
