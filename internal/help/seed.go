package help

import (
	"slices"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Apply seeds a world's help corpora from the built-in content and
// reports which ones it changed. It marks what it writes, so the next
// flush persists it.
//
// There are three cases. An empty corpus is filled outright, which is
// what a fresh world needs. With force, every corpus is replaced,
// which is what "fbemerald help-seed -force" is for. Otherwise a
// corpus is only touched when the seed version has moved on, and then
// only its untouched topics are refreshed — a topic a wizard has
// edited is left exactly as they left it, and a topic they added is
// kept.
//
// "Untouched" is Modified being zero, which is how a seeded topic is
// written and what any edit through World.SetHelpTopic replaces.
func Apply(w *world.World, seededVersion string, force bool) []string {
	var changed []string
	for _, corpus := range world.Corpora() {
		built, err := Seed(corpus)
		if err != nil || len(built) == 0 {
			continue
		}
		cur := w.HelpTopics(corpus)

		var next []world.HelpTopic
		switch {
		case force || len(cur) == 0:
			next = built
		case seededVersion == Version:
			continue
		default:
			next = merge(cur, built)
		}
		if sameTopics(cur, next) {
			continue
		}
		w.ReplaceHelp(corpus, next)
		changed = append(changed, corpus)
	}
	return changed
}

// merge takes the new content for every topic nobody has edited, and
// keeps everything else.
//
// The result is ordered by the built-in content, with a world's own
// additions after it. That keeps a refreshed corpus reading the way
// the release intended while never losing a local topic.
func merge(cur, built []world.HelpTopic) []world.HelpTopic {
	byName := make(map[string]world.HelpTopic, len(cur))
	for _, t := range cur {
		byName[ascii.Fold(t.Name)] = t
	}

	out := make([]world.HelpTopic, 0, len(cur)+len(built))
	taken := make(map[string]bool, len(built))
	for _, b := range built {
		k := ascii.Fold(b.Name)
		taken[k] = true
		if old, ok := byName[k]; ok && old.Modified != 0 {
			old.Ord = len(out)
			out = append(out, old)
			continue
		}
		b.Ord = len(out)
		out = append(out, b)
	}
	for _, t := range cur {
		if taken[ascii.Fold(t.Name)] {
			continue
		}
		t.Ord = len(out)
		out = append(out, t)
	}
	return out
}

// sameTopics reports whether seeding would be a no-op, so an
// unchanged corpus is not rewritten on every boot.
func sameTopics(a, b []world.HelpTopic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Body != b[i].Body ||
			!slices.Equal(a[i].Aliases, b[i].Aliases) {
			return false
		}
	}
	return true
}
