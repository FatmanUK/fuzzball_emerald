package muf

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/boolexp"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Host is what a running program can reach outside itself.
//
// The interpreter takes it as an interface so this package needs no
// world: primitives that only move values around never touch it, and
// the ones that do are explicit about it.
type Host interface {
	// Notify sends a line to an object's connections.
	Notify(who ref.Ref, msg string)
	// NotifyExcept sends a line to everything in a room, skipping
	// some.
	NotifyExcept(room ref.Ref, except []ref.Ref, msg string)

	// Name returns an object's name, and SetName changes it.
	Name(obj ref.Ref) string
	SetName(obj ref.Ref, name string) error

	// Location, Owner and Home answer the obvious questions.
	Location(obj ref.Ref) ref.Ref
	Owner(obj ref.Ref) ref.Ref
	Home(obj ref.Ref) ref.Ref
	// Links returns what an object points at: an exit's
	// destinations, a room's drop-to, or a thing's or player's
	// home.
	Links(obj ref.Ref) []ref.Ref

	// Contents and Exits walk the containment chains.
	Contents(obj ref.Ref) []ref.Ref
	Exits(obj ref.Ref) []ref.Ref
	// MoveTo relocates an object.
	MoveTo(what, dest ref.Ref) error

	// Valid reports whether a ref names a live object, ObjType
	// gives its type, and Flags its flag word.
	Valid(obj ref.Ref) bool
	ObjType(obj ref.Ref) ref.ObjType
	Flags(obj ref.Ref) ref.Flags
	SetFlags(obj ref.Ref, f ref.Flags)
	// Top is one past the highest ref in use.
	Top() ref.Ref

	// Properties. GetProp returns the stored value; the string,
	// integer and dbref forms of the primitives convert from it.
	GetProp(obj ref.Ref, path string) (props.Value, bool)
	SetProp(obj ref.Ref, path string, v props.Value)
	RemoveProp(obj ref.Ref, path string)
	// PropChildren lists the names directly under a property
	// path.
	PropChildren(obj ref.Ref, path string) []string

	// Match resolves a name the way a player's command would,
	// MatchPlayer looks only at player names, and
	// MatchPlayerPrefix accepts a partial one.
	Match(who ref.Ref, name string) ref.Ref
	MatchPlayer(name string) ref.Ref
	MatchPlayerPrefix(name string) ref.Ref

	// Create makes an object and returns its ref.
	Create(t ref.ObjType, name string, parent, owner ref.Ref) (ref.Ref, error)
	// Recycle destroys one.
	Recycle(obj ref.Ref) error
	// SetOwner and SetLinks change what an object belongs to and
	// points at.
	SetOwner(obj, owner ref.Ref)
	SetLinks(obj ref.Ref, dests []ref.Ref)
	// Timestamps returns when an object was created, modified and
	// last used, and how often.
	Timestamps(obj ref.Ref) (created, modified, used int64, count int32)
	// Entrances lists the exits that lead to an object.
	Entrances(target ref.Ref) []ref.Ref

	// CheckPassword and SetPassword handle a player's credential.
	CheckPassword(player ref.Ref, pass string) bool
	SetPassword(player ref.Ref, pass string) error

	// Connections returns how many times a player is connected,
	// and Descriptors the descriptor numbers.
	Connections(player ref.Ref) int
	Descriptors(player ref.Ref) []int
	// Online lists the players with a live connection.
	Online() []ref.Ref
	// DescrPlayer resolves a descriptor number to whoever is on
	// it.
	DescrPlayer(descr int) ref.Ref
	// DescrSize is a connection's reported terminal width and
	// height.
	DescrSize(descr int) (width, height int)
	// SetDescrSize sets what DescrSize reports, for SETWIDTH and
	// SETHEIGHT. ok is false when descr names no live connection.
	SetDescrSize(descr, width, height int) (ok bool)

	// DescrIdle and DescrOnTime are upstream's
	// pdescridle/pdescrontime: how long a connection has been
	// quiet, and how long it has been open. Both report -1 for a
	// descr naming no connection at all — pre-login included,
	// unlike DescrPlayer/DescrSize's own "connected" requirement.
	DescrIdle(descr int) int
	DescrOnTime(descr int) int
	// DescrHost and DescrUser are upstream's
	// pdescrhost/pdescruser. ok is false when descr names no
	// connection. DescrUser's user is always "" here — see the
	// primitive's own doc comment for why.
	DescrHost(descr int) (host string, ok bool)
	DescrUser(descr int) (user string, ok bool)
	// DescrBoot disconnects descr, upstream's pdescrboot, and
	// reports whether it named a live connection at all.
	DescrBoot(descr int) bool
	// DescrNotify sends msg straight to descr, bypassing any
	// object or listen-prop machinery — upstream's pdescrnotify
	// — and reports whether descr named a live connection.
	DescrNotify(descr int, msg string) bool
	// DescrFlush is upstream's pdescrflush: -1 flushes every
	// connection's queued output and reports how many there were;
	// any other descr flushes just that one and reports 1 found
	// or 0 not found. Emerald's output channel has no separate
	// flush step of its own, so this is a counting no-op beyond
	// validating descr exists.
	DescrFlush(descr int) int
	// DescrBufSize is upstream's pdescrbufsize: how much room is
	// left in descr's output buffer, or -1 if descr names no
	// connection. Emerald's output channel has no byte-budget of
	// its own — see the primitive's own doc comment for what
	// this reports instead.
	DescrBufSize(descr int) int
	// DescrLeastIdle and DescrMostIdle are upstream's
	// least_idle_player_descr/most_idle_player_descr: among
	// player's own connections, the one that has seen input most
	// recently, and the one that has seen it least recently. Both
	// report -1 when player has none.
	DescrLeastIdle(player ref.Ref) int
	DescrMostIdle(player ref.Ref) int
	// NextDescr is upstream's pnextdescr: the next live,
	// logged-in connection after descr, in connection order, or 0
	// if there is none — including when descr itself names no
	// connection at all, live or not, which upstream's own
	// null-frame short-circuit means even a genuinely later
	// connection is never found from an invalid starting point.
	NextDescr(descr int) int
	// FirstDescr and LastDescr are upstream's
	// pfirstdescr/plastdescr with player ref.Nothing, or their
	// own player-scoped branches otherwise — see the
	// primitives' own doc comments for the asymmetry between the
	// two forms this file found in the C.
	FirstDescr(player ref.Ref) int
	LastDescr(player ref.Ref) int
	// SetUser is upstream's pset_user, upstream's caller having
	// already verified the password — DESCR_SETUSER does that
	// itself, the same split as its own C. who == ref.Nothing
	// disconnects descr from whoever it was bound to without
	// binding it to anyone new. Reports whether descr named a
	// live connection.
	SetUser(descr int, who ref.Ref) bool

	// ParseProp evaluates the MPI in a property and returns the
	// result. It is a host method because MUF and MPI are
	// separate languages that the server joins, not layers of one
	// another.
	ParseProp(obj ref.Ref, path, arg string, private bool) (string, error)
	// ParsePropEx is ParseProp with named variables in scope —
	// PARSEPROPEX. Each of vars is bound before the property is
	// evaluated and read back afterwards, so the MPI can hand
	// values out as well as take them in; the returned slice is
	// in the same order it was given. A property that is unset or
	// empty evaluates to nothing and leaves the variables as they
	// were.
	ParsePropEx(obj ref.Ref, path string, vars []MPIVar, private bool) (string, []MPIVar, error)

	// ParseMPI evaluates source as MPI directly —
	// PARSEMPI/PARSEMPIBLESSED, as opposed to ParseProp's own
	// "read a property, then evaluate it". who is both the
	// audience and the permissions object, upstream's own
	// do_parse_mesg(player, who, ...) convention. blessed skips
	// permission checks, matching a blessed property. Unlike
	// ParseProp, there is no private/public
	// delay-message-audience distinction — see prim_props.go's
	// own doc comment for why.
	ParseMPI(who ref.Ref, source, arg string, blessed bool) (string, error)

	// BlessProp and UnblessProp set and clear a property's
	// blessed flag — BLESSPROP/UNBLESSPROP. IsPropBlessed reads
	// it back — BLESSED?.
	BlessProp(obj ref.Ref, path string, blessed bool)
	IsPropBlessed(obj ref.Ref, path string) bool

	// TuneGet is upstream's tune_get_parmstring, minus its own
	// mlev gate — SYSPARM and PRONOUN_SUB check TuneReadMLevel
	// themselves, against the reading player's own mlevel
	// (upstream's TUNE_MLEV(player) macro), not the calling
	// program's. ok is false for an unknown parameter name.
	TuneGet(name string) (value string, ok bool)
	// TuneReadMLevel and TuneWriteMLevel are one parameter's own
	// read/write floor. ok is false for an unknown parameter
	// name.
	TuneReadMLevel(name string) (mlev int, ok bool)
	TuneWriteMLevel(name string) (mlev int, ok bool)
	// TuneSet is upstream's tune_setparm, minus its own mlev gate
	// — SETSYSPARM checks TuneWriteMLevel itself. name starting
	// with "%" resets the parameter to its default, upstream's
	// own TP_HAS_FLAG_DEFAULT convention; value is then ignored.
	// ok is false for an unknown parameter name; err is the parse
	// failure otherwise.
	TuneSet(name, value string) (ok bool, err error)
	// TuneList is upstream's tune_parms_array: every parameter
	// mlevel may read, matching pattern — upstream's own exact,
	// case-insensitive equalstr, not truly an smatch pattern
	// despite what its own argument name and abort message claim;
	// an empty pattern matches everything.
	TuneList(pattern string, mlevel int) []TuneEntry
	// TuneBool and TuneInt are typed reads for server-side policy
	// checks — tp_ignore_support, tp_userlog_mlev and the like
	// — as opposed to TuneGet's MUF-facing formatted string.
	// Both panic if name does not name a parameter of that type,
	// the same "every call site names a compile-time constant"
	// contract tune.Set's own typed accessors use.
	TuneBool(name string) bool
	TuneInt(name string) int64
	TuneSpan(name string) time.Duration

	// NameOK is upstream's ok_object_name: whether name is
	// acceptable for a newly created object of type t.
	// EXT-NAME-OK?'s own primitive.
	NameOK(name string, t ref.ObjType) bool

	// UserLog is upstream's log_user: appends a line to the MUF
	// diagnostic log, upstream's own "player(#) [program.muf(#)]
	// timestamp: message" format, for USERLOG.
	UserLog(player, program ref.Ref, msg string)

	// IsIgnoring is upstream's ignore_is_ignoring,
	// IgnoreAdd/IgnoreDel upstream's
	// ignore_add_player/ignore_remove_player — the whole
	// ignore-list feature keyed off tp_ignore_support. All three
	// resolve both objects to their owners internally, matching
	// upstream's own OWNER() calls inside each —
	// IGNORING?/IGNORE_ADD/IGNORE_DEL pass their arguments
	// straight through unresolved.
	IsIgnoring(player, who ref.Ref) bool
	IgnoreAdd(player, who ref.Ref)
	IgnoreDel(player, who ref.Ref)

	// Controls is upstream's controls(): whether who has
	// ownership-level authority over target — a wizard, or
	// target's own owner.
	Controls(who, target ref.Ref) bool

	// CompiledSize reports a program's cached instruction count
	// without compiling it — upstream's PROGRAM_SIZ, and the
	// same "from the cache, never by compiling to find out" rule
	// examine's own doc comment documents. 0 means uncompiled or
	// never run.
	CompiledSize(prog ref.Ref) int
	// Compile (re)compiles a program and replaces its cache entry
	// — COMPILE — reporting its new instruction count.
	Compile(prog ref.Ref) (int, error)
	// Uncompile drops a program's cache entry — UNCOMPILE.
	Uncompile(prog ref.Ref)
	// ProgramLines splits a program's saved source into lines,
	// upstream's own struct line chain — PROGRAM_GETLINES.
	ProgramLines(prog ref.Ref) []string
	// SetProgramLines replaces a program's saved source and drops
	// its compiled form — PROGRAM_SETLINES. Every check the
	// primitive makes is its own; this is only the write.
	SetProgramLines(prog ref.Ref, lines []string)

	// NewPlayer is upstream's create_player — NEWPLAYER.
	// CopyPlayer is COPYPLAYER's own create-then-copy, and
	// ToadPlayer TOADPLAYER's deletion, with every check the
	// primitive makes left to the primitive.
	NewPlayer(name, password string) (ref.Ref, error)
	CopyPlayer(src ref.Ref, name, password string) (ref.Ref, error)
	ToadPlayer(victim, recipient ref.Ref)
	// TuneRefersTo reports whether some @tune parameter points at
	// obj, which is TOADPLAYER's own reason to refuse: deleting a
	// player a parameter names would leave it pointing at the
	// wrong type of object.
	TuneRefersTo(obj ref.Ref) bool

	// Interp runs prog to completion as its own nested frame and
	// returns what it left on its stack — INTERP. ok is false
	// when the program aborted, blocked or finished with an empty
	// stack, all three of which the primitive turns into an empty
	// string rather than a failure.
	Interp(descr, level int, prog, trig ref.Ref, arg string) (Value, bool)

	// CopyObject is upstream's clone_thing — COPYOBJ.
	// copyHidden carries the source's '@'-prefixed properties
	// over, which only a wizard's copy does.
	CopyObject(src ref.Ref, copyHidden bool) (ref.Ref, error)
	// DumpNow asks the persister to write what is pending —
	// DUMP.
	DumpNow()

	// SMTPConfigured reports whether the server has a mail relay
	// set at all, and SMTPModesValid whether its two mode
	// parameters hold values the server knows — both
	// SMTP_SEND's own checks, made before anything is queued.
	// SendMail hands a message off to be delivered; see the
	// implementation for why it cannot report a delivery failure.
	SMTPConfigured() bool
	SMTPModesValid() (tlsOK, authOK bool)
	SendMail(toEmail, toName, subject, body string, by ref.Ref)

	// TimerCount, TimerStart and TimerStop are
	// TIMER_START/TIMER_STOP's own bookkeeping against one
	// process's pending timers. A timer that comes due delivers a
	// TIMER.<id> event to that process's frame; starting one that
	// is already running replaces it. A pid naming no live
	// process is silently ignored, which is what a frame that
	// never became a process (pid 0) does.
	TimerCount(pid int) int
	TimerStart(pid int, id string, seconds int64)
	TimerStop(pid int, id string)
	// SendEvent queues a named event on another live process,
	// resuming it if it is already waiting for one —
	// EVENT_SEND. It reports whether pid named a live process,
	// which upstream treats as nothing to do rather than an
	// error.
	SendEvent(pid int, name string, data Value) bool

	// Stats is upstream's the counting loop shared by STATS and
	// STATS_ARRAY: how many objects owner owns, by type —
	// total, rooms, exits, things, players, programs, garbage, in
	// that order (upstream's own types[] array order, not the
	// text its two doc comments each describe, which disagree
	// with the code and with each other). owner == ref.Nothing
	// counts every object.
	Stats(owner ref.Ref) [7]int

	// MCPMinLevel is the mucker level the mcp_muf_mlev parameter
	// names.
	MCPMinLevel() int

	// MCPSupports reports the version of an MCP package a
	// connection agreed to, as major and minor; both zero means
	// it did not.
	MCPSupports(descr int, pkg string) (int, int)
	// MCPSend sends an out-of-band message. Args pairs a name
	// with its lines, in the order given.
	MCPSend(descr int, pkg, name string, args []MCPArg) error
	// MCPBind registers a program's procedure as the handler for
	// one message, which is how a MUF program serves its own
	// package.
	MCPBind(prog ref.Ref, pkg, name string, addr int) error
	// MCPRegister offers a package on a connection's behalf.
	MCPRegister(pkg string, minMajor, minMinor, maxMajor, maxMinor int) error

	// GUINew opens a dialog on a connection and returns its id.
	GUINew(descr int, frame *Frame) (string, error)
	// GUIDialog reports which connection a dialog is on.
	GUIDialog(id string) (descr int, ok bool)
	// GUIClose forgets a dialog.
	GUIClose(id string) bool
	// GUIValue reads one line of a control's value, and GUIValues
	// every control's first line.
	GUIValue(id, ctrl string, line int) (string, bool)
	GUIValueLines(id, ctrl string) ([]string, bool)
	GUIValues(id string) ([]string, [][]string, bool)
	// GUISetValue records a value locally as well as sending it,
	// so a program reads back what it just set.
	GUISetValue(id, ctrl string, lines []string)

	// Now is the server's clock, which tests replace.
	Now() time.Time
	// Uptime is how long the server has been running.
	Uptime() time.Duration
	// Version identifies the server.
	Version() string

	// TestLock evaluates an already-parsed lock (from PARSELOCK)
	// against testPlayer, the way TESTLOCK does. thingSource is
	// what the lock is evaluated as being "on" — upstream picks
	// trig or caller depending on the consistent_lock_source
	// @tune parameter, which only the host can read. level is the
	// calling frame's own Level; ok is false, with a non-nil
	// error, once it exceeds TESTLOCK's hardcoded recursion
	// limit.
	TestLock(descr, level int, testPlayer ref.Ref, lock *boolexp.Expr, trig, caller ref.Ref) (bool, error)
	// Locked reports whether player is locked out of thing, the
	// way LOCKED? does: could_doit's exit-destination rules, then
	// thing's own lock.
	Locked(descr, level int, player, thing ref.Ref) (bool, error)
	// MaxInterpRecursion is the max_interp_recursion @tune
	// parameter, which bounds LOCKED?'s own recursion guard
	// (TESTLOCK's is hardcoded).
	MaxInterpRecursion() int

	// LockString reads an object's @lock property in its stored,
	// unparsed form, or "*UNLOCKED*" if it has none —
	// GETLOCKSTR.
	LockString(obj ref.Ref) string
	// SetLockString parses raw with matchPlayer's own matching
	// context and stores it as obj's @lock, or clears the lock
	// when raw is empty — SETLOCKSTR (which is _set_lock with
	// silent true). It reports whether the lock was set, which is
	// false only when raw failed to parse.
	SetLockString(descr int, matchPlayer, obj ref.Ref, raw string) bool
	// ParseLock parses a lock expression with matchPlayer's own
	// matching context, upstream's PARSELOCK. A raw that fails to
	// parse notifies matchPlayer directly, the way
	// parse_boolexp's own match failures do, and returns nil
	// (TRUE_BOOLEXP) rather than an error.
	ParseLock(descr int, matchPlayer ref.Ref, raw string) *boolexp.Expr
	// UnparseLock renders a parsed lock back to its stored form,
	// upstream's UNPARSELOCK. A nil lock (TRUE_BOOLEXP) renders
	// as "", not "*UNLOCKED*" — that is UNPARSELOCK's own
	// special case, not shared with LockString/GETLOCKSTR.
	// matchPlayer is upstream's ProgUID, unused unless a dbref
	// inside the lock ever needs rendering with a viewer's
	// permissions, which UNPARSELOCK's own fullname-false form
	// never does.
	UnparseLock(matchPlayer ref.Ref, lock *boolexp.Expr) string
	// PrettyLock renders a parsed lock as a human-readable
	// string, upstream's PRETTYLOCK: unparse_boolexp with
	// fullname true, so a CONST dbref shows the way matchPlayer
	// (ProgUID) would see it rather than as a bare "#123".
	PrettyLock(matchPlayer ref.Ref, lock *boolexp.Expr) string

	// ForceLevel is upstream's global force_level: how deeply
	// @force, the FORCE primitive and the {force} MPI function
	// are currently nested, shared across all three call paths
	// — FORCE_LEVEL.
	ForceLevel() int
	// IsPID reports whether pid names a live process other than
	// the caller's own frame, which ISPID? checks in addition to
	// the caller's own PID.
	IsPID(pid int) bool
	// Instances counts the processes currently running prog —
	// INSTANCES.
	Instances(prog ref.Ref) int
	// CanCall reports whether a caller at callerLevel, acting as
	// callerUID (progUID), may CALL prog's public function name
	// — CANCALL?. prog is compiled on demand if it has not been
	// already; a program that fails to compile, or declares no
	// public by that name, reports false rather than an error,
	// matching upstream's own silent failure there.
	CanCall(callerLevel int, callerUID ref.Ref, prog ref.Ref, name string) bool

	// ControlsProcess is upstream's control_process: whether
	// callerUID (progUID) may act on pid, because it controls the
	// process's program or its trigger, or is the player the
	// process is running for. A pid that names no live process
	// reports false, the same as one that exists but is not
	// controlled — KILL cannot tell the two apart before
	// checking.
	ControlsProcess(callerUID ref.Ref, pid int) bool
	// KillPID is upstream's dequeue_process: removes pid if it
	// names a live process, and reports whether it did.
	KillPID(pid int) bool
	// Fork registers child — already built by FORK's own fork()
	// — as a new background process and returns its pid,
	// upstream's add_muf_delay_event called with a zero delay. It
	// returns 0, having already notified the process's own player
	// why, when the process or player process-count limit is
	// exceeded — upstream's "Event killed. Timequeue table
	// full."
	Fork(child *Frame) int
	// Queue is upstream's add_muf_delayq_event: compiles prog and
	// schedules it to run after seconds, with arg as its initial
	// stack argument and "Queued Event." as its COMMAND variable
	// — the two are different strings upstream, unlike a
	// command-driven program where they are the same one. It
	// returns the new process's pid, or 0 — never FORK's -1 —
	// on the same process-count-limit failure Fork reports.
	Queue(descr int, prog ref.Ref, seconds int64, arg string) int
	// Force is upstream's process_command call inside prim_force:
	// runs command as victim, on behalf of player (running as
	// program). Unlike @force, FORCE needs none of its
	// ownership-escaping checks — mlev 4 already means the
	// calling program has full wizard authority — so this only
	// does the forcelist/force_level bookkeeping FORCEDBY and
	// FORCEDBY_ARRAY read; every other check is FORCE's own
	// primFunc's job.
	Force(descr int, player, program, victim ref.Ref, command string)
	// ForcedBy and ForcedByArray read upstream's forcelist: the
	// object that most recently forced the currently-running
	// program (ref.Nothing if none has), and every forcer still
	// on the stack, most recent first.
	ForcedBy() ref.Ref
	ForcedByArray() []ref.Ref
	// GetPIDs is upstream's get_pids: every live pid whose
	// program or player is obj, or every pid at all when obj is
	// negative (upstream's "ref < 0" — not only ref.Nothing).
	// selfPID is excluded from every match here — upstream's
	// own timequeue never holds the calling program's own,
	// still-synchronously-running process, so GETPIDS's own
	// primFunc handles including it, only when obj is exactly the
	// calling program's own ref, as its own explicit final step,
	// matching prim_getpids' "if (program == ref) push fr->pid"
	// exactly. No existence check is made on obj, matching
	// upstream's own prim_getpids, which only checks the argument
	// is a dbref-typed value at all.
	GetPIDs(obj ref.Ref, selfPID int) []int
	// PIDInfo is upstream's get_pidinfo, for GETPIDINFO's
	// other-pid branch — the caller's own pid is answered
	// straight from the live *Frame* instead, matching
	// prim_getpidinfo's own self/other split. ok is false when
	// pid names no live process, which GETPIDINFO turns into an
	// empty dictionary, matching upstream leaving one built by
	// new_array_dictionary untouched.
	PIDInfo(pid int) (info PIDInfo, ok bool)
	// WatchPID is the "target exists" branch of upstream's
	// prim_watchpid: callerPID starts watching targetPID, and
	// reports whether targetPID named a live process at all. When
	// it reports false, WATCHPID's own primFunc queues the
	// PROC.EXIT event itself, straight onto the calling Frame,
	// matching prim_watchpid's own else branch — that half
	// needs no Host call, since it never touches another process.
	WatchPID(callerPID, targetPID int) bool
}

