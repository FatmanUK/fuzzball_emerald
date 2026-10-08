package game

import (
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
)

// objTypeName is upstream's str_objecttype table, for SYSPARM_ARRAY's
// own "objtype" key on a dbref-typed parameter.
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

// TuneGet implements muf.Host for the parameters upstream keeps in C
// globals — PRONOUN_SUB's gender_prop, and the server-policy values
// MPI's tuneBool and tuneInt ask for. It is deliberately **not**
// gated: `tp_gender_prop` is read as a plain global
// (fbstrings.c:307), never through tune_get_parmstring, so putting a
// mucker test here would invent a refusal upstream does not make.
func (h *mufHost) TuneGet(name string) (string, bool) {
	p, ok := tune.Lookup(name)
	if !ok {
		return "", false
	}
	v, _ := h.w.Tune.Get(p.Name)
	return p.Format(v), true
}

// TuneGetParm is tune_get_parmstring (tune.c:341), gate included: a
// parameter whose read level is above mlev answers the empty string
// rather than refusing, which is how a program tells "not for you"
// from "no such parameter" — it cannot.
//
// It also strips the reset prefix first, as upstream does, so
// "%muckname" reads back like "muckname".
func (h *mufHost) TuneGetParm(name string, mlev int) (string, bool) {
	name = strings.TrimPrefix(name, string(tune.ResetFlag))
	p, ok := tune.Lookup(name)
	if !ok {
		return "", false
	}
	if p.ReadMLev > mlev {
		return "", true
	}
	v, _ := h.w.Tune.Get(p.Name)
	return p.Format(v), true
}

// TuneMLevel is TUNE_MLEV (include/db.h:669), and it is @tune's alone
// — nothing else in the mucker system goes above 4.
//
// GOD_PRIV is defined in upstream's default build, so God gets 255
// and the ten parameters whose *read* level is MLEV_GOD —
// smtp_password, the rest of the smtp family, max_force_level and
// strict_god_priv — are beyond a plain wizard. Emerald collapsed
// MLEV_GOD to 4 in the generator and recorded a GodOnly field beside
// it; the levels carry it properly now.
func (h *mufHost) TuneMLevel(who ref.Ref) int {
	if who == ref.God {
		return tune.MLevGod
	}
	return h.Flags(who).MLevel()
}

// TuneReadMLevel and TuneWriteMLevel implement muf.Host for the mlev
// checks SYSPARM, SETSYSPARM and SYSPARM_ARRAY make against
// TUNE_MLEV(player).
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

// TuneSet implements muf.Host for SETSYSPARM, and goes through
// **tune_setparm** (tune.Set.SetParm) rather than the loader's
// setter.
//
// That is two fixes in one. SetParm is the stricter of the two --
// CLAUDE.md spells out how: a boolean reads only its first character,
// a timespan refuses a bare count of seconds -- so SETSYSPARM used to
// accept values upstream rejects. And it returns tune_setparm's own
// result code, which the primitive needs because upstream has a
// different message for each; this used to do the lookup and the
// permission test by hand and then report every failure as a bad
// value.
//
// mlev is the caller's, which SetParm compares against each
// parameter's own write level.
func (h *mufHost) TuneSet(name, value string,
	mlev int) muf.TuneSetResult {

	switch h.w.SetParm(name, value, mlev, h.tuneRefResolver()) {
	case tune.SetSuccess:
		return muf.TuneSetSuccess
	case tune.SetSuccessDefault:
		return muf.TuneSetSuccessDefault
	case tune.SetUnknown:
		return muf.TuneSetUnknown
	case tune.SetSyntax:
		return muf.TuneSetSyntax
	case tune.SetBadVal:
		return muf.TuneSetBadVal
	default:
		return muf.TuneSetDenied
	}
}

// tuneRefResolver is the match list tune_setparm uses for a dbref
// parameter: absolute, registered, player, me, here — and nothing
// nearby, so a room cannot be named by standing in it unless "here"
// is typed.
//
// The searcher is the program's **caller**, which is what
// tune_setparm's own `player` argument is. An earlier version here
// dropped Me, Here and Player on the reasoning that a MUF caller has
// no searcher; it does, and the oracle said so — "here" resolves
// for SETSYSPARM exactly as it does for @tune.
func (h *mufHost) tuneRefResolver() func(string) (ref.Ref,
	ref.ObjType, bool) {

	return func(name string) (ref.Ref, ref.ObjType, bool) {
		r := match.New(h.w, h.caller, name).
			Absolute().Registered().Player().Me().Here().
			Result()
		o := h.w.Get(r)
		if o == nil {
			return ref.Nothing, 0, false
		}
		return r, o.Type(), true
	}
}

// TuneBool and TuneInt implement muf.Host's typed, server-side tune
// reads.
func (h *mufHost) TuneBool(name string) bool {
	return h.w.Tune.Bool(name)
}
func (h *mufHost) TuneInt(name string) int64 {
	return h.w.Tune.Int(name)
}

func (h *mufHost) TuneSpan(name string) time.Duration {
	return h.w.Tune.Duration(name)
}

// TuneList implements muf.Host for SYSPARM_ARRAY, upstream's
// tune_parms_array.
func (h *mufHost) TuneList(pattern string, mlevel int) []muf.TuneEntry {
	var out []muf.TuneEntry
	for _, p := range tune.Params() {
		if p.ReadMLev > mlevel {
			continue
		}
		// equalstr, which is smatch -- so "file_*" matches
		// twenty-six parameters where an exact fold matched
		// none (tune.c:267). strings.EqualFold was wrong
		// twice over: the wrong comparison, and the function
		// CLAUDE.md forbids, since upstream folds only A-Z.
		if pattern != "" && !ascii.SMatch(p.Name, pattern) {
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
			Active:    p.Active(),
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
