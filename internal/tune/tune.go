// Package tune holds the @tune parameter table and the live values a
// running server reads from it.
//
// Parameter names are a public API, not labels: the MUF primitives
// SYSPARM, SETSYSPARM and SYSPARM_ARRAY look them up by string at
// runtime, so renaming one silently breaks third-party MUF. Names are
// preserved verbatim from Fuzzball 7 except where the plan records a
// deliberate divergence, and those carry a LegacyName so the old
// spelling keeps resolving.
package tune

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Type is a parameter's value type.
type Type int

const (
	TypeString Type = iota
	TypeTimespan
	TypeInteger
	TypeDbref
	TypeBoolean
)

func (t Type) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeTimespan:
		return "timespan"
	case TypeInteger:
		return "integer"
	case TypeDbref:
		return "dbref"
	case TypeBoolean:
		return "boolean"
	default:
		return "unknown"
	}
}

// Value holds a parameter value. Only the field matching the
// parameter's Type is meaningful.
type Value struct {
	Str  string
	Span time.Duration
	Num  int64
	Ref  ref.Ref
	Bool bool
}

// Param describes a single tunable parameter.
type Param struct {
	Name    string
	Label   string
	Group   string
	Module  string
	Type    Type
	Default Value

	// ReadMLev and WriteMLev are the mucker levels needed to see
	// and to change the parameter.
	ReadMLev  int
	WriteMLev int

	// GodOnly marks parameters that upstream gated behind
	// MLEV_GOD, which collapses to MLEV_WIZARD unless the server
	// is built with GOD_PRIV.
	GodOnly bool

	// Nullable marks string parameters that accept an empty
	// value.
	Nullable bool

	// ObjType constrains a dbref parameter to one object type.
	ObjType    ref.ObjType
	HasObjType bool

	// LegacyName is the Fuzzball 7 spelling, when Emerald renamed
	// this parameter. Lookups by the old name still resolve, so
	// existing MUF keeps working.
	LegacyName string

	// Inert, when non-empty, explains why this parameter no
	// longer affects the server. It is still readable and
	// settable so MUF that consults it does not break.
	Inert string
}

// byName indexes params by both current and legacy name.
var byName = func() map[string]*Param {
	m := make(map[string]*Param, len(params)*2)
	for i := range params {
		p := &params[i]
		m[p.Name] = p
		if p.LegacyName != "" {
			m[p.LegacyName] = p
		}
	}
	return m
}()

// Lookup finds a parameter by name, case-insensitively. It resolves
// legacy names too. The bool reports whether the name was found.
func Lookup(name string) (*Param, bool) {
	p, ok := byName[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// DroppedReplacement reports whether name is a Fuzzball 7 parameter
// that Emerald deliberately does not implement, and names the
// environment variable that replaced it. An empty replacement means
// the concept is simply gone.
func DroppedReplacement(name string) (string, bool) {
	env, ok := droppedParams[strings.ToLower(strings.TrimSpace(name))]
	return env, ok
}

// Params returns every parameter, ordered by name.
func Params() []Param {
	out := make([]Param, len(params))
	copy(out, params)
	return out
}

// Set is a live table of parameter values. It is not safe for
// concurrent use; the world goroutine owns it.
type Set struct {
	vals map[string]Value
	// dflt tracks which parameters still hold their default,
	// which is what decides the leading '%' when a dump header is
	// written.
	dflt map[string]bool
}

// NewSet returns a Set with every parameter at its default.
func NewSet() *Set {
	s := &Set{
		vals: make(map[string]Value, len(params)),
		dflt: make(map[string]bool, len(params)),
	}
	for i := range params {
		s.vals[params[i].Name] = params[i].Default
		s.dflt[params[i].Name] = true
	}
	return s
}

// Params returns every parameter, ordered by name. It is a method as
// well as a package function so callers holding only a Set can
// enumerate.
func (s *Set) Params() []Param { return Params() }

// Get returns the current value of a parameter.
func (s *Set) Get(name string) (Value, bool) {
	p, ok := Lookup(name)
	if !ok {
		return Value{}, false
	}
	v, ok := s.vals[p.Name]
	return v, ok
}

// IsDefault reports whether a parameter still holds its default
// value.
func (s *Set) IsDefault(name string) bool {
	p, ok := Lookup(name)
	if !ok {
		return false
	}
	return s.dflt[p.Name]
}

// Typed accessors. Each panics if the named parameter does not exist
// or is of the wrong type, because every call site names a
// compile-time constant and a mismatch is a bug in the server, not
// bad input.

func (s *Set) String(name string) string {
	return s.must(name, TypeString).Str
}
func (s *Set) Bool(name string) bool {
	return s.must(name, TypeBoolean).Bool
}
func (s *Set) Int(name string) int64 {
	return s.must(name, TypeInteger).Num
}
func (s *Set) Ref(name string) ref.Ref {
	return s.must(name, TypeDbref).Ref
}
func (s *Set) Duration(name string) time.Duration {
	return s.must(name, TypeTimespan).Span
}

func (s *Set) must(name string, want Type) Value {
	p, ok := Lookup(name)
	if !ok {
		panic("tune: unknown parameter " + name)
	}
	if p.Type != want {
		panic(fmt.Sprintf("tune: parameter %s is %s, read as %s", p.Name, p.Type, want))
	}
	return s.vals[p.Name]
}

// SetValue stores a pre-parsed value, marking the parameter
// non-default.
func (s *Set) SetValue(name string, v Value) error {
	p, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown parameter %q", name)
	}
	s.vals[p.Name] = v
	s.dflt[p.Name] = false
	return nil
}

// Reset returns a parameter to its default.
func (s *Set) Reset(name string) error {
	p, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown parameter %q", name)
	}
	s.vals[p.Name] = p.Default
	s.dflt[p.Name] = true
	return nil
}