// MPIVar is one named variable PARSEPROPEX puts in scope, and reads
// back once the property has been evaluated.
type MPIVar struct {
	Name  string
	Value string
}

// TuneEntry is one row of SYSPARM_ARRAY's result, upstream's own
// tune_parms_array dictionary.
type TuneEntry struct {
	Group, Name, Label, Type  string
	ReadMLev, WriteMLev       int
	Nullable, Active, Default bool

	// Only the field matching Type ("string", "integer", "dbref"
	// or "boolean") is meaningful — the same
	// one-of-several-fields shape tune.Value itself uses, not
	// reused directly so this package need not import
	// internal/tune.
	ValueStr  string
	ValueNum  int64
	ValueRef  ref.Ref
	ValueBool bool
	// ObjType is set only when Type is "dbref", upstream's own
	// str_objecttype.
	ObjType string
}

// PIDInfo is the subset of get_pidinfo's dictionary that depends on
// which process pid names, rather than on the calling frame —
// GETPIDINFO fills in the rest (PID, MLEVEL, CPU, FILTERS, TYPE)
// itself, the same fields upstream hardcodes or computes identically
// in both branches of prim_getpidinfo.
type PIDInfo struct {
	CalledProg ref.Ref
	CalledData string
	Descr      int
	InstCnt    int
	NextRun    int64
	Player     ref.Ref
	Started    time.Time
	Subtype    string
	Trig       ref.Ref
}

