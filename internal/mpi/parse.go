package mpi

import (
	"fmt"
	"strings"
)

// Lexical characters, from include/mpi.h.
const (
	litChar   = '`' // toggles literal mode
	leadChar  = '{'
	argStart  = ':'
	argSep    = ','
	argEnd    = '}'
	escape    = '\\'
	escapeSeq = 27 // what "\[" produces
)

// Limits, from include/mpi.h.
const (
	maxFuncNameLen = 16
	maxVariables   = 32
	recursionLimit = 26
	// bufferLen is BUFFER_LEN (config.h:131), which bounds a
	// justification fieldwidth.
	bufferLen       = 8192
	maxArgs         = 9
	defaultMaxInstr = 2048
)

// Env is what an evaluation runs against: the objects involved, the
// variables in scope, and the world it may reach.
type Env struct {
	// Who is the player the message is for, What the object
	// carrying it, and Perms the object whose permissions apply.
	Who, What, Perms Ref

	// Blessed grants wizard permissions, which a blessed property
	// has.
	Blessed bool
	// Type is what triggered this evaluation, upstream's mesgtyp.
	// It is not decoration: {tell} and {otell} refuse outright
	// from a listener on anything but a room, and {delay} carries
	// it forward to whatever it schedules.
	Type MesgType
	// Descr is the connection this is being evaluated for.
	Descr int

	// Host reaches the world.
	Host Host

	// MaxInstructions bounds how many function calls one
	// evaluation may make. Zero uses the default.
	MaxInstructions int

	vars  []variable
	depth int
	instr int

	// funcs holds the macros {func} defines, keyed by upper-cased
	// name. They live only as long as one evaluation.
	funcs map[string]string

	// notes collects the messages a failing call produces, which
	// upstream sends to the player as it goes.
	notes []string
}

// variable is one {&name} binding.
type variable struct {
	name  string
	value string
}

// MesgType is upstream's mesgtyp word (include/mpi.h): what kind of
// thing triggered an evaluation.
//
// Blessing is deliberately *not* in here even though upstream keeps
// it in the same word, because it is a permission rather than a
// provenance and it is already Env.Blessed — and because {revoke}
// drops the blessing while leaving everything else alone.
type MesgType uint8

const (
	// Private is a message meant for one player, which is what a
	// description or a @succ is. Upstream's ISPRIVATE.
	Private MesgType = 1 << iota
	// Listener is a message triggered by a listen propqueue. This
	// is the one that actually refuses things.
	Listener
	// Lock is an evaluation triggered by a lock.
	Lock
)

// Has reports whether a flag is set.
func (t MesgType) Has(f MesgType) bool { return t&f != 0 }

// Public is the absence of Private, which is how upstream spells it:
// MPI_ISPUBLIC is zero and is never tested for directly.
func (t MesgType) Public() bool { return !t.Has(Private) }

// Ref is a database reference. It is an alias so this package does
// not depend on the world's own type for what is only an identifier.
type Ref = int32

