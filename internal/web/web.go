// Package web is the optional configurator: a small administrative
// interface over the same Postgres database the server uses.
//
// It is a separate binary against the same FBE_* variables, and it
// reads the database directly rather than loading a world. It has to:
// the server may be running, holding the authoritative object graph
// in its own memory, and a world loaded here would be a copy that
// went stale the moment it was taken.
//
// **Whether the server is running decides what this may do.** The
// liveness lease in internal/store answers that, and one middleware
// on every non-GET turns the answer into a refusal. Writing behind a
// running server's back would be writing into a copy it is about to
// overwrite.
//
// Stdlib only — net/http and html/template. The module has no
// router and no template engine, and this needs neither.
package web

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
)

//go:embed templates
var templateFS embed.FS

// Errors the handlers turn into messages. They are deliberately
// indistinguishable to a caller who is guessing: a wrong name and a
// wrong password give the same one.
var (
	errBadCredentials = errors.New("incorrect name or password")
	errNotAWizard     = errors.New(
		"that character is not a wizard")
)

// Options configure a Server.
type Options struct {
	Store      *store.Store
	Logger     *slog.Logger
	SessionTTL time.Duration
	// Version is shown in the footer, so an operator can tell
	// which build they are looking at.
	Version string
}

// Server serves the configurator.
type Server struct {
	store *store.Store
	log   *slog.Logger
	tmpl  *template.Template

	// now is the clock, replaceable so a test can pin the
	// timestamps an edit writes.
	now func() time.Time

	sessions *sessions
	throttle *throttle
	ttl      time.Duration
	version  string
}

// New builds a configurator. The templates are parsed once here, so a
// broken one fails at startup rather than on the request that reaches
// it.
func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = 2 * time.Hour
	}
	tmpl, err := template.New("").Funcs(templateFuncs()).
		ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{
		store:    opts.Store,
		log:      opts.Logger,
		tmpl:     tmpl,
		now:      time.Now,
		sessions: newSessions(opts.SessionTTL),
		throttle: newThrottle(),
		ttl:      opts.SessionTTL,
		version:  opts.Version,
	}, nil
}

// Handler builds the routing.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("POST /logout", s.required(s.postLogout))
	mux.HandleFunc("GET /{$}", s.required(s.getStatus))
	mux.HandleFunc("GET /tune", s.required(s.getTune))
	mux.HandleFunc("POST /tune", s.required(s.postTune))
	mux.HandleFunc("GET /help", s.required(s.getHelp))
	mux.HandleFunc("POST /help", s.required(s.postHelp))
	mux.HandleFunc("GET /players", s.required(s.getPlayers))
	mux.HandleFunc("POST /players", s.required(s.postPlayers))
	mux.HandleFunc("GET /objects", s.required(s.getObject))

	// Anything not matched above is a 404 rather than a redirect
	// to the login page: telling somebody who is not logged in
	// which paths exist is free information.
	mux.HandleFunc("/", s.notFound)

	return s.secureHeaders(s.readOnlyGuard(mux))
}

// ctxKey is the type this package's context values are keyed by, so
// nothing else can collide with them.
type ctxKey int

const sessionKey ctxKey = iota

// sessionFrom returns the logged-in session on a request.
func sessionFrom(r *http.Request) (session, bool) {
	sess, ok := r.Context().Value(sessionKey).(session)
	return sess, ok
}

// required wraps a handler so only a logged-in wizard reaches it.
func (s *Server) required(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			s.redirectToLogin(w, r)
			return
		}
		sess, ok := s.sessions.lookup(c.Value)
		if !ok {
			clearSessionCookie(w)
			s.redirectToLogin(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next(w, r.WithContext(ctx))
	}
}

// redirectToLogin sends a browser to the login page, and anything
// else a bare 401. A form post that lands here has lost its session,
// and a redirect would turn it into a GET and lose the body with it.
func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "your session has expired; log in again",
			http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// readOnlyGuard refuses every state-changing request while a server
