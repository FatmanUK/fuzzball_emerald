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

// Tag is the type as @tune prints it, upstream's str_tunetype
// (tunelist.h:24). It is not Type.String(): that names the type for
// SYSPARM_ARRAY, which MUF reads, and these are the six-column labels
// a listing is padded to.
func (t Type) Tag() string {
	switch t {
	case TypeString:
		return "(str)"
	case TypeTimespan:
		return "(time)"
	case TypeInteger:
		return "(int)"
	case TypeDbref:
		return "(ref)"
	case TypeBoolean:
		return "(bool)"
	default:
		return "(?)"
	}
}

// modules names the optional subsystems this server has, which is
// upstream's compile_options string (game.c:66). A parameter whose
// Module is not in it does nothing here, and a listing marks it
// "[inactive]" — upstream's own word for a parameter that is
// present because MUF looks parameters up by name, but whose feature
// was compiled out.
//
// Emerald has MCP and nothing else on that list: DISKBASE is replaced
// by Postgres, MEMPROF is malloc profiling, and RESOLVER is
// upstream's separate resolver process where Emerald resolves names
// on its own goroutines.
var modules = map[string]bool{"MCP": true}

// Active reports whether this server acts on a parameter at all.
//
// It is MOD_ENABLED (game.h:30) and nothing more. Emerald's own Inert
// set — the dump_* family and the file_* names — is deliberately
// not folded in: upstream prints no marker for those, `fbemerald
// tune` marks them, and the configurator explains them.
func (p *Param) Active() bool {
	return p.Module == "" || modules[p.Module]
}

// SetResult is tune_setparm's answer (tune.c:402). Every one of the
// six has its own message, and @tune and SETSYSPARM word them
// differently, so the code is reported rather than a string.
type SetResult int

const (
	SetSuccess SetResult = iota
	SetSuccessDefault
	SetUnknown
	SetSyntax
	SetBadVal
	SetDenied
)

// ResetFlag is upstream's TP_FLAG_DEFAULT: a parameter name prefixed
// with it is reset to its compiled-in default rather than set.
const ResetFlag = '%'

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
	// and to change the parameter. Either may be MLevGod, which
	// only TUNE_MLEV ever hands out.
	ReadMLev  int
	WriteMLev int

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

// SetParm is tune_setparm (tune.c:402), which is what @tune and
// SETSYSPARM both go through and is stricter than Parse in three ways
// that are visible to anybody typing a value.
//
// A boolean reads only its **first character**: y, Y or 1 is true, n,
// N or 0 is false, and anything else is a syntax error — so "yes"
// works, "yellow" also works, and "true" does not. An integer is
// `number()`: optional sign then digits only, no "0x10" and no "3.5".
// A timespan is tune_timespan_seconds, which wants either "<days>d
// <h>:<mm>:<ss>" or a run of unit suffixes like "1d12h", and rejects
// a bare number of seconds outright.
//
// A dbref is matched rather than parsed, through resolve — which is
// upstream's match_absolute, match_registered, match_player,
// match_me, match_here, in that order, and notably not the room's
// contents or the player's. A failed match is a syntax error and a
// wrong object type is a bad value.
//
// The order of the checks is upstream's and is observable: the write
// permission is tested before anything else, the reset-to-default
// before the empty-value check, so resetting a non-nullable string
// with no value given succeeds where setting it would not.
func (s *Set) SetParm(name, val string, mlev int,
	resolve func(string) (ref.Ref, ref.ObjType, bool)) SetResult {

	reset := false
	flag := string(ResetFlag)
	if rest, ok := strings.CutPrefix(name, flag); ok {
		name, reset = rest, true
	}
	p, ok := Lookup(name)
	if !ok {
		return SetUnknown
	}
	if p.WriteMLev > mlev {
		return SetDenied
	}
	if reset {
		s.vals[p.Name] = p.Default
		s.dflt[p.Name] = true
		return SetSuccessDefault
	}
	if !p.Nullable && val == "" {
		return SetBadVal
	}

	var v Value
	switch p.Type {
	case TypeString:
		v = Value{Str: val}

	case TypeBoolean:
		switch val[0] {
		case 'y', 'Y', '1':
			v = Value{Bool: true}
		case 'n', 'N', '0':
			v = Value{Bool: false}
		default:
			return SetSyntax
		}

	case TypeInteger:
		if !isNumber(val) {
			return SetSyntax
		}
		n, err := strconv.ParseInt(
			strings.TrimSpace(val), 10, 64)
		if err != nil {
			return SetSyntax
		}
		v = Value{Num: n}

	case TypeTimespan:
		secs, ok := TuneTimespanSeconds(val)
		if !ok {
			return SetSyntax
		}
		v = Value{Span: time.Duration(secs) * time.Second}

	case TypeDbref:
		if resolve == nil {
			return SetSyntax
		}
		r, ty, ok := resolve(val)
		if !ok {
			return SetSyntax
		}
		if p.HasObjType && ty != p.ObjType {
			return SetBadVal
		}
		v = Value{Ref: r}

	default:
		return SetSyntax
	}

	s.vals[p.Name] = v
	s.dflt[p.Name] = false
	return SetSuccess
}

// isNumber is fbstrings.c's number(): leading whitespace, an optional
// sign, then digits and nothing else. An empty run of digits is not a
// number, so "-" is rejected.
func isNumber(s string) bool {
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// TuneTimespanSeconds is tune.c's tune_timespan_seconds, which is not
// ParseTimespan.
//
// It tries "<days>d <hh>:<mm>:<ss>" first — the form a listing
// prints and a dump stores — and otherwise walks the string
// accumulating digits and applying each d/h/m/s suffix as it meets
// it, so "1d12h" and "90m" both work. Anything else is a failure, and
// so is a total of zero: a bare "3600" has no suffix, accumulates
// nothing, and is rejected, which is why a timespan cannot be set in
// plain seconds.
func TuneTimespanSeconds(value string) (int64, bool) {
	var days, hrs, mins, secs int64
	if n, err := fmt.Sscanf(value, "%dd %2d:%2d:%2d",
		&days, &hrs, &mins, &secs); err == nil && n == 4 {
		return days*86400 + hrs*3600 + mins*60 + secs, true
	}

	var total, subtotal int64
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= '0' && c <= '9' {
			subtotal = subtotal*10 + int64(c-'0')
			continue
		}
		switch c | 0x20 {
		case 'd':
			total += subtotal * 86400
		case 'h':
			total += subtotal * 3600
		case 'm':
			total += subtotal * 60
		case 's':
			total += subtotal
		default:
			return 0, false
		}
		subtotal = 0
	}
	if total == 0 {
		return 0, false
	}
	return total, true
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

// MLevGod is upstream's MLEV_GOD under GOD_PRIV, which
// include/config.h defines by default: an overkill level that only
// TUNE_MLEV hands out, and then only to #1.
const MLevGod = 255

// GodOnly reports whether a plain wizard may not write a parameter,
// which upstream expresses as MLEV_GOD on its write level.
//
// This was a generated field, back when the generator collapsed
// MLEV_GOD to MLEV_WIZARD and had to record the distinction
// somewhere. The levels carry it now, so deriving it is the one
// spelling that cannot disagree with them.
func (p Param) GodOnly() bool { return p.WriteMLev >= MLevGod }

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
