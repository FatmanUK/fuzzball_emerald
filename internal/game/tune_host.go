package game

import (
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
)

// objTypeName is upstream's str_objecttype table, for SYSPARM_ARRAY's own
// "objtype" key on a dbref-typed parameter.
func objTypeName(t ref.ObjType) string {
	switch t {
	case ref.TypeRoom:
		return "room"
	case ref.TypeThing:
		return "thing"
	case ref.TypeExit:
		return "exit"
	case ref.TypePlayer:
		return "player"
	case ref.TypeProgram:
		return "program"
	}
	return "unknown"
}

// TuneGet implements muf.Host for SYSPARM (and PRONOUN_SUB's own gender_prop
// lookup), upstream's tune_get_parmstring minus its own mlev gate.
func (h *mufHost) TuneGet(name string) (string, bool) {
	p, ok := tune.Lookup(name)
	if !ok {
		return "", false
	}
	v, _ := h.w.Tune.Get(p.Name)
	return p.Format(v), true
}

// TuneReadMLevel and TuneWriteMLevel implement muf.Host for the mlev checks
// SYSPARM, SETSYSPARM and SYSPARM_ARRAY make against TUNE_MLEV(player).
func (h *mufHost) TuneReadMLevel(name string) (int, bool) {
	p, ok := tune.Lookup(name)
	if !ok {
		return 0, false
	}
	return p.ReadMLev, true
}

func (h *mufHost) TuneWriteMLevel(name string) (int, bool) {
	p, ok := tune.Lookup(name)
	if !ok {
		return 0, false
	}
	return p.WriteMLev, true
}

// TuneSet implements muf.Host for SETSYSPARM, upstream's tune_setparm minus
// its own mlev gate.
func (h *mufHost) TuneSet(name, value string) (bool, error) {
	reset := false
	if rest, isReset := strings.CutPrefix(name, "%"); isReset {
		name, reset = rest, true
	}
	p, ok := tune.Lookup(name)
	if !ok {
		return false, nil
	}
	if reset {
		return true, h.w.ResetTune(p.Name)
	}
	return true, h.w.SetTune(p.Name, value)
}

// TuneBool and TuneInt implement muf.Host's typed, server-side tune reads.
func (h *mufHost) TuneBool(name string) bool { return h.w.Tune.Bool(name) }
func (h *mufHost) TuneInt(name string) int64 { return h.w.Tune.Int(name) }

func (h *mufHost) TuneSpan(name string) time.Duration { return h.w.Tune.Duration(name) }

// TuneList implements muf.Host for SYSPARM_ARRAY, upstream's
// tune_parms_array.
func (h *mufHost) TuneList(pattern string, mlevel int) []muf.TuneEntry {
	var out []muf.TuneEntry
	for _, p := range tune.Params() {
		if p.ReadMLev > mlevel {
			continue
		}
		if pattern != "" && !strings.EqualFold(pattern, p.Name) {
			continue
		}
		v, _ := h.w.Tune.Get(p.Name)
		entry := muf.TuneEntry{
			Group:     p.Group,
			Name:      p.Name,
			Label:     p.Label,
			Type:      p.Type.String(),
			ReadMLev:  p.ReadMLev,
			WriteMLev: p.WriteMLev,
			Nullable:  p.Nullable,
			Active:    true,
			Default:   h.w.Tune.IsDefault(p.Name),
			ValueStr:  v.Str,
			ValueNum:  v.Num,
			ValueRef:  v.Ref,
			ValueBool: v.Bool,
		}
		switch p.Type {
		case tune.TypeTimespan:
			entry.ValueNum = int64(v.Span.Seconds())
		case tune.TypeDbref:
			entry.ObjType = "unknown"
			if p.HasObjType {
				entry.ObjType = objTypeName(p.ObjType)
			}
		}
		out = append(out, entry)
	}
	return out
}