// MCPArg is one argument of an outgoing MCP message: a name and its
// lines.
type MCPArg struct {
	Name  string
	Lines []string
}

// callSite records where a call came from, so EXIT can return to it.
type callSite struct {
	pc int
	// scopeBase is how many scope frames were open before the
	// call.
	scopeBase int
}

// forLoop is one active FOR or FOREACH.
type forLoop struct {
	// list is what FOREACH walks; nil for a counting FOR.
	keys []Value
	vals []Value
	idx  int

	// cur, end and step drive a counting FOR.
	cur, end, step int64
	counting       bool
}

// tryBlock is one active TRY, recording where to resume and how much
// stack to restore when something is caught.
type tryBlock struct {
	catchPC  int
	stackTop int
	callTop  int
	forTop   int
	scopeTop int
	detailed bool
}

// Frame is one running program.
type Frame struct {
	Prog *Program
	PC   int

	// Stack is the argument stack the program pushes and pops.
	Stack []Value

	// Vars are the program's globals, LVars its program-locals,
	// and scopes the per-call scoped variables.
	Vars   []Value
	LVars  []Value
	scopes [][]Value

	calls []callSite
	fors  []forLoop
	trys  []tryBlock

	// Instructions counts what has run, which bounds a runaway
	// program.
	Instructions int

	// Mode is the multitasking mode, which decides how readily
	// the program yields.
	Mode int

	// Block says why the program stopped, when Run returned
	// Blocked. The scheduler reads it to decide what the program
	// is waiting for.
	Block BlockReason

	// Err holds the error a TRY has not yet caught.
	err *Error

	// ErrorFlags records the arithmetic conditions a program can
	// ask about with is_set?, rather than being told about by an
	// abort. Fuzzball treats integer division by zero as a flag
	// and a zero result, not a failure.
	ErrorFlags ErrorFlags

	// Caller identifies who is running the program, for the
	// reserved variables and for permission checks.
	Caller ref.Ref
	Trig   ref.Ref

	// Perms is upstream's fr->perms, chosen by whoever *starts*
	// the program rather than by the program itself. It is what
	// find_uid consults alongside the STICKY and HAVEN flags, so
	// a launch site that leaves it at the zero value is saying
	// the program runs as whoever triggered it.
	Perms Perms
	// Descr is the connection the program was started from.
	Descr int

	// Level is upstream's fr->level: how deeply nested this
	// frame's own interp_loop call is. A program started from a
	// command runs at level 1; TESTLOCK and LOCKED? propagate
	// level+1 to a frame they start to evaluate a program-type
	// lock constant, so a chain of locks that each trigger
	// another lock check cannot recurse forever.
	Level int

	// PID is this frame's process id, upstream's fr->pid, for the
	// PID and ISPID? primitives. It is assigned by whoever
	// registers the frame as a process (internal/game's
	// procQueue) and stays zero for a frame that never becomes
	// one, such as a lock-triggered program RunLock evaluates
	// synchronously.
	PID int

	// Started is when this frame's process began, upstream's
	// fr->started, for GETPIDINFO's own STARTED key. Set
	// alongside PID by whoever registers the frame as a process;
	// zero for a frame that never becomes one.
	Started time.Time

	// Supplicant is upstream's fr->supplicant: the object being
	// tested against a lock, for a frame a program-type lock
	// constant is running. It is ref.Nothing outside that
	// context.
	Supplicant ref.Ref

	// PendingEvents is upstream's fr->events: named data queued
	// for this frame by something outside it — currently only
	// WATCHPID's own PROC.EXIT.<pid>, delivered either
	// immediately, when the watched pid already names no live
	// process, or later by whoever runs that process to
	// completion. EVENT_WAITFOR consumes from here.
	PendingEvents []MufEvent

	// alreadyCreated is upstream's fr->already_created: how many
	// objects this run has made. Below mucker level 3 it caps a
	// program at one, so a low-level program cannot fill the
	// database in a loop.
	alreadyCreated int

	// Traced is whether this frame is currently printing a trace
	// line per instruction. It is recomputed whenever the frame
	// resumes, and DEBUG_ON/DEBUG_OFF set it directly so they
	// take effect immediately.
	Traced bool

	// ForceTrace makes this frame print a trace line per
	// instruction even when its program is not flagged DARK —
	// DEBUGGER_BREAK's own effect, since Emerald has no
	// interactive debugger to break into.
	ForceTrace bool

	// rndbuf is upstream's fr->rndbuf: the 16-byte state SRAND
	// draws from, seeded on first use and readable and
	// replaceable through GETSEED/SETSEED. RANDOM ignores it —
	// only SRAND is the repeatable one.
	rndbuf []byte

	// WantsBlanks is READ_WANTS_BLANKS/READ_WANTS_NO_BLANKS's own
	// flag: whether an empty line typed while this frame is
	// blocked on READ should resume it. Upstream defaults this
	// false, so a blank line is swallowed rather than delivered
	// until a program asks otherwise.
	WantsBlanks bool

	host Host
}