// SetString parses a textual value the way @tune and a dump header
// do, then stores it.
func (s *Set) SetString(name, raw string) error {
	p, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown parameter %q", name)
	}
	v, err := p.Parse(raw)
	if err != nil {
		return err
	}
	s.vals[p.Name] = v
	s.dflt[p.Name] = false
	return nil
}

// Parse converts a textual value into a Value for this parameter.
func (p *Param) Parse(raw string) (Value, error) {
	switch p.Type {
	case TypeString:
		if raw == "" && !p.Nullable {
			return Value{}, fmt.Errorf("%s: value may not be empty", p.Name)
		}
		return Value{Str: raw}, nil

	case TypeBoolean:
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "yes", "y", "true", "1", "on":
			return Value{Bool: true}, nil
		case "no", "n", "false", "0", "off":
			return Value{Bool: false}, nil
		}
		return Value{}, fmt.Errorf("%s: %q is not a boolean", p.Name, raw)

	case TypeInteger:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return Value{}, fmt.Errorf("%s: %q is not an integer", p.Name, raw)
		}
		return Value{Num: n}, nil

	case TypeDbref:
		r, err := ref.Parse(raw)
		if err != nil {
			return Value{}, fmt.Errorf("%s: %w", p.Name, err)
		}
		return Value{Ref: r}, nil

	case TypeTimespan:
		d, err := ParseTimespan(raw)
		if err != nil {
			return Value{}, fmt.Errorf("%s: %w", p.Name, err)
		}
		return Value{Span: d}, nil
	}
	return Value{}, fmt.Errorf("%s: unhandled parameter type", p.Name)
}

// Format renders a value the way a dump header stores it.
func (p *Param) Format(v Value) string {
	switch p.Type {
	case TypeString:
		return v.Str
	case TypeBoolean:
		if v.Bool {
			return "yes"
		}
		return "no"
	case TypeInteger:
		return strconv.FormatInt(v.Num, 10)
	case TypeDbref:
		return v.Ref.String()
	case TypeTimespan:
		return FormatTimespan(v.Span)
	}
	return ""
}

// ParseTimespan reads Fuzzball's " 0d 4:00:00" timespan form. A bare
// number of seconds is also accepted, which is what MUF SETSYSPARM
// tends to pass.
func ParseTimespan(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty timespan")
	}

	var days int64
	if i := strings.IndexByte(s, 'd'); i >= 0 {
		d, err := strconv.ParseInt(strings.TrimSpace(s[:i]), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad day count in timespan %q", raw)
		}
		days = d
		s = strings.TrimSpace(s[i+1:])
	}

	total := time.Duration(days) * 24 * time.Hour
	if s == "" {
		return total, nil
	}

	// Remaining text is h:m:s, m:s, or a bare second count.
	fields := strings.Split(s, ":")
	if len(fields) > 3 {
		return 0, fmt.Errorf("bad timespan %q", raw)
	}
	units := []time.Duration{time.Hour, time.Minute, time.Second}
	// Right-align: "5:00" is minutes and seconds, not hours and
	// minutes.
	units = units[len(units)-len(fields):]
	for i, f := range fields {
		n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timespan %q", raw)
		}
		total += time.Duration(n) * units[i]
	}
	return total, nil
}

// FormatTimespan renders a duration in Fuzzball's dump form, e.g. "
// 90d 0:00:00".
func FormatTimespan(d time.Duration) string {
	secs := int64(d / time.Second)
	days := secs / 86400
	secs %= 86400
	return fmt.Sprintf("%3dd %2d:%02d:%02d", days, secs/3600, (secs%3600)/60, secs%60)
}

// Groups lists the distinct parameter groups, ordered.
func Groups() []string {
	seen := map[string]bool{}
	var out []string
	for i := range params {
		if g := params[i].Group; g != "" && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}
