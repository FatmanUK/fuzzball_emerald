package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// ignoreProp is upstream's IGNORE_PROP: a hidden reflist under a
// directory no MUF property-reading primitive can reach, upstream's
// own SYSTEM_PROPDIR_NOREAD.
const ignoreProp = "@__sys__/ignore/def"

// nameForbidden is upstream's ok_object_name: characters and whole
// names no object may be created with, regardless of type.
func nameForbidden(name string) bool {
	if name == "" {
		return true
	}
	switch name[0] {
	case '!', '*', '$', '#':
		return true
	}
	if strings.ContainsAny(name, "=&|\r\x1b") {
		return true
	}
	switch strings.ToLower(name) {
	case "me", "here", "home", "nil":
		return true
	}
	return false
}

// NameOK implements muf.Host for EXT-NAME-OK?, upstream's
// ok_object_name. Unlike upstream, this does not check
// tp_reserved_names/ tp_reserved_player_names, ok_player_name's
// length limit, or the 7-bit-only tune flags — Emerald has no
// object-creation command that enforces any of those either, so
// adding them only to this one introspection primitive would make it
// stricter than @create itself.
func (h *mufHost) NameOK(name string, t ref.ObjType) bool {
	if nameForbidden(name) {
		return false
	}
	if t == ref.TypePlayer {
		_, exists := h.w.PlayerNamed(name)
		return !exists
	}
	if t == ref.TypeGarbage {
		return false
	}
	return true
}

// UserLog implements muf.Host for USERLOG, upstream's log_user —
// written to the "muf" diagnostic channel (internal/logging) rather
// than upstream's own flat tp_file_log_user file, matching how every
// other MUF-triggered log line in this codebase is recorded.
func (h *mufHost) UserLog(player, program ref.Ref, msg string) {
	logging.On(h.s.log, logging.Muf).Info("USERLOG",
		"player", player.String(), "player_name", h.Name(player),
		"program", program.String(), "program_name", h.Name(program),
		"message", msg)
}

// resolveIgnore is ignore_is_ignoring_sub's own owner/wizard gate,
// shared by IsIgnoring, IgnoreAdd and IgnoreDel.
func (h *mufHost) resolveIgnore(player, who ref.Ref) (p, w ref.Ref, ok bool) {
	if !h.TuneBool("ignore_support") {
		return 0, 0, false
	}
	if !h.Valid(player) || !h.Valid(who) {
		return 0, 0, false
	}
	p, w = h.Owner(player), h.Owner(who)
	if p == w || h.Flags(p).IsWizard() || h.Flags(w).IsWizard() {
		return 0, 0, false
	}
	return p, w, true
}

// IsIgnoring implements muf.Host for IGNORING?, upstream's
// ignore_is_ignoring: player's owner is ignoring who's owner, or —
// when ignore_bidirectional is on — the other way around.
func (h *mufHost) IsIgnoring(player, who ref.Ref) bool {
	p, w, ok := h.resolveIgnore(player, who)
	if !ok {
		return false
	}
	if refListHas(h, p, w) {
		return true
	}
	return h.TuneBool("ignore_bidirectional") && refListHas(h, w, p)
}

// IgnoreAdd and IgnoreDel implement muf.Host for
// IGNORE_ADD/IGNORE_DEL, upstream's
// ignore_add_player/ignore_remove_player.
func (h *mufHost) IgnoreAdd(player, who ref.Ref) {
	p, w, ok := h.resolveIgnore(player, who)
	if !ok {
		return
	}
	if !refListHas(h, p, w) {
		list := append(refList(h, p), w)
		h.SetProp(p, ignoreProp, props.Value{Type: props.String, Str: joinRefs(list)})
	}
}

func (h *mufHost) IgnoreDel(player, who ref.Ref) {
	p, w, ok := h.resolveIgnore(player, who)
	if !ok {
		return
	}
	list := refList(h, p)
	out := list[:0]
	for _, r := range list {
		if r != w {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		h.RemoveProp(p, ignoreProp)
		return
	}
	h.SetProp(p, ignoreProp, props.Value{Type: props.String, Str: joinRefs(out)})
}

// refList and joinRefs read and write a reflist property — the same
// space-separated-dbrefs format internal/muf's own REFLIST_*
// primitives use, duplicated here rather than shared since those live
// in package muf and this needs no MUF Value/Frame machinery at all.
func refList(h *mufHost, obj ref.Ref) []ref.Ref {
	v, ok := h.GetProp(obj, ignoreProp)
	if !ok || v.Str == "" {
		return nil
	}
	fields := strings.Fields(v.Str)
	out := make([]ref.Ref, 0, len(fields))
	for _, f := range fields {
		if r, err := ref.Parse(f); err == nil {
			out = append(out, r)
		}
	}
	return out
}

func refListHas(h *mufHost, obj, target ref.Ref) bool {
	for _, r := range refList(h, obj) {
		if r == target {
			return true
		}
	}
	return false
}

func joinRefs(list []ref.Ref) string {
	strs := make([]string, len(list))
	for i, r := range list {
		strs[i] = r.String()
	}
	return strings.Join(strs, " ")
}

// Stats implements muf.Host for STATS and STATS_ARRAY.
func (h *mufHost) Stats(owner ref.Ref) [7]int {
	var out [7]int
	h.w.Each(func(o *world.Object) bool {
		if owner != ref.Nothing && o.Owner != owner {
			return true
		}
		idx := 0
		switch o.Type() {
		case ref.TypeRoom:
			idx = 1
		case ref.TypeExit:
			idx = 2
		case ref.TypeThing:
			idx = 3
		case ref.TypePlayer:
			idx = 4
		case ref.TypeProgram:
			idx = 5
		case ref.TypeGarbage:
			idx = 6
		default:
			return true
		}
		out[idx]++
		out[0]++
		return true
	})
	return out
}

var _ muf.Host = (*mufHost)(nil)