// Host is what MPI can reach outside itself.
type Host interface {
	// Name returns an object's name.
	Name(obj Ref) string
	// GetPropStr reads a property's string value.
	GetPropStr(obj Ref, path string) string
	// SetPropStr writes one, and DelProp removes one.
	SetPropStr(obj Ref, path, val string)
	DelProp(obj Ref, path string)
	// PropChildren lists the names directly under a property
	// path.
	PropChildren(obj Ref, path string) []string
	// BlessProp sets or clears a property's blessed flag.
	BlessProp(obj Ref, path string, blessed bool)
	// Location, Owner and Contents answer the obvious questions.
	Location(obj Ref) Ref
	Owner(obj Ref) Ref
	Contents(obj Ref) []Ref
	// Parent is one step out in the environment tree, upstream's
	// getparent: a property search walks it until something
	// answers.
	Parent(obj Ref) Ref
	// Valid reports whether a ref names a live object.
	Valid(obj Ref) bool
	// IsPlayer reports whether it is a player, and Online whether
	// they are connected.
	IsPlayer(obj Ref) bool
	Online(obj Ref) bool
	// Match resolves a name the way a player's command would.
	Match(who Ref, name string) Ref
	// Notify sends a line to an object's connections, and
	// NotifyExcept to everything in a room but the objects named.
	Notify(obj Ref, msg string)
	NotifyExcept(room Ref, except []Ref, msg string)
	// Now is the server's clock, as a Unix time.
	Now() int64

	// TypeName is an object's type as one of upstream's own words
	// — "Room", "Exit", "Thing", "Player", "Program", "Bad" —
	// for {type}.
	TypeName(obj Ref) string
	// FlagString is the letters examine prints for an object's
	// flags, and HasFlag tests one by name or letter.
	FlagString(obj Ref) string
	HasFlag(obj Ref, flag string) bool
	// Exits and Links list what an object holds and points at: an
	// exit's destinations, a room's drop-to, or a thing's or
	// player's home.
	Exits(obj Ref) []Ref
	Links(obj Ref) []Ref
	// Value is an object's currency, for {money}.
	Value(obj Ref) int
	// Timestamps is when an object was made, last changed and
	// last used, and how often — {created}, {modified},
	// {lastused}, {usecount}.
	Timestamps(obj Ref) (created, modified, used int64, count int)

	// Controls reports whether who has ownership-level authority
	// over target, and Locked whether player is locked out of
	// thing.
	Controls(who, target Ref) bool
	Locked(descr int, player, thing Ref) bool
	// ForceLevel is how deep a chain of forces already is, which
	// bounds {force} against max_force_level. Upstream's is a
	// global, force_level.
	ForceLevel() int
	// PlayerNamed reports whether a player of that exact name
	// exists, which is lookup_player. {force} needs it to refuse
	// a puppet whose first word is somebody's name, so that a
	// thing cannot be made to impersonate a player.
	PlayerNamed(name string) bool
	// ForceLockPasses evaluates the force lock (`@/flk`) on obj
	// against who, defaulting to **false** when none is set —
	// test_lock_false_default (boolexp.c:906), which returns 0
	// for an unset lock rather than passing it.
	ForceLockPasses(descr int, who, obj Ref) bool

	// TestLock evaluates a lock expression written as text.
	TestLock(descr int, player, thing Ref, lock string) (bool, error)

	// OnlinePlayers lists who is connected, and Idle and OnTime
	// report how long one connection has been quiet and how long
	// it has been open.
	OnlinePlayers() []Ref
	Idle(obj Ref) int
	OnTime(obj Ref) int
	// Width and Height are a connection's reported terminal size,
	// found from an object — upstream's mfn_width and
	// mfn_height resolve a player to a descriptor themselves.
	Width(obj Ref) int
	Height(obj Ref) int
	// DescrWidth is a *descriptor's* reported width, which is a
	// different question: the justification functions read
	// `d->detected_width` off the descriptor the evaluation is
	// running for rather than looking a player up, so a player
	// with two connections justifies to the one being written to.
	// Zero when the descriptor reports none.
	DescrWidth(descr int) int

	// TuneGet reads an @tune parameter as its formatted string,
	// **ungated**, which is how upstream reads the ones it keeps
	// in C globals: tp_gender_prop and the server-policy
	// parameters tuneBool and tuneInt ask for. It is not
	// tune_get_parmstring and must not be used for {sysparm}.
	TuneGet(name string) (string, bool)
	// TuneGetParm is tune_get_parmstring (tune.c:341), which
	// answers the empty string when the parameter's read level is
	// above mlev. {sysparm} is its only caller here, and used to
	// call TuneGet instead -- so a mortal could read all 55
	// parameters gated at wizard level or above, smtp_password
	// among them, by putting {sysparm:smtp_password} in their own
	// description and looking at themselves.
	TuneGetParm(name string, mlev int) (string, bool)
	// TuneMLevel is TUNE_MLEV (include/db.h:669): 255 for God and
	// the player's own level otherwise. mfn_sysparm passes it
	// (mfuns.c:4141).
	TuneMLevel(who Ref) int
	MuckName() string
	// PronounSub substitutes the pronoun directives in a string
	// for an object's gender.
	PronounSub(obj Ref, text string) string

	// Force runs a command as another object, Kill removes a
	// process, and RunMUF runs a program and returns what it left
	// on its stack. All three are blessed-only, checked by the
	// functions rather than here.
	Force(descr int, who Ref, command string)
	Kill(pid int) bool
	// RunMUF's perms is what the run is attributed to and becomes
	// the program's TRIGGER, and how is the calling context —
	// {&how} — which upstream puts in COMMAND with "(MPI)"
	// after it.
	RunMUF(descr int, player, prog, perms Ref,
		how, arg string) (string, error)
	// Delay schedules a message to be evaluated later, for
	// {delay}.
	Delay(descr int, player, what, perms Ref, seconds int, text string, blessed bool)
}