// holds the world's lease.
//
// This is the whole of the read-only rule, and it is one place on
// purpose. Disabling the inputs in the templates is the cosmetic
// half; a form posted from a stale page, or a request made by hand,
// has to be refused here or not at all.
func (s *Server) readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet ||
			r.Method == http.MethodHead ||
			r.URL.Path == "/login" || r.URL.Path == "/logout" {
			next.ServeHTTP(w, r)
			return
		}
		held, err := s.store.LeaseHeld(r.Context())
		if err != nil {
			s.log.Error("checking the world lease",
				"error", err)
			http.Error(w, "cannot tell whether the server is "+
				"running; refusing to write",
				http.StatusServiceUnavailable)
			return
		}
		if held {
			http.Error(w, "the MUCK server is running; stop it "+
				"before changing anything here",
				http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secureHeaders sets the few that matter for an interface like this.
//
// The content security policy is the important one: everything here
// is served from the binary, so there is no reason for the page to
// fetch or run anything from anywhere.
func (s *Server) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; "+
				"form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// page is what every template is rendered with.
type page struct {
	Title string
	// Who is the logged-in wizard's name, empty on the login
	// page.
	Who string
	// ReadOnly reports that a server holds the world, so the
	// templates can say so and leave their inputs out.
	ReadOnly bool
	// LeaseError is set when the lease could not be read at all,
	// which is treated as read-only.
	LeaseError string
	Version    string
	// Error and Notice are messages for the top of the page.
	Error  string
	Notice string
	// Data is the page's own content.
	Data any
}

// newPage fills in everything the layout needs.
func (s *Server) newPage(r *http.Request, title string) *page {
	p := &page{Title: title, Version: s.version, ReadOnly: true}
	if sess, ok := sessionFrom(r); ok {
		p.Who = sess.Name
	}
	held, err := s.store.LeaseHeld(r.Context())
	switch {
	case err != nil:
		// Failing closed: not knowing whether a server is
		// running is not a reason to let somebody write.
		p.LeaseError = err.Error()
	default:
		p.ReadOnly = held
	}
	return p
}

// render writes a template, reporting a failure rather than leaving a
// half-written page.
func (s *Server) render(w http.ResponseWriter, name string, p *page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, p); err != nil {
		s.log.Error("rendering a page", "template", name,
			"error", err)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "no such page", http.StatusNotFound)
}

// templateFuncs are the handful of helpers the templates need.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"dbref": func(r int32) string { return ref.Ref(r).String() },
	}
}

// fail re-renders a page with an error rather than replacing it with
// a bare status line, so somebody who mistyped a value is still
// looking at the form they mistyped it in.
func (s *Server) fail(w http.ResponseWriter, r *http.Request,
	name, msg string) {

	p := s.newPage(r, "")
	p.Error = msg
	switch name {
	case "tune.html":
		p.Title = "Parameters"
		if d, err := s.tuneData(r); err == nil {
			p.Data = d
		}
	case "help.html":
		p.Title = "Manual"
		if d, err := s.helpData(r); err == nil {
			p.Data = d
		}
	case "players.html":
		p.Title = "Players"
		if d, err := s.playerData(r); err == nil {
			p.Data = d
		}
	}
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, name, p)
}

// redirectNotice sends the browser back to a page with a message,
// which is the post-redirect-get that stops a refresh from repeating
// a write.
func (s *Server) redirectNotice(w http.ResponseWriter,
	r *http.Request, path, changed, notice string) {

	u := url.URL{Path: path}
	q := u.Query()
	q.Set("notice", notice)
	if changed != "" {
		q.Set("changed", changed)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// audit records a change with who made it.
//
// Everything this interface writes bypasses the game, so the log is
// the only record that it happened at all: a wizard editing a
// password here leaves no trace in the MUCK's own logs.
func (s *Server) audit(r *http.Request, what string, args ...any) {
	who, _ := sessionFrom(r)
	head := []any{"who", who.Name, "player", who.Player.String(),
		"host", hostOf(r)}
	all := append(head, args...)
	s.log.Info(what, all...)
}
