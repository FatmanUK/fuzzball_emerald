package web

import (
	"errors"
	"net/http"

	"github.com/FatmanUK/fuzzball_emerald/internal/store"
)

// getLogin shows the login form. Somebody already logged in goes
// straight to the status page rather than being asked again.
func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, ok := s.sessions.lookup(c.Value); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	s.render(w, "login.html", s.newPage(r, "Sign in"))
}

// postLogin checks a wizard's MUCK credentials.
func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	host := hostOf(r)
	if s.throttle.blocked(host) {
		p := s.newPage(r, "Sign in")
		p.Error = throttleReason
		w.WriteHeader(http.StatusTooManyRequests)
		s.render(w, "login.html", p)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}

	name := r.PostFormValue("name")
	pass := r.PostFormValue("password")
	player, realName, err := s.authenticate(r.Context(), name, pass)
	if err != nil {
		s.throttle.fail(host)
		// The name is logged and the password is not, and the
		// two failures are distinguished here even though the
		// page does not distinguish them: an operator looking
		// at the log wants to know which it was.
		s.log.Warn("configurator login refused",
			"name", name, "host", host, "reason", err.Error())

		p := s.newPage(r, "Sign in")
		switch {
		case errors.Is(err, errBadCredentials),
			errors.Is(err, errNotAWizard):
			p.Error = errBadCredentials.Error()
		default:
			p.Error = "the database could not be reached"
		}
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, "login.html", p)
		return
	}

	token, err := s.sessions.create(player, realName)
	if err != nil {
		s.log.Error("issuing a session", "error", err)
		http.Error(w, "could not start a session",
			http.StatusInternalServerError)
		return
	}
	s.throttle.succeed(host)
	s.log.Info("configurator login", "name", realName,
		"player", player.String(), "host", host)

	setSessionCookie(w, token, s.ttl)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// postLogout ends a session. It is a post, not a link, so that
// another site cannot log somebody out by pointing at it.
func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.destroy(c.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// statusData is what the front page shows.
type statusData struct {
	Counts  store.Counts
	Top     string
	Live    bool
	Seeded  string
	HasTop  bool
	HasSeed bool
}

// getStatus is the front page: what the world holds, and whether a
// server is running against it.
func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Status")

	counts, err := s.store.CountObjects(r.Context())
	if err != nil {
		s.log.Error("counting objects", "error", err)
		p.Error = "the world could not be counted"
	}
	top, hasTop, err := s.store.Meta(r.Context(), "db_top")
	if err != nil {
		s.log.Error("reading db_top", "error", err)
	}
	seeded, hasSeed, err := s.store.Meta(r.Context(),
		"help_seed_version")
	if err != nil {
		s.log.Error("reading the help seed version", "error", err)
	}

	p.Data = statusData{
		Counts:  counts,
		Top:     top,
		HasTop:  hasTop,
		Seeded:  seeded,
		HasSeed: hasSeed,
		Live:    p.ReadOnly,
	}
	s.render(w, "status.html", p)
}