// Error is an MPI failure.
type Error struct {
	Func string
	Msg  string
	// Trail is the enclosing calls whose *argument parsing*
	// failed because of this error, innermost first.
	//
	// Upstream reports a nested MPI failure as several lines, not
	// one: the innermost ABORT_MPI notifies its own message, and
	// then every enclosing function that was pre-parsing an
	// argument notifies "{NAME} (arg N)" as the NULL propagates
	// out (msgparse.c:1637). So "{null:{force:me,x}}" prints the
	// {FORCE} refusal *and* " {NULL} (arg 1)", where this server
	// printed the first line and stopped.
	//
	// Emerald reports a failure once, from Eval, so the chain is
	// carried on the error rather than notified as it unwinds —
	// which also keeps the lines in upstream's order. The frames
	// carry no colon, unlike the message line.
	Trail []string
}

func (e *Error) Error() string {
	if e.Func != "" {
		return fmt.Sprintf("%c%s%c: %s", leadChar, e.Func, argEnd, e.Msg)
	}
	return e.Msg
}

// errf builds an MPI error.
func errf(fn, format string, args ...any) *Error {
	return &Error{Func: fn, Msg: fmt.Sprintf(format, args...)}
}

// Eval evaluates a message, reporting any failure to the player and
// yielding empty text.
//
// That is upstream's behaviour and it matters: an MPI error in a
// description must not abort the program or command that was reading
// it. Parse is the inner form, which propagates the error so nested
// calls can stop.
func Eval(env *Env, in string) string {
	out, err := Parse(env, in)
	if err == nil {
		return out
	}
	// The "how" variable names what is being evaluated, and
	// prefixes the message. It is empty unless the caller set
	// one, which leaves the leading space upstream also produces.
	how, _ := env.Var("how")
	env.Host.Notify(env.Who, how+" "+err.Error())
	// Then one line per enclosing call that was parsing an
	// argument when this failed, innermost first.
	if e, ok := err.(*Error); ok {
		for _, frame := range e.Trail {
			env.Host.Notify(env.Who, how+" "+frame)
		}
	}
	return ""
}

// Parse evaluates a message, substituting every function call in it.
func Parse(env *Env, in string) (string, error) {
	env.depth++
	defer func() { env.depth-- }()

	if env.depth > recursionLimit {
		return "", errf("", "Recursion limit exceeded.")
	}

	var out strings.Builder
	literal := false

	for i := 0; i < len(in); i++ {
		c := in[i]

		switch {
		case c == escape && i+1 < len(in):
			// An escape passes the next character
			// through, with two spellings that mean
			// something else.
			i++
			switch in[i] {
			case 'r':
				out.WriteByte('\r')
			case '[':
				out.WriteByte(escapeSeq)
			default:
				out.WriteByte(in[i])
			}

		case c == litChar:
			// A backtick toggles literalness rather than
			// being output, so a run of text can hold
			// braces.
			literal = !literal

		case !literal && c == leadChar:
			if i+1 < len(in) && in[i+1] == leadChar {
				// "{{" is a literal brace.
				out.WriteByte(leadChar)
				i++
				continue
			}
			call, next, err := env.evalCall(in, i)
			if err != nil {
				return "", err
			}
			out.WriteString(call)
			i = next - 1

		default:
			out.WriteByte(c)
		}
	}
	return out.String(), nil
}

