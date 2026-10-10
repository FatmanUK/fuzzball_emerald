// Package game joins connections to the world: the login flow,
// command dispatch, and the commands themselves.
//
// Everything here runs on the world goroutine. Transports call the
// On* methods from their own goroutines; those only enqueue work.
package game

import (
	"log/slog"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/mcp"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// maxInputLen bounds a single line of input, matching Fuzzball's
// MAX_COMMAND_LEN. Anything longer is truncated rather than
// allocated.
const maxInputLen = 2048

// Server owns the connection hub and dispatches commands.
type Server struct {
	engine *world.Engine
	hub    *session.Hub
	log    *slog.Logger

	// shutdown asks the process to stop, set by the server
	// binary.
	shutdown func()

	// started is when the server came up, for uptime.
	started time.Time
	// compiling names the programs a compile is currently inside,
	// so a $ifcancall cycle between two libraries fails rather
	// than recursing on the world goroutine.
	compiling map[ref.Ref]bool
	// lookDepth is enter_room's donelook counter: an autolook
	// command that moves the player again would otherwise not
	// stop.
	lookDepth int
	// wizOnly is upstream's wizonly_mode: while it is set, only a
	// true wizard may log in. It is deliberately not persisted
	// — a maintenance window that survived a restart would be
	// the opposite of useful.
	wizOnly bool

	// programs caches compiled MUF.
	programs map[ref.Ref]compiled

	// editors holds one open MUF editor session per player. The
	// program text being edited lives here rather than on the
	// object, so an abandoned session cannot corrupt what is
	// stored.
	editors map[ref.Ref]*editSession
	// editLine remembers each program's current line between
	// sessions, as upstream keeps it on the program itself. It is
	// not persisted.
	editLine map[ref.Ref]int

	// mpiEvents holds what MPI's {delay} has scheduled, fired by
	// the tick.
	mpiEvents []mpiEvent
	// deferred is the rest of upstream's timequeue: work a listen
	// propqueue has filed for a later tick, so a listener answers
	// *after* the line that woke it. See propqueue.go.
	deferred []deferredEvent

	// metaDepth bounds trigger's metalink recursion. Upstream has
	// no equivalent and an exit linked to itself crashes it; see
	// trigger's doc comment.
	metaDepth int

	// propqLevel is upstream's propq_level, the recursion counter
	// the propqueues share. One counter across every queue type,
	// which is what stops an _arrive that departs from looping
	// through a _depart that arrives.
	propqLevel int

	// lastQuotaRefill is where the spam limiter's clock stands,
	// advanced a whole time slice at a time.
	lastQuotaRefill time.Time

	// forceDepth counts how deep @force is nested, so a command
	// that forces something that forces back cannot recurse
	// without end.
	forceDepth int
	// relayDepth is notify_nolisten_level: a puppet's @pecho is
	// evaluated only at depth zero, so a @pecho that notifies
	// anything cannot recurse through the relay for ever.
	relayDepth int
	// forcelist is upstream's own global objnode stack: who is
	// forcing what, most recently pushed last, read by
	// FORCEDBY/FORCEDBY_ARRAY. Both @force (cmdForce) and the
	// FORCE primitive push onto and pop from it around their own
	// call to force/commandAs.
	forcelist []ref.Ref

	// procs holds suspended programs: those sleeping, waiting for
	// input, or waiting for an event.
	procs *procQueue

	// dialogs holds the MCP-GUI dialogs open on every connection,
	// and dialogOwner the frame that opened each one.
	dialogs     *mcp.Dialogs
	dialogOwner map[string]*muf.Frame

	// inOwnLock guards ownLockPasses against a lock whose own
	// evaluation asks whether somebody controls something. One
	// goroutine owns the world, so a plain bool is enough.
	inOwnLock bool

	// mcpPackages is what a new connection is offered, and
	// mcpBindings maps a message to the program procedure that
	// claimed it.
	mcpPackages []mcp.Package
	mcpBindings map[mcpBinding]mcpTarget
}

// OnShutdown sets what @shutdown calls.
func (s *Server) OnShutdown(fn func()) { s.shutdown = fn }

// Options configure a Server.
type Options struct {
	Logger *slog.Logger
}

// New returns a Server driving engine.
func New(engine *world.Engine, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{
		engine:   engine,
		hub:      session.NewHub(),
		log:      opts.Logger,
		started:  time.Now(),
		programs: map[ref.Ref]compiled{},
		procs:    newProcQueue(),
		editors:  map[ref.Ref]*editSession{},

		dialogs:     mcp.NewDialogs(),
		dialogOwner: map[string]*muf.Frame{},
		mcpBindings: map[mcpBinding]mcpTarget{},
		editLine:    map[ref.Ref]int{},
	}
	s.installMCPHandlers()
	// The ownership lock, which World.Controls cannot evaluate
	// for itself. The operation queue is buffered, so this is
	// simply the first thing Run applies — ahead of any
	// command.
	_ = engine.Go(func(w *world.World) {
		w.OwnLockPasses = func(who, what ref.Ref) bool {
			return s.ownLockPasses(w, who, what)
		}
	})
	return s
}

// ownLockPasses is the last clause of `controls` (`db.c:1866`):
// `test_lock_false_default(NOTHING, who, what, MESGPROP_OWNLOCK)`.
//
// `@ownlock` wrote a property, `examine` displayed it as "Ownership
// Key", and **nothing read it** — the shape `_/oecho` had. It is
// the one route past the ownership test a world can configure, so
// until this existed every mucker rule and every per-type refusal
// that depends on a non-owner reaching `controls` was unreachable;
// `TestSetFlagMuckerOwnershipClauseIsUnreachable` was the pin.
//
// The descriptor is NOTHING, which is upstream's: an ownership check
// is not made on anybody's behalf, so a lock property's MPI runs with
// no connection to report to.
//
// **The re-entrancy guard is not upstream's.** A lock may be a
// property holding MPI, and `{controls}` is an MPI function, so a
// world can write an ownlock whose evaluation asks the same question
// again. Upstream recurses until it runs out of stack; refusing the
// inner question is strictly better and is the answer an unset lock
// would have given anyway.
func (s *Server) ownLockPasses(w *world.World,
	who, what ref.Ref) bool {

	if s.inOwnLock {
		return false
	}
	s.inOwnLock = true
	defer func() { s.inOwnLock = false }()
	return s.lockPasses(w, -1, 1, who, what, propOwnLock, false)
}

func defaultWelcome() []string {
	return []string{
		"",
		"  Fuzzball Emerald",
		"",
		"  connect <name> <password>   to enter",
		"  create <name> <password>    to make a new character",
		"  QUIT                        to disconnect",
		"",
	}
}

// Connect registers a new connection and shows the welcome banner. It
// blocks until the descriptor exists, because the transport needs it
// to start pumping output.
func (s *Server) Connect(tr session.Transport, host string) (*session.Descriptor, error) {
	var d *session.Descriptor
	done := make(chan struct{})
	err := s.engine.Go(func(w *world.World) {
		d = s.hub.Add(tr, host, w.Now())
		d.Quota.Set(int(w.Tune.Int("command_burst_size")))
		// The world pointer is captured rather than looked
		// up: an Engine owns exactly one for its lifetime and
		// never swaps it, so this is the same *World every
		// handler sees — and the alternative would be an
		// accessor that handed the world out to whoever
		// asked, which is the one thing the engine exists to
		// prevent.
		d.AllowANSI = func() bool { return s.allowANSI(w, d) }
		// The same capture, for the same reason: the
		// descriptor stamps its own "last sent" timestamp
		// from a transport goroutine, and a test that freezes
		// the world's clock must freeze that too.
		d.Clock = w.Now
		close(done)

		// MCP is offered before the banner, so a client that
		// speaks it has answered by the time anything else
		// arrives. A client that does not simply sees a line
		// it ignores.
		d.MCP.StartNegotiation()

		for _, line := range s.welcomeLines(w, d) {
			d.Send(line)
		}
		// Someone arriving at a shut server is told so now
		// rather than after they have typed a password, which
		// is upstream's own welcome_user behaviour. The two
		// are an if/else there as well: maintenance mode
		// refuses everyone the cap would have, so saying both
		// would be saying it twice.
		if s.wizOnly {
			d.Send(wizOnlyBanner)
		} else if s.serverFull(w) {
			if msg := w.Tune.String("playermax_warnmesg"); msg != "" {
				d.Send(msg)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	<-done
	return d, nil
}

// Input handles one line from a client.
//
// This runs on the transport's own goroutine, which is where the spam
// limiter lives: a connection that has spent its allowance waits here
// for the next one rather than queueing work the world would have to
// throttle later. Nothing is dropped, and only that one connection is
// held up.
func (s *Server) Input(d *session.Descriptor, line string) {
	if len(line) > maxInputLen {
		line = line[:maxInputLen]
	}
	// An out-of-band message is a client talking to the server,
	// not a player typing, so it costs nothing — upstream
	// excludes it too.
	if !strings.HasPrefix(line, mcp.Prefix) {
		if !d.Quota.Take(d.Done()) {
			return
		}
	}
	_ = s.engine.Go(func(w *world.World) {
		d.LastActive = w.Now()
		line = sanitizeInput(line,
			w.Tune.Bool("tab_input_replaced_with_space"))

		// Out-of-band messages are taken off the line before
		// anything else sees it. A line the client quoted
		// comes back as the text it was quoting.
		line, ok := d.MCP.ProcessInput(line)
		if !ok {
			return
		}

		if d.Connected {
			// The order here is do_command's. Interface
			// commands are answered first, then a program
			// waiting on a READ takes the line, then the
			// editor, and only then does the command
			// parser see it.
			//
			// The editor and a READ both take the line
			// untrimmed: leading spaces are part of
			// program text, and an empty line is a blank
			// line to insert.
			//
			// `log_interactive` decides whether either is
			// logged, which nothing read: a line taken by
			// a program or the editor was not recorded at
			// all.
			switch {
			case s.interfaceCommand(w, d, line):
			case s.readInput(w, d.ID, line):
				s.logInteractive(w, d, line, true)
			case s.editing(d.Player) != nil:
				s.logInteractive(w, d, line, false)
				s.editInput(w, d, line)
			default:
				s.command(w, d, line)
			}
			return
		}
		s.login(w, d, line)
	})
}

// Disconnect tears a connection down.
func (s *Server) Disconnect(d *session.Descriptor) {
	_ = s.engine.Go(func(w *world.World) {
		// A dialog cannot be closed from a connection that
		// has gone, so anything still open on it is forgotten
		// here.
		s.closeDialogsFor(d.ID)
		if d.Connected {
			// `dequeue_prog(player, 2)`, and the
			// descriptor count is **upstream's**: the
			// sweep runs only when this was the player's
			// last connection, so dropping one of two
			// leaves the other's foreground program
			// alone. The "Foreground program aborted."
			// line upstream prints here can therefore
			// reach nobody, which is why there is none.
			if len(s.hub.DescriptorsFor(d.Player)) < 2 {
				s.abortForegroundFor(w, d.Player)
			}
			s.announceDisconnect(w, d)
			s.log.Info("disconnected",
				"descriptor", d.ID,
				"player", d.Player.String(),
				"name", nameOf(w, d.Player),
				"host", d.Hostname,
			)
		}
		s.hub.Remove(d)
	})
}

// Resize records a client's reported terminal size.
func (s *Server) Resize(d *session.Descriptor, ws session.WindowSize) {
	_ = s.engine.Go(func(*world.World) {
		if ws.Width > 0 {
			d.Width = ws.Width
		}
		if ws.Height > 0 {
			d.Height = ws.Height
		}
	})
}

// Tick is called on the world goroutine at each flush interval. It
// runs whatever the process queue has due, which is how a sleeping
// program wakes.
func (s *Server) OnTick() func(*world.World) {
	return func(w *world.World) { s.Tick(w) }
}

// Hub exposes the connection hub. Only the world goroutine may use
// it.
func (s *Server) Hub() *session.Hub { return s.hub }

// allowANSI is queue_ansi's gate (interface.c:673): whether this
// connection keeps colour or has it stripped.
//
// Logged in, it is the player's own COLOR flag — upstream's
// CHOWN_OK, which means something else entirely on anything but a
// player, and is why `examine` prints it as COLOR for one and
// CHOWN_OK for the rest.
//
// *Before* login it is neither a flag nor a setting of the client's
// but two @tune parameters, both of which have to be on: a world that
// has turned MPI off, or turned it off for the welcome screen, gets
// no colour on its banner either. That reads like an accident of
// implementation — the banner is the only pre-login text a world
// writes, so the parameters that decide whether MPI runs over it also
// decide whether its colour survives — and it is upstream's.
//
// It is read live rather than cached, so "@set me=C" takes effect on
// the next line. The descriptor holds it as a callback for that
// reason; internal/session cannot see a world.
func (s *Server) allowANSI(w *world.World,
	d *session.Descriptor) bool {

	if d.Connected {
		o := w.Get(d.Player)
		return o != nil && o.Flags&ref.ChownOK != 0
	}
	return w.Tune.Bool("do_mpi_parsing") &&
		w.Tune.Bool("do_welcome_parsing")
}

// notify sends a formatted line to every descriptor a player is
// connected on.
func (s *Server) notify(w *world.World, player ref.Ref, format string, args ...any) {
	s.send(w, player, sprintf(format, args...))
}

// send delivers a line verbatim. Use it for text that came from the
// world, such as a description or a player's own words, where a stray
// '%' must not be read as a format verb.
//
// A puppet's output is forwarded to whoever owns it, prefixed,
// because a THING has no connection of its own. That is how anything
// a puppet is told — by a program, or by @force — reaches a
// person at all.
func (s *Server) send(w *world.World, player ref.Ref, text string) {
	s.sendCount(w, player, text)
}

// sendCount is send, reporting how many connections heard it —
// counting the owner of a puppet that relayed. `notify_nolisten`
// returns that count and three callers act on it.
func (s *Server) sendCount(w *world.World, player ref.Ref,
	text string) int {

	n := s.hub.Tell(player, text)
	// A direct notify is **private**, which is half of upstream's
	// relay condition.
	if owner, prefix, ok := puppetRelay(s, w, player,
		true); ok {
		n += s.hub.Tell(owner, prefix+text)
	}
	return n
}

// notifyPrivately is `notify_listeners` with `isprivate` set
// (`interface.c:4870`): the listen propqueues fire, the vehicle echo
// does not, and the message is delivered to a PLAYER or a THING and
// to nothing else.
//
// It reports whether **anybody heard**, which is the return value
// `page` and `whisper` both branch on — and which is why their "X
// is not connected." is decided after the message has been composed
// rather than before.
//
// The ignore filter belongs here rather than in `send`:
// `notify_filtered` applies it and a bare `notify` does not, so a
// program telling somebody something still gets through.
func (s *Server) notifyPrivately(w *world.World, from, obj,
	room ref.Ref, text string) bool {

	s.notifyListeners(w, from, ref.Nothing, obj, room, text)
	o := w.Get(obj)
	if o == nil {
		return false
	}
	switch o.Type() {
	case ref.TypePlayer, ref.TypeThing:
	default:
		return false
	}
	h := &mufHost{s: s, w: w}
	if h.IsIgnoring(obj, from) {
		return false
	}
	return s.sendCount(w, obj, text) > 0
}

// puppetRelay reports whether a target's output should also reach its
// owner, and with what prefix.
//
// The conditions are upstream's (`interface.c:4697`), and each
// excludes a way of using a puppet to spy: a DARK puppet is silent
// unless a wizard owns it, a room flagged ZOMBIE is a no-puppet zone,
// and an owner who is themselves flagged ZOMBIE has opted out of
// hearing any of it.
//
// **And one more, which was missing**: the relay happens only when
// the message is private *or* the puppet is somewhere other than its
// owner. A puppet standing in the room with its owner relays nothing
// public, because the owner has already heard the line — and
// without that test every pose a puppet made arrived twice, once as
// itself and once prefixed. The old comment here asserted the
// condition was always satisfied; §4.3's `@force $pup = :jumps!` is
// where that turned out to be wrong.
func puppetRelay(s *Server, w *world.World, target ref.Ref,
	isPrivate bool) (ref.Ref, string, bool) {
	if !w.Tune.Bool("allow_zombies") {
		return ref.Nothing, "", false
	}
	o := w.Get(target)
	if o == nil || o.Type() != ref.TypeThing ||
		o.Flags&ref.Zombie == 0 {
		return ref.Nothing, "", false
	}
	owner := w.Get(o.Owner)
	if owner == nil || owner.Flags&ref.Zombie != 0 {
		return ref.Nothing, "", false
	}
	wizardOwned := owner.Flags.IsWizard()
	if o.Flags&ref.Dark != 0 && !wizardOwned {
		return ref.Nothing, "", false
	}
	if room := w.Get(o.Location); !wizardOwned && room != nil &&
		room.Type() == ref.TypeRoom && room.Flags&ref.Zombie != 0 {
		return ref.Nothing, "", false
	}

	if !isPrivate && owner.Location == o.Location {
		return ref.Nothing, "", false
	}

	prefix := o.Name + "> "
	// notify_nolisten_level: while a relay is already evaluating
	// a @pecho, the prefix is taken as empty rather than
	// evaluated again. Without it a @pecho that notifies anything
	// recurses, and on one goroutine that is the whole server.
	if v, ok := w.GetProp(target, propPuppetEcho); ok &&
		v.Type == props.String && s.relayDepth == 0 {
		// Upstream evaluates this one with no descriptor at
		// all — do_parse_prop(-1, ...) at interface.c:4722
		// — because the text is being relayed rather than
		// triggered by anyone in particular.
		s.relayDepth++
		got := s.evalMPI(w, -1, target, target, v.Str,
			"(@Pecho)", v.Blessed, mpi.Private)
		s.relayDepth--
		if got != "" {
			prefix = got + " "
		}
	}
	return o.Owner, prefix, true
}

// propPuppetEcho overrides the prefix a puppet's output reaches its
// owner with, from include/db.h.
const propPuppetEcho = "_/pecho"

// notifyRoom sends a line to everyone in a room, optionally skipping
// some.
//
// The container itself hears it too when it is a player or a thing,
// which is how someone carrying a puppet hears what the puppet says:
// upstream's notify_except notifies the object the contents belong to
// before walking them. Rooms are skipped, because a room is not an
// audience.
//
// The listen propqueues fire from here, which is where upstream puts
// them: notify_except runs them on the room, then up the environment
// chain, and then on every object in the room — before delivering a
// word to anybody. notifyRoom passes no speaker, so a caller that
// knows one uses notifyRoomFrom.
func (s *Server) notifyRoom(w *world.World, room ref.Ref, except []ref.Ref, format string, args ...any) {
	s.notifyRoomFrom(w, ref.Nothing, room, except,
		"%s", sprintf(format, args...))
}

// notifyRoomFrom is notify_except, with the speaker named.
//
// The speaker matters for two things a listener can see: the room a
// listening program is told the line happened in is the *speaker's*
// location rather than the room being notified, and the ignore filter
// is applied between the speaker and each recipient.
//
// Upstream's environment walk here is inconsistent with itself and is
// reproduced: the first step upwards is LOCATION(room) and every step
// after it is getparent, so a VEHICLE room's chain is followed
// differently on the first hop than on the rest.
func (s *Server) notifyRoomFrom(w *world.World, from, room ref.Ref,
	except []ref.Ref, format string, args ...any) {

	text := sprintf(format, args...)
	where := from
	if o := w.Get(from); o != nil {
		where = o.Location
	}

	if w.Tune.Bool("allow_listeners") {
		s.notifyListeners(w, from, ref.Nothing, room, where,
			text)
		if w.Tune.Bool("allow_listeners_env") {
			s.envListeners(w, from, room, where, text)
		}
	}

	// Delivery, which is separate from the queues above: the
	// container's own listen props have already fired, and
	// upstream's walk over the contents skips rooms, so neither
	// half can run twice on one object.
	tell := func(r ref.Ref) {
		o := w.Get(r)
		if o == nil || containsRef(except, r) {
			return
		}
		switch o.Type() {
		case ref.TypePlayer:
			s.hub.Tell(r, text)
		case ref.TypeThing:
			// A thing hears nothing itself, but a puppet
			// relays what it hears to whoever owns it.
			// Room speech is **public** -- notify_except
			// passes isprivate 0 all the way down -- so a
			// puppet standing where its owner stands
			// relays nothing: the owner has already heard
			// the line itself.
			if owner, prefix, ok := puppetRelay(s, w, r,
				false); ok {
				s.hub.Tell(owner, prefix+text)
			}
			s.vehicleEcho(w, from, r, text)
		}
	}

	tell(room)
	for _, r := range w.Contents(room) {
		if o := w.Get(r); o != nil &&
			o.Type() != ref.TypeRoom &&
			!containsRef(except, r) {
			s.notifyListeners(w, from, ref.Nothing, r,
				where, text)
		}
		tell(r)
	}
}

// envListeners runs the listen propqueues up the environment chain
// from a room.
//
// The first step upwards is LOCATION(room) and every step after it is
// getparent, which is inconsistent with itself and is upstream's: a
// VEHICLE room's chain is followed differently on the first hop than
// on the rest.
func (s *Server) envListeners(w *world.World, from, room,
	where ref.Ref, text string) {

	srch := ref.Nothing
	if o := w.Get(room); o != nil {
		srch = o.Location
	}
	for srch != ref.Nothing {
		s.notifyListeners(w, from, ref.Nothing, srch, where,
			text)
		srch = w.Parent(srch)
	}
}

func containsRef(list []ref.Ref, r ref.Ref) bool {
	for _, x := range list {
		if x == r {
			return true
		}
	}
	return false
}

// nameOf returns an object's name, or a placeholder when it is gone.
func nameOf(w *world.World, r ref.Ref) string {
	if o := w.Get(r); o != nil {
		return o.Name
	}
	return "<nothing>"
}

// unparse is `unparse_object` (`db.c:1428`): an object's name,
// followed by its dbref and flags when the viewer may see them.
//
// The virtual refs render as their names rather than as numbers,
// because they are what a link or a location field says when it
// points at nothing real, and a report that says "#-1" tells the
// reader less than "*NOTHING*" does.
//
// A viewer of ref.Nothing is the sanity checker rather than a person,
// and sees everything: there is nobody to keep a secret from.
//
// **Three of upstream's clauses were missing**, and what stood in
// their place was an invented wizard-or-owner test. The real
// condition has four parts and no wizardry of its own:
//
//   - a **STICKY viewer** sees only names, whatever else is true. On
//     a player STICKY is "goes home when dropped" for their things;
//     here it doubles as a per-player switch for a quieter display,
//     which reads like an accident of flag reuse and is upstream's.
//   - `can_see_flags`, which is `can_teleport_to` — control of the
//     target, *or* its link lock passing and either LINK_OK or, for
//     anything that is not a thing, ABODE. Wizardry and ownership
//     arrive through `controls` inside it.
//   - for a **non-player** target, `controls_link`: control of what
//     it points at, which for an exit is any of its destinations or
//     the owner of its location.
//   - or the target being **CHOWN_OK**, which is how a world
//     publishes an object's dbref to everybody.
//
// So a mortal sees the dbref of anything marked LINK_OK or CHOWN_OK,
// which this server showed to nobody but the owner. Porting it meant
// a lock evaluation, which is why `unparse` is now a method.
func (s *Server) unparse(w *world.World, viewer,
	target ref.Ref) string {

	switch target {
	case ref.Nothing:
		return "*NOTHING*"
	case ref.Ambiguous:
		return "*AMBIGUOUS*"
	case ref.Home:
		return "*HOME*"
	case ref.Nil:
		return "*NIL*"
	}
	o := w.Get(target)
	if o == nil {
		return "*INVALID*"
	}
	// unparse_object's first line, commented "Handle ZOMBIE case"
	// (`db.c:1434`): the test is made on whoever **owns** the
	// viewer, so a puppet sees what its owner sees. Without it a
	// wizard's puppet reported bare names where the wizard would
	// have seen dbrefs and flags, which is what §4.3's "z look"
	// found.
	if viewer != ref.Nothing {
		viewer = w.OwnerOf(viewer)
	}
	v := w.Get(viewer)
	if viewer == ref.Nothing || v != nil &&
		v.Flags&ref.Sticky == 0 &&
		(s.canSeeFlagsFor(w, viewer, target) ||
			o.Type() != ref.TypePlayer &&
				(s.controlsLink(w, viewer, target) ||
					o.Flags&ref.ChownOK != 0)) {

		return o.Name + "(" + target.String() +
			o.Flags.Unparse() + ")"
	}
	return o.Name
}

// vehicleEcho is `notify_listeners`'s vehicle branch
// (`interface.c:4902`): what happens *outside* a vehicle is relayed
// to whoever is inside it, prefixed.
//
// `_/oecho` was written by `@oecho`, displayed by `examine`, and
// **read nowhere** -- the same shape `@ownlock` still has. The
// default prefix is "Outside>", so the gap showed even in a world
// that had never set the property: §4.4's `drive :vroom` is said by
// the car, in the room, to a driver sitting inside it, and this
// server delivered nothing.
//
// Five conditions, and each of them excludes a way of listening in
// from a parked car. The vehicle must not be DARK unless a wizard
// owns it; the line must be **public**, which is why this is reached
// only from notifyRoomFrom; the speaker must be where the vehicle is;
// and a vehicle inside another vehicle relays nothing unless a wizard
// owns it, which is what stops a chain of them carrying a room's
// speech away.
func (s *Server) vehicleEcho(w *world.World, from, obj ref.Ref,
	text string) {

	o := w.Get(obj)
	if o == nil || o.Type() != ref.TypeThing ||
		o.Flags&ref.Vehicle == 0 {
		return
	}
	wizardOwned := isWizard(w, o.Owner)
	if o.Flags&ref.Dark != 0 && !wizardOwned {
		return
	}
	speaker := w.Get(from)
	if speaker == nil || speaker.Location != o.Location {
		return
	}
	if loc := w.Get(o.Location); !wizardOwned && loc != nil &&
		loc.Type() == ref.TypeRoom &&
		loc.Flags&ref.Vehicle != 0 {
		return
	}

	prefix := "Outside>"
	if v, ok := w.GetProp(obj, propRoomEcho); ok &&
		v.Type == props.String {
		// do_parse_prop(-1, who, obj, ...): no descriptor,
		// the speaker as the viewer and the vehicle as the
		// object carrying the property, and private even
		// though the line being prefixed is public.
		got := s.evalMPI(w, -1, from, obj, v.Str,
			"(@Oecho)", v.Blessed, mpi.Private)
		if got != "" {
			prefix = got
		}
	}
	for _, r := range w.Contents(obj) {
		s.send(w, r, prefix+" "+text)
	}
}

// statusLog returns the logger for server-lifecycle messages.
func (s *Server) statusLog() *slog.Logger {
	return logging.On(s.log, logging.Status)
}

// securityLog returns the logger for the audit trail: who tried to
// authenticate, whose password changed, and who ran a privileged
// command.
func (s *Server) securityLog() *slog.Logger {
	return logging.On(s.log, logging.Security)
}

// mufLog returns the logger for MUF diagnostics.
func (s *Server) mufLog() *slog.Logger {
	return logging.On(s.log, logging.MUFError)
}

// commandLog returns the logger for player commands.
func (s *Server) commandLog() *slog.Logger {
	return logging.On(s.log, logging.Command)
}

// trimCommand splits a line into a verb and its argument.
func trimCommand(line string) (verb, arg string) {
	verb, rest := cutWord(line)
	a1, a2, found := strings.Cut(rest, string(argDelimiter))
	a1 = trimSpace(a1)
	if !found {
		return verb, a1
	}
	// Rejoined rather than carried as two fields, because every
	// command that takes a second argument cuts `ctx.arg` at the
	// first '=' itself — so putting each half's own trimming in
	// before the join gives all of them upstream's answer without
	// touching any of them.
	return verb, a1 + string(argDelimiter) + trimLeftSpace(a2)
}

// argDelimiter is upstream's ARG_DELIMITER, the '=' that separates a
// command's two arguments.
const argDelimiter = '='

// cutWord splits a line at its first whitespace, which is what
// upstream's command-word scan does: `!isspace`, not a space. A
// tab-separated command line reached one word here.
func cutWord(line string) (word, rest string) {
	for i := 0; i < len(line); i++ {
		if isSpaceByte(line[i]) {
			return line[:i], line[i+1:]
		}
	}
	return line, ""
}

// isSpaceByte is C's isspace for the ASCII range, which is what
// upstream's parsing uses throughout: space, tab, newline, vertical
// tab, form feed and carriage return.
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' ||
		c == '\f' || c == '\r'
}

// trimLeftSpace is skip_whitespace, trimRightSpace is
// remove_ending_whitespace, and trimSpace is both. They are spelled
// out rather than taken from strings because strings.TrimSpace also
// trims Unicode spaces, and upstream trims only what isspace answers
// for — so a non-breaking space is part of an argument there and
// was not here.
func trimLeftSpace(s string) string {
	for len(s) > 0 && isSpaceByte(s[0]) {
		s = s[1:]
	}
	return s
}

func trimRightSpace(s string) string {
	// The bound is `len(s) > 1`, not `> 0`: upstream's loop
	// condition is `p > *s`, so it never removes the *first*
	// character and a string of nothing but whitespace keeps one.
	// Unobservable where it is used, since arg1 is left-trimmed
	// first and an all-whitespace argument is empty by then, but
	// the loop is the one upstream has.
	for len(s) > 1 && isSpaceByte(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

func trimSpace(s string) string {
	return trimRightSpace(trimLeftSpace(s))
}

// sanitizeInput is `process_input`'s byte filter
// (`interface.c:3637`), which Emerald did not have: every byte a
// client sent reached the command parser.
//
// Four rules, and `isinput` is the first of them: `isprint(q & 127)`
// — the byte is **masked** to seven bits for the test and stored
// unmasked, so 0xE9 is kept because 0x69 is printable, while 0x81 is
// dropped because 0x01 is not. That is what lets a high byte into a
// name at all, and why `7bit_other_names` and `7bit_thing_names`
// exist to refuse one.
//
// A tab becomes a space when `tab_input_replaced_with_space` is set,
// which it is by default and which nothing here read. Backspace and
// delete remove the previous character, and do nothing at the start
// of the line. Everything else — a stray carriage return included
// — is dropped silently.
func sanitizeInput(line string, tabToSpace bool) string {
	out := make([]byte, 0, len(line))
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case isPrintByte(c & 127):
			out = append(out, c)
		case c == '\t':
			if tabToSpace {
				c = ' '
			}
			out = append(out, c)
		case c == 8 || c == 127:
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		}
	}
	return string(out)
}

// isPrintByte is `isprint` in the C locale: a space through a tilde.
func isPrintByte(c byte) bool { return c >= 0x20 && c <= 0x7e }

// fullCommand is upstream's full_command (game.c:677): the line after
// the command word, with exactly *one* character skipped.
//
// It is not trimCommand's argument. That one is arg1, which upstream
// trims at both ends; this keeps whatever follows the single space,
// which is what say and pose print back.
func fullCommand(line string) string {
	for i := 0; i < len(line); i++ {
		if isSpaceByte(line[i]) {
			return line[i+1:]
		}
	}
	return ""
}
