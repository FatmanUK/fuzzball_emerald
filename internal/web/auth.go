package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Authentication is against the world's own wizards: the same name
// and password they connect to the MUCK with, read out of the objects
// table and checked with internal/password.
//
// There is deliberately no separate account store. A second set of
// credentials would be a second thing to get wrong, and anyone who
// can be trusted with this interface is already a wizard — it can
// do everything a wizard can, and some of it without the game
// watching.

// sessionCookie is the cookie a logged-in browser carries.
const sessionCookie = "fbeconfig_session"

// tokenBytes is how much entropy a session token carries. Thirty-two
// bytes is well past guessing; the tokens are not stored anywhere but
// memory, so there is no reason to be frugal.
const tokenBytes = 32

// session is one logged-in wizard.
type session struct {
	Player  ref.Ref
	Name    string
	Expires time.Time
}

// sessions holds the live logins.
//
// In memory and nowhere else: a restart logging everybody out is the
// right behaviour for an administrative interface, and it means a
// stolen database gives away no sessions.
type sessions struct {
	mu   sync.Mutex
	byID map[string]session
	ttl  time.Duration
	now  func() time.Time
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{
		byID: map[string]session{},
		ttl:  ttl,
		now:  time.Now,
	}
}

// create issues a token for a player.
func (s *sessions) create(player ref.Ref, name string) (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.byID[token] = session{
		Player:  player,
		Name:    name,
		Expires: s.now().Add(s.ttl),
	}
	return token, nil
}

// lookup returns a live session, sliding its expiry forward so that
// somebody working in the interface is not logged out under them.
func (s *sessions) lookup(token string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byID[token]
	if !ok {
		return session{}, false
	}
	if !s.now().Before(sess.Expires) {
		delete(s.byID, token)
		return session{}, false
	}
	sess.Expires = s.now().Add(s.ttl)
	s.byID[token] = sess
	return sess, true
}

func (s *sessions) destroy(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, token)
}

// sweepLocked drops expired sessions. It runs on create rather than
// on a timer: nothing else grows the map, so that is the only moment
// it can need it.
func (s *sessions) sweepLocked() {
	now := s.now()
	for id, sess := range s.byID {
		if !now.Before(sess.Expires) {
			delete(s.byID, id)
		}
	}
}

// Throttling is per address and counts failures only. A wizard who
// types their password correctly is never delayed, and somebody
// working through a word list gets a handful of tries a minute.
const (
	maxFailures    = 5
	failureWindow  = time.Minute
	throttleReason = "too many failed attempts; wait a minute"
)

type throttle struct {
	mu    sync.Mutex
	hosts map[string]*failures
	now   func() time.Time
}

type failures struct {
	count int
	since time.Time
}

func newThrottle() *throttle {
	return &throttle{hosts: map[string]*failures{}, now: time.Now}
}

// blocked reports whether an address has spent its attempts.
func (t *throttle) blocked(host string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.hosts[host]
	if f == nil {
		return false
	}
	if t.now().Sub(f.since) > failureWindow {
		delete(t.hosts, host)
		return false
	}
	return f.count >= maxFailures
}

// fail records a failed attempt.
func (t *throttle) fail(host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.hosts[host]
	if f == nil || t.now().Sub(f.since) > failureWindow {
		t.hosts[host] = &failures{count: 1, since: t.now()}
		return
	}
	f.count++
}

// succeed clears an address's record, so one person's typo does not
// lock out the office they share an address with for a minute after
// they get it right.
func (t *throttle) succeed(host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.hosts, host)
}

// authenticate checks a name and password against the world's
// wizards.
//
// A correct password stored in a legacy format is **not** rehashed
// here, which the MUCK does on login. Writing is exactly what this
// process may be forbidden to do while a server is running, and a
// login that sometimes writes and sometimes does not is worse than
// one that never does.
func (s *Server) authenticate(ctx context.Context,
	name, pass string) (ref.Ref, string, error) {

	o, ok, err := s.store.PlayerByName(ctx, name)
	if err != nil {
		return ref.Nothing, "", err
	}
	if !ok {
		// The same answer as a wrong password, so this cannot
		// be used to find out who exists.
		return ref.Nothing, "", errBadCredentials
	}
	if o.PasswordHash == password.NoPassword {
		return ref.Nothing, "", errBadCredentials
	}
	if !password.Verify(o.PasswordHash, pass).OK {
		return ref.Nothing, "", errBadCredentials
	}
	// Quell is a wizard setting their powers aside inside the
	// game; it is not a statement about who they are, and it is
	// not checked here.
	if !ref.Flags(o.Flags).IsTrueWizard() {
		return ref.Nothing, "", errNotAWizard
	}
	return ref.Ref(o.Ref), o.Name, nil
}

// hostOf is the address a request came from, for throttling. The port
// is dropped so that a new connection does not read as a new client.
func hostOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// setSessionCookie issues the cookie a logged-in browser carries.
//
// Secure, because this is served over TLS and a cookie that would go
// out in the clear is one a network can take. HttpOnly, so a script
// cannot read it. SameSite=Strict, because every state-changing route
// here is a form post and none of them should ever be reachable from
// another site.
func setSessionCookie(w http.ResponseWriter, token string,
	ttl time.Duration) {

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearSessionCookie removes it.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