// MufEvent is upstream's struct mufevent: a named piece of data
// queued for a frame, for EVENT_WAITFOR to consume.
type MufEvent struct {
	Name string
	Data Value
}

// AddEvent queues a named event for this frame, upstream's
// muf_event_add.
func (f *Frame) AddEvent(name string, data Value) {
	f.PendingEvents = append(f.PendingEvents, MufEvent{Name: name, Data: data})
}

// popEvent is upstream's muf_event_pop_specific (a non-empty filters)
// and muf_event_pop (no filter, pops the oldest event regardless of
// name) in one: it removes and returns the oldest queued event
// matching one of filters, or the oldest event of any name when
// filters is empty.
func (f *Frame) popEvent(filters []string) (MufEvent, bool) {
	for i, ev := range f.PendingEvents {
		if len(filters) == 0 ||
			matchesEvent(ev.Name, filters) {
			f.PendingEvents = append(f.PendingEvents[:i], f.PendingEvents[i+1:]...)
			return ev, true
		}
	}
	return MufEvent{}, false
}

func matchesEvent(name string, filters []string) bool {
	for _, f := range filters {
		if f == name {
			return true
		}
	}
	return false
}

// NewFrame prepares a program to run.
func NewFrame(p *Program, host Host) *Frame {
	f := &Frame{
		Prog:       p,
		PC:         p.Start,
		Level:      1,
		Vars:       make([]Value, len(p.Vars)),
		LVars:      make([]Value, len(p.LVars)),
		Supplicant: ref.Nothing,
		host:       host,
	}
	// Every variable starts as integer zero, which is what
	// interp() fills them with before overwriting the four
	// reserved ones.
	for i := range f.Vars {
		f.Vars[i] = Int(0)
	}
	for i := range f.LVars {
		f.LVars[i] = Int(0)
	}
	return f
}

