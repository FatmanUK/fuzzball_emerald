// Package mpi implements MPI, the macro language Fuzzball evaluates
// inside property values.
//
// MPI is not a stack language like MUF. A property's text is scanned
// for "{name:arg,arg}" calls, which are substituted in place and may
// nest. It is what makes a description say the viewer's name back to
// them.
package mpi

// Func describes one MPI function: its name, how many arguments it
// takes, and how the parser should treat them.
type Func struct {
	Name string
	// Parse evaluates the arguments before the function sees
	// them. It is off for the ones that decide for themselves,
	// such as {if} and {and}, which must not evaluate a branch
	// they will not use.
	Parse bool
	// PostParse evaluates whatever the function returns.
	PostParse bool
	// Strip trims spaces from the arguments.
	Strip bool
	// Min and Max bound the argument count. A Max of 9 means "as
	// many as the syntax allows".
	Min, Max int
}

// Lookup finds a function by name, case-insensitively.
func Lookup(name string) (*Func, bool) {
	f, ok := functions[upper(name)]
	return f, ok
}

// Count is how many functions the table holds.
func Count() int { return len(functions) }

// upper folds a name to the upper case the table is keyed by. MPI
// names are ASCII, and upstream compares them with strcasecmp.
func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