// evalCall evaluates the call starting at in[start], which is its
// '{'. It returns what the call produced and the index just past its
// '}'.
func (env *Env) evalCall(in string, start int) (string, int, error) {
	i := start + 1

	// The name runs until a delimiter. A '&' prefix names a
	// variable rather than a function.
	nameStart := i
	for i < len(in) && in[i] != leadChar && in[i] != argStart &&
		in[i] != argEnd && !isSpace(in[i]) {
		i++
	}
	name := in[nameStart:i]

	// The limit allows one extra character for the '&', which is
	// not part of the name being looked up.
	limit := maxFuncNameLen
	if strings.HasPrefix(name, "&") {
		limit++
	}
	if len(name) > limit || i >= len(in) ||
		(in[i] != argStart && in[i] != argEnd) {
		// Not a call at all, so the brace is ordinary text.
		return string(leadChar), start + 1, nil
	}

	if err := env.charge(name); err != nil {
		return "", 0, err
	}

	// A variable reference takes the variable's value as its
	// first argument and behaves as {sublist} otherwise.
	varName := ""
	if strings.HasPrefix(name, "&") {
		varName = name[1:]
		name = "sublist"
	}

	fn, known := Lookup(name)

	// Collect the raw arguments.
	var args []string
	end := i
	if in[i] == argEnd {
		end = i + 1
	} else {
		var err error
		args, end, err = splitArgs(in, i+1)
		if err != nil {
			return "", 0, errf(name, "%s", err.Error())
		}
	}

	if !known {
		// A name the built-in table does not hold may still
		// be a macro — one {func} defined, or one stored in
		// a property.
		body, found := env.macro(name)
		if !found {
			return "", 0, errf(name, "Unrecognized function.")
		}
		out, err := env.expandMacro(body, args)
		return out, end, err
	}

	if varName != "" {
		value, ok := env.Var(varName)
		if !ok {
			return "", 0, errf("&"+varName, "Unrecognized variable.")
		}
		args = append([]string{value}, args...)
	}

	if fn.Strip {
		for j := range args {
			args[j] = strings.TrimSpace(args[j])
		}
	}
	if fn.Parse {
		for j := range args {
			evaluated, err := Parse(env, args[j])
			if err != nil {
				return "", 0, withFrame(err,
					fn.Name, j+1)
			}
			args[j] = evaluated
		}
	}

	if len(args) < fn.Min {
		return "", 0, errf(fn.Name, "Too few arguments.")
	}
	if fn.Max > 0 && len(args) > fn.Max {
		return "", 0, errf(fn.Name, "Too many arguments.")
	}

	impl, ok := impls[fn.Name]
	if !ok {
		return "", 0, errf(fn.Name, "Not implemented yet.")
	}
	result, err := impl(env, fn, args)
	if err != nil {
		return "", 0, err
	}

	if fn.PostParse {
		result, err = Parse(env, result)
		if err != nil {
			return "", 0, err
		}
	}
	return result, end, nil
}

// charge counts a call against the instruction limit.
func (env *Env) charge(name string) error {
	limit := env.MaxInstructions
	if limit <= 0 {
		limit = defaultMaxInstr
	}
	env.instr++
	if env.instr > limit {
		return errf(name, "Instruction limit exceeded.")
	}
	return nil
}

