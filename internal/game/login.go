package game

import (
	"runtime/debug"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// login handles input from a connection that has not yet
// authenticated.
func (s *Server) login(w *world.World, d *session.Descriptor, line string) {
	defer func() {
		if r := recover(); r != nil {
			d.Send("Something went wrong. Please try again.")
			// The line is not logged: it may hold a
			// password.
			s.log.Error("panic during login",
				"descriptor", d.ID, "host", d.Hostname,
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	cmd, user, pass := parseConnect(line)

	switch {
	case cmd == "":
		return

	case ascii.HasPrefix("connect", cmd) && len(cmd) >= 2:
		s.doConnect(w, d, user, pass)

	case ascii.HasPrefix("create", cmd) && len(cmd) >= 2:
		s.doCreate(w, d, user, pass)

	case ascii.EqualFold(cmd, "quit"):
		d.Send("Goodbye.")
		d.Close()

	case ascii.EqualFold(cmd, "who"):
		s.loginWho(w, d)

	// Upstream matches "help" by its first four characters here,
	// and answers with the connection help rather than the banner
	// the connection has already been shown.
	case ascii.HasPrefix(cmd, "help"):
		s.connectHelp(w, d)

	default:
		d.Send("Type \"connect <name> <password>\" to enter, or \"help\" for more.")
	}
}

// parseConnect splits a login line into a command, a name and a
// password.
//
// It mirrors Fuzzball's parse_connect: whitespace-separated, and the
// command is matched case-insensitively so "CO" works as well as
// "connect".
func parseConnect(line string) (cmd, user, pass string) {
	fields := strings.Fields(line)
	switch len(fields) {
	case 0:
		return "", "", ""
	case 1:
		return fields[0], "", ""
	case 2:
		return fields[0], fields[1], ""
	default:
		return fields[0], fields[1], fields[2]
	}
}

// doConnect authenticates an existing player.
func (s *Server) doConnect(w *world.World, d *session.Descriptor, user, pass string) {
	log := s.statusLog()

	player, ok := w.PlayerNamed(user)
	if !ok {
		s.failConnect(w, d, user, "no such player")
		return
	}
	o := w.Get(player)
	if o == nil || o.Type() != ref.TypePlayer {
		s.failConnect(w, d, user, "not a player")
		return
	}

	// A player imported with no password is unreachable until one
	// is set. Fuzzball would accept any password here; saying so
	// plainly is better than a bare "incorrect password".
	if o.PasswordHash == password.NoPassword {
		d.Send("That character has no password set and cannot be connected to.")
		d.Send("A wizard must set one with @password before it can be used.")
		s.securityLog().Warn("refused login to a player with no password",
			"player", player.String(), "name", o.Name, "host", d.Hostname)
		return
	}

	res := password.Verify(o.PasswordHash, pass)
	if !res.OK {
		s.failConnect(w, d, user, "bad password")
		return
	}

	// The password was right; the server may still be shut to
	// them. A true wizard is exempt from both tests, so an admin
	// can always get in — to lift the restriction, or to deal
	// with whatever filled the place up.
	if !o.Flags.IsTrueWizard() {
		log := s.securityLog()
		if s.wizOnly {
			d.Send(wizOnlyBootMesg)
			log.Warn("refused login: wizards only",
				"player", player.String(),
				"name", o.Name, "host", d.Hostname)
			d.Close()
			return
		}
		if s.serverFull(w) {
			d.Send(w.Tune.String("playermax_bootmesg"))
			log.Warn("refused login: server full",
				"player", player.String(),
				"name", o.Name, "host", d.Hostname,
				"limit",
				w.Tune.Int("playermax_limit"))
			d.Close()
			return
		}
	}

	// A correct password stored in one of Fuzzball's formats is
	// upgraded to Argon2id now that we have the plaintext to
	// rehash from.
	if res.NeedsUpgrade {
		if hashed, err := password.Hash(pass); err != nil {
			log.Error("could not upgrade a legacy password hash",
				"player", player.String(), "error", err)
		} else {
			o.PasswordHash = hashed
			w.Modified(player)
			log.Info("upgraded a legacy password hash to argon2id",
				"player", player.String(), "name", o.Name)
		}
	}

	s.finishLogin(w, d, player)
}

// The two things wizonly_mode says, which are not @tune parameters
// because upstream compiles them in: the banner line somebody sees
// before they type anything, and the refusal when they do.
const (
	wizOnlyBanner = "## The game is currently in maintenance " +
		"mode, and only wizards will be able to connect."
	wizOnlyBootMesg = "Sorry, but the game is in maintenance " +
		"mode currently, and only wizards are allowed to " +
		"connect.  Try again later."
)

// serverFull reports whether the playermax cap has been reached,
// upstream's "tp_playermax && con_players_curr >=
// tp_playermax_limit".
//
// The count is of logged-in connections, not of distinct players: two
// windows open as the same character cost two places, which is
// upstream's own con_players_curr.
func (s *Server) serverFull(w *world.World) bool {
	if !w.Tune.Bool("playermax") {
		return false
	}
	return int64(len(s.hub.Connected())) >= w.Tune.Int("playermax_limit")
}

// failConnect reports a failed login without saying which half was
// wrong.
func (s *Server) failConnect(w *world.World, d *session.Descriptor, user, why string) {
	d.Send(w.Tune.String("connect_fail_mesg"))
	s.securityLog().Warn("failed login",
		"user", user, "reason", why, "descriptor", d.ID, "host", d.Hostname)
}

// doCreate makes a new player.
func (s *Server) doCreate(w *world.World, d *session.Descriptor, user, pass string) {
	if w.Tune.Bool("registration") {
		// Registration on means characters are made out of
		// band, not from the login screen.
		d.Send(w.Tune.String("register_mesg"))
		return
	}
	// A brand-new character has no wizard bit to be exempt by, so
	// both tests simply apply.
	if s.wizOnly {
		d.Send(wizOnlyBootMesg)
		d.Close()
		return
	}
	if s.serverFull(w) {
		d.Send(w.Tune.String("playermax_bootmesg"))
		d.Close()
		return
	}
	// create_player has exactly two refusals and every one of its
	// four callers prints them verbatim (`player.c:213`), where
	// this had five messages of its own invention — and each
	// caller printed a different subset of them.
	if !okObjectName(w, user, ref.TypePlayer) {
		d.Send(cannotUseThatName)
		return
	}
	if !okPassword(pass) {
		d.Send(cannotUseThatPassword)
		return
	}

	o, err := s.createPlayer(w, user, pass)
	if err != nil {
		d.Send(err.Error())
		return
	}

	s.securityLog().Info("created player",
		"player", o.Ref.String(), "name", user, "host", d.Hostname)
	s.finishLogin(w, d, o.Ref)
}

// createPlayer makes a player and puts them at the starting room. The
// name is expected to have been checked already.
//
// The name is recorded in a property as well as on the object,
// because a player may be renamed and upstream keeps what they were
// first called.
func (s *Server) createPlayer(w *world.World, user, pass string) (*world.Object, error) {
	hashed, err := password.Hash(pass)
	if err != nil {
		return nil, errMsg("That password could not be used.")
	}

	start := w.Tune.Ref("player_start")
	if !w.Valid(start) {
		start = ref.GlobalEnvironment
	}

	o := w.Create(user, ref.TypePlayer, ref.Nothing)
	o.Owner = o.Ref // a player owns itself
	o.PasswordHash = hashed
	o.Home = start
	o.Props.SetString(propCreatedAs, user)
	o.Props.Set(propValue, props.Value{
		Type: props.Int, Num: w.Tune.Int("start_pennies"),
	})
	applyTuneFlags(o, w.Tune.String("pcreate_flags"))
	if err := w.MoveTo(o.Ref, start); err != nil {
		s.statusLog().Error("could not place a new player",
			"player", o.Ref.String(), "error", err)
	}
	return o, nil
}

// propCreatedAs records the name a player was created with, from
// include/game.h.
const propCreatedAs = "@__sys__/name/created_as"

// create_player's two refusals (`player.c:214`, `:219`). Every caller
// prints them as given, so they are constants rather than a
// per-caller message.
const (
	cannotUseThatName = "You cannot use that name for a " +
		"player."
	cannotUseThatPassword = "You cannot use that password."
)

// errMsg is an error carrying a message meant for a player.
type errMsg string

func (e errMsg) Error() string { return string(e) }

// finishLogin binds a descriptor to a player and puts them in the
// world.
func (s *Server) finishLogin(w *world.World, d *session.Descriptor, player ref.Ref) {
	alreadyOn := s.hub.Online(player)
	s.hub.Bind(d, player, w.Now())
	w.Used(player)

	// _sys/max_connects is a high-water mark of *connections*,
	// not of distinct players and not of sockets — upstream's
	// con_players_max against its con_players_curr
	// (interface.c:4583). Upstream raises it from its descriptor
	// sweep, where the count can only have grown since the last
	// pass; the one moment it can grow here is a login finishing,
	// so this is the same mark reached from the other side.
	w.RecordMaxConnects(len(s.hub.Connected()))

	s.securityLog().Info("connected",
		"descriptor", d.ID,
		"player", player.String(),
		"name", nameOf(w, player),
		"host", d.Hostname,
		"transport", string(d.Transport),
	)

	if alreadyOn {
		d.Send("You are already connected elsewhere; both connections are now active.")
	}

	s.showMOTD(w, d)
	// The arrival look belongs **inside** announceConnect, which
	// is where `announce_connect` does it: between the
	// announcement and the `connect` action, and before the
	// propqueues. Doing it afterwards put a world's `_connect`
	// output above the room description instead of below it.
	s.announceConnect(w, d, alreadyOn)
	s.warnInteractive(d)
}

// warnInteractive tells a reconnecting player that their input is
// going somewhere other than the command parser. An editor session
// outlives the connection that opened it, so without this a player
// comes back to a prompt that silently eats everything they type.
func (s *Server) warnInteractive(d *session.Descriptor) {
	e := s.editing(d.Player)
	if e == nil {
		return
	}
	if e.insert {
		d.Send(sprintf("***  You are currently inserting MUF program text.  Use \"%s\" to return to the editor, then \"%c\" if you wish to return to your regularly scheduled MUCK universe.  ***",
			exitInsert, quitEditCommand))
		return
	}
	d.Send("***  You are currently using the MUF program editor.  ***")
}

// announceConnect is `announce_connect` (`interface.c:1068`): the
// arrival line, the look, the `connect` action, the puppets waking
// up, and the two propqueues, in that order.
//
// **Which of those is gated on being the first connection is not what
// this said.** The line is tested against DARK and nothing else, so
// it fires on *every* connection; what fires only on the first is the
// `connect` **action** and the puppet wake-up, which upstream's own
// comment calls odd and leaves alone. This had the line first-only
// and neither of the other two at all.
func (s *Server) announceConnect(w *world.World,
	d *session.Descriptor, alreadyOn bool) {

	o := w.Get(d.Player)
	if o == nil || o.Location == ref.Nothing {
		return
	}
	if o.Flags&ref.Dark == 0 &&
		!hasFlag(w, o.Location, ref.Dark) {
		s.notifyRoom(w, o.Location, []ref.Ref{d.Player},
			"%s has connected.", o.Name)
	}
	s.autolook(w, d.ID, d.Player)
	if !alreadyOn {
		s.connectAction(w, d, "connect")
		s.announcePuppets(w, d.Player, "wakes up.", propPCon)
	}
	// ts_useobject is already done by the login path above, where
	// upstream does it here, after the queues. Same count either
	// way; doing it twice is not.
	s.connectQueues(w, d.ID, d.Player, o.Location,
		propConnect, propOConnect, "Connect", "Oconnect")
}

// announceDisconnect is `announce_disconnect` (`interface.c:2325`),
// and the same split: the line and the propqueues fire on **every**
// disconnect, while the `disconnect` action and the puppets falling
// asleep wait for the last one. This returned early for anything but
// the last, so a world's `_disconnect` never saw a second connection
// closing.
func (s *Server) announceDisconnect(w *world.World,
	d *session.Descriptor) {

	o := w.Get(d.Player)
	if o == nil || o.Location == ref.Nothing {
		return
	}
	// The descriptor being closed is still in the hub here, so
	// "the last one" is a count of one.
	last := len(s.hub.DescriptorsFor(d.Player)) <= 1
	if o.Flags&ref.Dark == 0 &&
		!hasFlag(w, o.Location, ref.Dark) {
		s.notifyRoom(w, o.Location, []ref.Ref{d.Player},
			"%s has disconnected.", o.Name)
	}
	if last {
		s.connectAction(w, d, "disconnect")
		s.announcePuppets(w, d.Player, "falls asleep.",
			propPDCon)
	}
	s.connectQueues(w, d.ID, d.Player, o.Location,
		propDisconnect, propODisconnect,
		"Disconnect", "Odisconnect")
}

// connectAction is the `connect` and `disconnect` **exits**, which
// had no port at all: matched as an exit at priority 1, and an
// ambiguous match is no match (`interface.c:1085`).
//
// A floor of 1 is **not** a privilege, which is worth saying because
// it reads like one beside `m3_huh`'s floor of 3. Priority 1 is the
// *default* — an exit with no mucker bits has it — so the only
// thing this excludes is an **ABODE** exit, which is the one shape
// that binds more weakly than the default (`match.c:694`).
func (s *Server) connectAction(w *world.World,
	d *session.Descriptor, name string) {

	m := match.New(w, d.Player, name).
		PreferType(ref.TypeExit).Level(1)
	r := m.Exits().Result()
	if r == ref.Nothing || r == ref.Ambiguous {
		return
	}
	c := &ctx{w: w, d: d, who: d.Player, out: d.Send,
		verb: name}
	s.useExit(c, r)
}

// announcePuppets is `announce_puppets` (`interface.c:1013`): every
// ZOMBIE thing the player owns announces itself in its own room when
// they connect or disconnect, with `_/pcon` or `_/pdcon` replacing
// the wording.
//
// It is a **whole-database walk**, which upstream's own comment calls
// brutal and does anyway, on every login and every disconnect. Three
// DARK tests gate each one: the room, the player and the thing.
func (s *Server) announcePuppets(w *world.World, player ref.Ref,
	msg, prop string) {

	if hasFlag(w, player, ref.Dark) {
		return
	}
	w.Each(func(o *world.Object) bool {
		what := o.Ref
		if o.Type() != ref.TypeThing ||
			o.Flags&ref.Zombie == 0 ||
			ownerOf(w, what) != player {
			return true
		}
		where := o.Location
		if o.Flags&ref.Dark != 0 ||
			hasFlag(w, where, ref.Dark) ||
			where == ref.Nothing {
			return true
		}
		line := msg
		if v, ok := w.GetProp(what, prop); ok &&
			v.Str != "" {
			line = v.Str
		}
		s.notifyRoom(w, where, []ref.Ref{what}, "%s %s",
			o.Name, line)
		return true
	})
}

// connectQueues runs one of the two connect/disconnect propqueue
// pairs. Both are environment walks from the *player*, so getparent
// takes them through the room and upwards in one pass — the same
// shape _arrive uses, and unlike _depart's four calls.
func (s *Server) connectQueues(w *world.World, descr int,
	who, loc ref.Ref, prop, oprop, arg, oarg string) {

	r := propqRun{
		descr: descr, player: who, where: loc,
		// The trigger is NOTHING: nothing in the world caused
		// this, so a program hooked here has no TRIGGER to
		// read. That is upstream's, and it is why a _connect
		// program cannot tell which descriptor woke it except
		// through DESCR.
		trigger: ref.Nothing, what: who, exclude: ref.Nothing,
		mlev: 1,
	}
	r.arg, r.private = arg, true
	s.envpropqueue(w, r, prop)
	r.arg, r.private = oarg, false
	s.envpropqueue(w, r, oprop)
}

// loginWho lists who is online, for someone who has not logged in
// yet.
func (s *Server) loginWho(w *world.World, d *session.Descriptor) {
	if w.Tune.Bool("secure_who") {
		d.Send("You must be connected to see who is online.")
		return
	}
	s.writeWho(w, d, "")
}
