package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
)

// Player management: set a password, and turn the WIZARD, BUILDER and
// QUELL bits on and off.
//
// It is deliberately a short list. Everything else about a player is
// reachable through the object inspector, and the things a wizard
// most often needs a configurator for are a forgotten password and a
// wizard bit they cannot grant themselves because nobody can log in.

// playerRow is one line of the list.
type playerRow struct {
	Ref      string
	RefNum   int32
	Name     string
	Wizard   bool
	Builder  bool
	Quell    bool
	HasPass  bool
	Legacy   bool
	Created  int64
	LastUsed int64
}

type playerData struct {
	Players []playerRow
	Changed string
}

// getPlayers lists them.
func (s *Server) getPlayers(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Players")
	data, err := s.playerData(r)
	if err != nil {
		s.log.Error("listing players", "error", err)
		p.Error = "the players could not be listed"
	}
	p.Data = data
	p.Notice = r.URL.Query().Get("notice")
	s.render(w, "players.html", p)
}

func (s *Server) playerData(r *http.Request) (playerData, error) {
	rows, err := s.store.Players(r.Context())
	if err != nil {
		return playerData{}, err
	}
	out := make([]playerRow, 0, len(rows))
	for _, o := range rows {
		f := ref.Flags(o.Flags)
		out = append(out, playerRow{
			Ref:     ref.Ref(o.Ref).String(),
			RefNum:  o.Ref,
			Name:    o.Name,
			Wizard:  f&ref.Wizard != 0,
			Builder: f&ref.Builder != 0,
			Quell:   f&ref.Quell != 0,
			HasPass: o.PasswordHash != password.NoPassword,
			// A password imported from a legacy dump is
			// upgraded when its owner next logs in to the
			// MUCK. Saying so here explains why one
			// player's hash looks unlike the others.
			Legacy: isLegacyHash(o.PasswordHash),

			Created:  o.Created,
			LastUsed: o.LastUsed,
		})
	}
	return playerData{Players: out,
		Changed: r.URL.Query().Get("changed")}, nil
}

// isLegacyHash reports whether a stored password is one of the
// imported formats rather than Argon2id.
func isLegacyHash(h string) bool {
	return h != password.NoPassword &&
		!strings.HasPrefix(h, "$argon2")
}

// postPlayers applies one change to one player.
func (s *Server) postPlayers(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	target, err := parseRef(r.PostFormValue("ref"))
	if err != nil {
		s.fail(w, r, "players.html", "that is not a dbref")
		return
	}
	o, ok, err := s.store.ObjectByRef(r.Context(), target)
	if err != nil {
		s.log.Error("reading a player", "ref", target.String(),
			"error", err)
		s.fail(w, r, "players.html", "that player could not be read")
		return
	}
	if !ok || ref.ObjType(o.Type) != ref.TypePlayer {
		s.fail(w, r, "players.html",
			target.String()+" is not a player")
		return
	}

	switch r.PostFormValue("action") {
	case "password":
		s.setPlayerPassword(w, r, o)
	case "flags":
		s.setPlayerFlags(w, r, o)
	default:
		s.fail(w, r, "players.html", "unknown action")
	}
}

// setPlayerPassword hashes and stores a new password.
//
// There is no "old password" field, on purpose: this is the interface
// somebody reaches for precisely because the old one is lost. What
// guards it is that only a wizard is here at all, and that the server
// has to be stopped.
func (s *Server) setPlayerPassword(w http.ResponseWriter,
	r *http.Request, o store.Object) {

	pass := r.PostFormValue("password")
	if pass == "" {
		s.fail(w, r, "players.html",
			"a password cannot be empty; Emerald refuses to "+
				"log in a character that has none")
		return
	}
	hash, err := password.Hash(pass)
	if err != nil {
		s.log.Error("hashing a password", "error", err)
		s.fail(w, r, "players.html", "the password could not be set")
		return
	}
	err = s.store.SetPassword(r.Context(), ref.Ref(o.Ref), hash)
	if err != nil {
		s.log.Error("writing a password", "ref", o.Ref,
			"error", err)
		s.fail(w, r, "players.html", "the password could not be set")
		return
	}
	// The password itself is never logged, here or anywhere.
	s.audit(r, "password set", "target", o.Name,
		"target_ref", ref.Ref(o.Ref).String())
	s.redirectNotice(w, r, "/players", strconv.Itoa(int(o.Ref)),
		o.Name+"'s password was changed.")
}

// setPlayerFlags turns the three bits this page offers on and off.
//
// Only those three are writable here. The rest of the flag word has
// meanings that depend on an object's type and on the game's own
// rules, and a checkbox is the wrong shape for them — the object
// inspector shows them, and the MUCK's own @set is where they belong.
func (s *Server) setPlayerFlags(w http.ResponseWriter,
	r *http.Request, o store.Object) {

	flags := ref.Flags(o.Flags)
	for _, f := range []struct {
		field string
		bit   ref.Flags
	}{
		{"wizard", ref.Wizard},
		{"builder", ref.Builder},
		{"quell", ref.Quell},
	} {
		if r.PostFormValue(f.field) != "" {
			flags |= f.bit
		} else {
			flags &^= f.bit
		}
	}
	if flags == ref.Flags(o.Flags) {
		s.redirectNotice(w, r, "/players",
			strconv.Itoa(int(o.Ref)), "Nothing changed.")
		return
	}

	err := s.store.SetFlags(r.Context(), ref.Ref(o.Ref), uint32(flags))
	if err != nil {
		s.log.Error("writing flags", "ref", o.Ref, "error", err)
		s.fail(w, r, "players.html", "the flags could not be set")
		return
	}
	s.audit(r, "flags set", "target", o.Name,
		"target_ref", ref.Ref(o.Ref).String(),
		"from", ref.Flags(o.Flags).Unparse(),
		"to", flags.Unparse())
	s.redirectNotice(w, r, "/players", strconv.Itoa(int(o.Ref)),
		o.Name+"'s flags were changed.")
}

// parseRef reads a dbref written either as "#123" or as "123".
func parseRef(s string) (ref.Ref, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return ref.Nothing, err
	}
	return ref.Ref(n), nil
}