// BlockReason says what a suspended program is waiting for.
type BlockReason struct {
	Kind BlockKind
	// Seconds is how long a SLEEP asked for.
	Seconds int64
	// Events lists what an EVENT_WAITFOR is waiting on.
	Events []string
}

// BlockKind enumerates the ways a program can suspend.
type BlockKind int

const (
	BlockNone BlockKind = iota
	// BlockRead waits for a line of input from the player.
	BlockRead
	// BlockSleep waits for a time to pass.
	BlockSleep
	// BlockEvent waits for a named event.
	BlockEvent
)

// Multitasking modes, from the MODE and SETMODE primitives.
const (
	ModePreempt    = 0
	ModeForeground = 1
	ModeBackground = 2
)

// ErrorFlags are the arithmetic conditions is_set? reports. The order
// matches the bits union error_mask defines, because is_set? takes
// the number.
type ErrorFlags struct {
	DivZero   bool
	NaN       bool
	Imaginary bool
	FBounds   bool
	IBounds   bool
}

// Get reads a flag by the number is_set? uses.
func (e ErrorFlags) Get(n int) bool {
	switch n {
	case 0:
		return e.DivZero
	case 1:
		return e.NaN
	case 2:
		return e.Imaginary
	case 3:
		return e.FBounds
	case 4:
		return e.IBounds
	}
	return false
}

