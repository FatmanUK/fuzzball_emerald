package game

import (
	"runtime/debug"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// login handles input from a connection that has not yet authenticated.
func (s *Server) login(w *world.World, d *session.Descriptor, line string) {
	defer func() {
		if r := recover(); r != nil {
			d.Send("Something went wrong. Please try again.")
			// The line is not logged: it may hold a password.
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

	case ascii.EqualFold(cmd, "help"):
		for _, l := range s.welcome {
			d.Send(l)
		}

	default:
		d.Send("Type \"connect <name> <password>\" to enter, or \"help\" for more.")
	}
}

// parseConnect splits a login line into a command, a name and a password.
//
// It mirrors Fuzzball's parse_connect: whitespace-separated, and the command
// is matched case-insensitively so "CO" works as well as "connect".
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

	// A player imported with no password is unreachable until one is set.
	// Fuzzball would accept any password here; saying so plainly is better
	// than a bare "incorrect password".
	if o.PasswordHash == password.NoPassword {
		d.Send("That character has no password set and cannot be connected to.")
		d.Send("A wizard must set one with @password before it can be used.")
		log.Warn("refused login to a player with no password",
			"player", player.String(), "name", o.Name, "host", d.Hostname)
		return
	}

	res := password.Verify(o.PasswordHash, pass)
	if !res.OK {
		s.failConnect(w, d, user, "bad password")
		return
	}

	// A correct password stored in one of Fuzzball's formats is upgraded to
	// Argon2id now that we have the plaintext to rehash from.
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

// failConnect reports a failed login without saying which half was wrong.
func (s *Server) failConnect(w *world.World, d *session.Descriptor, user, why string) {
	d.Send(w.Tune.String("connect_fail_mesg"))
	s.statusLog().Warn("failed login",
		"user", user, "reason", why, "descriptor", d.ID, "host", d.Hostname)
}

// doCreate makes a new player.
func (s *Server) doCreate(w *world.World, d *session.Descriptor, user, pass string) {
	if w.Tune.Bool("registration") {
		// Registration on means characters are made out of band, not
		// from the login screen.
		d.Send(w.Tune.String("register_mesg"))
		return
	}
	if err := validPlayerName(w, user); err != nil {
		d.Send(err.Error())
		return
	}
	if pass == "" {
		d.Send("You must give a password.")
		return
	}

	o, err := s.createPlayer(w, user, pass)
	if err != nil {
		d.Send(err.Error())
		return
	}

	s.statusLog().Info("created player",
		"player", o.Ref.String(), "name", user, "host", d.Hostname)
	s.finishLogin(w, d, o.Ref)
}

// createPlayer makes a player and puts them at the starting room. The name is
// expected to have been checked already.
//
// The name is recorded in a property as well as on the object, because a
// player may be renamed and upstream keeps what they were first called.
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

// validPlayerName applies the rules a new name must satisfy.
func validPlayerName(w *world.World, name string) error {
	switch {
	case name == "":
		return errMsg("You must give a name.")
	case len(name) > int(w.Tune.Int("player_name_limit")):
		return errMsg("That name is too long.")
	}
	for _, r := range name {
		// Fuzzball reserves these because they are matcher and property
		// syntax; a name containing one could never be referred to.
		if strings.ContainsRune("#*!$ \t\r\n", r) || r < 32 {
			return errMsg("That name contains a character that is not allowed.")
		}
	}
	for _, reserved := range []string{"me", "here", "home", "nil"} {
		if ascii.EqualFold(name, reserved) {
			return errMsg("That name is reserved.")
		}
	}
	if _, taken := w.PlayerNamed(name); taken {
		return errMsg("That name is already taken.")
	}
	return nil
}

// errMsg is an error carrying a message meant for a player.
type errMsg string

func (e errMsg) Error() string { return string(e) }

// finishLogin binds a descriptor to a player and puts them in the world.
func (s *Server) finishLogin(w *world.World, d *session.Descriptor, player ref.Ref) {
	alreadyOn := s.hub.Online(player)
	s.hub.Bind(d, player, w.Now())
	w.Used(player)

	s.statusLog().Info("connected",
		"descriptor", d.ID,
		"player", player.String(),
		"name", nameOf(w, player),
		"host", d.Hostname,
		"transport", string(d.Transport),
	)

	if alreadyOn {
		d.Send("You are already connected elsewhere; both connections are now active.")
	}

	s.announceConnect(w, d, alreadyOn)
	s.lookHere(w, player)
	s.warnInteractive(d)
}

// warnInteractive tells a reconnecting player that their input is going
// somewhere other than the command parser. An editor session outlives the
// connection that opened it, so without this a player comes back to a prompt
// that silently eats everything they type.
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

// announceConnect tells the player's room that they have arrived.
func (s *Server) announceConnect(w *world.World, d *session.Descriptor, alreadyOn bool) {
	if alreadyOn {
		return // they were already here
	}
	o := w.Get(d.Player)
	if o == nil || o.Location == ref.Nothing {
		return
	}
	s.notifyRoom(w, o.Location, []ref.Ref{d.Player}, "%s has connected.", o.Name)
}

// announceDisconnect tells the player's room that they have gone.
func (s *Server) announceDisconnect(w *world.World, d *session.Descriptor) {
	// Only announce when the last connection for this player goes away.
	if len(s.hub.DescriptorsFor(d.Player)) > 1 {
		return
	}
	o := w.Get(d.Player)
	if o == nil || o.Location == ref.Nothing {
		return
	}
	s.notifyRoom(w, o.Location, []ref.Ref{d.Player}, "%s has disconnected.", o.Name)
}

// loginWho lists who is online, for someone who has not logged in yet.
func (s *Server) loginWho(w *world.World, d *session.Descriptor) {
	if w.Tune.Bool("secure_who") {
		d.Send("You must be connected to see who is online.")
		return
	}
	s.writeWho(w, d, "")
}
