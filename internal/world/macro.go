package world

import (
	"sort"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Macro is one entry in the MUF editor's macro table, upstream's muf/macros
// file. A program reaches a macro by prefixing its name with '.', and the
// expansion happens in the compiler's token stream.
type Macro struct {
	Name       string
	Definition string
	Owner      ref.Ref
}

// SetMacros replaces the whole macro table without marking it for writing,
// which is what loading from the store wants.
func (w *World) SetMacros(list []Macro) {
	w.macros = make(map[string]Macro, len(list))
	for _, m := range list {
		w.macros[ascii.Fold(m.Name)] = m
	}
}

// Macros returns the table in name order, as upstream's alphabetical dump has
// it.
func (w *World) Macros() []Macro {
	out := make([]Macro, 0, len(w.macros))
	for _, m := range w.macros {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return ascii.Compare(out[i].Name, out[j].Name) < 0
	})
	return out
}

// MacroTable returns the folded name to definition mapping the compiler wants.
func (w *World) MacroTable() map[string]string {
	out := make(map[string]string, len(w.macros))
	for k, m := range w.macros {
		out[k] = m.Definition
	}
	return out
}

// DefineMacro adds an entry, reporting false if the name is taken. Upstream's
// insert_macro refuses to overwrite: a macro is changed by deleting it first.
func (w *World) DefineMacro(name, definition string, owner ref.Ref) bool {
	k := ascii.Fold(name)
	if _, taken := w.macros[k]; taken {
		return false
	}
	w.macros[k] = Macro{Name: name, Definition: definition, Owner: owner}
	w.macrosDirty = true
	return true
}

// KillMacro removes an entry, reporting whether there was one.
func (w *World) KillMacro(name string) bool {
	k := ascii.Fold(name)
	if _, ok := w.macros[k]; !ok {
		return false
	}
	delete(w.macros, k)
	w.macrosDirty = true
	return true
}

// ChownMacros reassigns every macro owned by one object to another, which
// deleting a player has to do: a macro outlives the player who defined it.
func (w *World) ChownMacros(from, to ref.Ref) {
	for k, m := range w.macros {
		if m.Owner == from {
			m.Owner = to
			w.macros[k] = m
			w.macrosDirty = true
		}
	}
}