// Set writes a flag by number.
func (e *ErrorFlags) Set(n int, v bool) {
	switch n {
	case 0:
		e.DivZero = v
	case 1:
		e.NaN = v
	case 2:
		e.Imaginary = v
	case 3:
		e.FBounds = v
	case 4:
		e.IBounds = v
	}
}

// Clear resets every flag.
func (e *ErrorFlags) Clear() { *e = ErrorFlags{} }

// SetReserved fills the four variables every program starts with, and
// puts the command's argument on the stack.
//
// That last part is easy to miss and load-bearing: interp() pushes
// the argument string before the program runs, so a program starts
// with one value on the stack rather than none, and "depth" reflects
// it.
func (f *Frame) SetReserved(me, loc, trigger ref.Ref, command string) {
	if len(f.Vars) < ReservedVars {
		return
	}
	f.Vars[VarMe] = Obj(me)
	f.Vars[VarLoc] = Obj(loc)
	f.Vars[VarTrigger] = Obj(trigger)
	f.Vars[VarCommand] = Str(command)
	f.Caller, f.Trig = me, trigger
	f.Stack = append(f.Stack, Str(command))
}

// MLevel is the mucker level the program runs at, which bounds what
// its primitives may do.
func (f *Frame) MLevel() int { return f.Prog.MLevel }