// splitArgs reads a call's comma-separated arguments, starting just
// past the ':'. It returns them unevaluated and the index past the
// closing '}'.
//
// Nesting and literalness are respected, so a comma inside a nested
// call or a backtick-quoted run does not separate arguments.
func splitArgs(in string, start int) ([]string, int, error) {
	var args []string
	var cur strings.Builder
	depth := 0
	literal := false

	for i := start; i < len(in); i++ {
		c := in[i]
		switch {
		case c == escape && i+1 < len(in):
			cur.WriteByte(c)
			i++
			cur.WriteByte(in[i])
		case c == litChar:
			literal = !literal
			cur.WriteByte(c)
		case literal:
			cur.WriteByte(c)
		case c == leadChar:
			depth++
			cur.WriteByte(c)
		case c == argEnd:
			if depth == 0 {
				args = append(args, cur.String())
				return args, i + 1, nil
			}
			depth--
			cur.WriteByte(c)
		case c == argSep && depth == 0:
			args = append(args, cur.String())
			cur.Reset()
			if len(args) > maxArgs {
				return nil, 0, fmt.Errorf("Too many arguments.")
			}
		default:
			cur.WriteByte(c)
		}
	}
	return nil, 0, fmt.Errorf("End brace not found.")
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// Var reads a variable.
func (env *Env) Var(name string) (string, bool) {
	for i := len(env.vars) - 1; i >= 0; i-- {
		if upper(env.vars[i].name) == upper(name) {
			return env.vars[i].value, true
		}
	}
	return "", false
}

// BindVar pushes a new variable, and is new_mvar (msgparse.c:871).
//
// **Binding is not assignment, and conflating the two was a bug.**
// Upstream's varv is a stack and new_mvar appends to it without
// looking for the name first, so binding a name that is already bound
// **shadows** it — get_mvar searches backwards and finds the newest
// — and PopVar brings the outer one back. The old SetVar searched
// for an existing name and overwrote it instead, so
// `{with:n,1,{with:n,2,{&n}}{&n}}` destroyed the outer binding and
// left n undefined, where upstream answers "21".
//
// Both refusals are upstream's wording and each is reported by the
// function that asked, which is why fn is a parameter: there are ten
// binding sites and ten occurrences of each message in mfuns2.c. The
// old code had neither, answering an invented "Variable limit
// exceeded." and never checking the name's length at all.
func (env *Env) BindVar(fn, name, value string) error {
	if len(name) > maxFuncNameLen {
		return errf(fn, "Variable name too long.")
	}
	if len(env.vars) >= maxVariables {
		return errf(fn, "Too many variables already defined.")
	}
	env.vars = append(env.vars, variable{name: name, value: value})
	return nil
}

// AssignVar writes to the **innermost** binding of name and reports
// whether there was one.
//
// This is what {set}, {inc} and {dec} do: upstream writes through the
// pointer get_mvar hands back, which is the buffer belonging to the
// most recent binding of that name. It pushes nothing and pops
// nothing, so a name must already be bound for any of the three to
// work — and the value it changes is the one the innermost {with}
// or loop is holding.
//
// The search runs newest-first to match get_mvar. The old SetVar
// searched oldest-first, so reading and writing disagreed the moment
// two bindings shared a name.
func (env *Env) AssignVar(name, value string) bool {
	for i := len(env.vars) - 1; i >= 0; i-- {
		if upper(env.vars[i].name) == upper(name) {
			env.vars[i].value = value
			return true
		}
	}
	return false
}

// PopVar removes the most recently bound variable, which the looping
// functions do when they finish.
func (env *Env) PopVar() {
	if len(env.vars) > 0 {
		env.vars = env.vars[:len(env.vars)-1]
	}
}

// Notes returns the messages produced during evaluation.
func (env *Env) Notes() []string { return env.notes }

// withFrame records an enclosing call on an error as it propagates
// out of argument parsing, which is how upstream's several-line
// report is built: msgparse.c:1637 notifies "{NAME} (arg N)" at each
// level the NULL passes through.
//
// An error that is not one of ours is wrapped, so a failure from
// anywhere still accumulates a trail.
func withFrame(err error, name string, arg int) error {
	frame := fmt.Sprintf("%c%s%c (arg %d)",
		leadChar, name, argEnd, arg)
	e, ok := err.(*Error)
	if !ok {
		return &Error{Msg: err.Error(),
			Trail: []string{frame}}
	}
	e.Trail = append(e.Trail, frame)
	return e
}