// Push puts a value on the stack.
func (f *Frame) Push(v Value) error {
	if len(f.Stack) >= StackSize {
		return errf("stack overflow")
	}
	f.Stack = append(f.Stack, v)
	return nil
}

// Pop takes the top value.
func (f *Frame) Pop() (Value, error) {
	if len(f.Stack) == 0 {
		return Value{}, errf("stack underflow")
	}
	v := f.Stack[len(f.Stack)-1]
	f.Stack = f.Stack[:len(f.Stack)-1]
	return v, nil
}

// PopN takes the top n values, leftmost first.
func (f *Frame) PopN(n int) ([]Value, error) {
	if len(f.Stack) < n {
		return nil, errf("stack underflow")
	}
	out := make([]Value, n)
	copy(out, f.Stack[len(f.Stack)-n:])
	f.Stack = f.Stack[:len(f.Stack)-n]
	return out, nil
}

// Peek returns the value n places from the top without removing it.
func (f *Frame) Peek(n int) (Value, error) {
	if n < 0 || n >= len(f.Stack) {
		return Value{}, errf("stack underflow")
	}
	return f.Stack[len(f.Stack)-1-n], nil
}

// Depth is how many values are on the stack.
func (f *Frame) Depth() int { return len(f.Stack) }

// Host returns what the frame can reach outside itself.
func (f *Frame) Host() Host { return f.host }

// scope returns the scoped variables of the innermost call.
func (f *Frame) scope() []Value {
	if len(f.scopes) == 0 {
		return nil
	}
	return f.scopes[len(f.scopes)-1]
}
