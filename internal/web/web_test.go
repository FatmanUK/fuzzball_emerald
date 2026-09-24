package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// schemaSeq keeps test schema names distinct within a run.
var schemaSeq atomic.Int64

// testServer builds a configurator over a scratch schema holding one
// wizard and one mortal, or skips if no test database is configured.
func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dsn := os.Getenv("FBE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set FBE_TEST_DATABASE_URL to run web tests")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("fbe_web_%d_%d", os.Getpid(),
		schemaSeq.Add(1))

	admin, err := store.Open(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	if err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	scoped, err := withSchema(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, scoped, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// One wizard and one mortal, written through a world so the
	// rows are exactly what the server would have produced.
	w := world.New()
	hash, err := password.Hash("potrzebie")
	if err != nil {
		t.Fatal(err)
	}
	wiz := w.Create("Igor", ref.TypePlayer, ref.Nothing)
	wiz.Owner = wiz.Ref
	wiz.Flags |= ref.Wizard
	wiz.PasswordHash = hash

	mortal := w.Create("Mortal", ref.TypePlayer, ref.Nothing)
	mortal.Owner = mortal.Ref
	mortal.PasswordHash = hash

	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	srv, err := New(Options{Store: st, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

// withSchema puts a search_path in a DSN, the way the store's own
// tests do: GORM pools connections, so a SET reaches exactly one of
// them and every other query lands in public.
func withSchema(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// login posts credentials and returns the session cookie, if one was
// issued.
func login(t *testing.T, h http.Handler, name, pass string) (
	*http.Cookie, *httptest.ResponseRecorder) {

	t.Helper()
	form := url.Values{"name": {name}, "password": {pass}}
	r := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type",
		"application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c, w
		}
	}
	return nil, w
}

func TestLoginAcceptsAWizard(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	c, w := login(t, h, "Igor", "potrzebie")
	if c == nil {
		t.Fatalf("no session cookie; status %d body %q",
			w.Code, w.Body.String())
	}
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect", w.Code)
	}
	if !c.HttpOnly || !c.Secure ||
		c.SameSite != http.SameSiteStrictMode {
		t.Errorf("the session cookie is not protected: %+v", c)
	}

	// The name is matched case-insensitively, as the MUCK does.
	if c, _ := login(t, h, "IGOR", "potrzebie"); c == nil {
		t.Error("the name was matched case-sensitively")
	}
}

func TestLoginRefusals(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	for _, tc := range []struct{ what, name, pass string }{
		{"a wrong password", "Igor", "wrong"},
		{"an unknown name", "Nobody", "potrzebie"},
		{"a mortal", "Mortal", "potrzebie"},
	} {
		// Each case gets a fresh server so the throttle from
		// the previous one does not answer for it.
		srv, _ = testServer(t)
		h = srv.Handler()
		c, w := login(t, h, tc.name, tc.pass)
		if c != nil {
			t.Errorf("%s was let in", tc.what)
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", tc.what, w.Code)
		}
		// A mortal and an unknown name must look the same, or
		// this becomes a way to enumerate wizards.
		if !strings.Contains(w.Body.String(),
			"incorrect name or password") {
			t.Errorf("%s: body gave the reason away:\n%s",
				tc.what, w.Body.String())
		}
	}
}

func TestLoginThrottles(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	for i := 0; i < maxFailures; i++ {
		login(t, h, "Igor", "wrong")
	}
	_, w := login(t, h, "Igor", "wrong")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status after %d failures = %d, want 429",
			maxFailures+1, w.Code)
	}
	// The right password is refused too while the window holds:
	// the throttle is on the address, not on the attempt.
	if c, _ := login(t, h, "Igor", "potrzebie"); c != nil {
		t.Error("the throttle let a login through")
	}
}

// TestThrottleClearsOnSuccess checks that one person's typo does not
// lock out everybody sharing their address.
func TestThrottleClearsOnSuccess(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	for i := 0; i < maxFailures-1; i++ {
		login(t, h, "Igor", "wrong")
	}
	if c, _ := login(t, h, "Igor", "potrzebie"); c == nil {
		t.Fatal("a correct password was refused")
	}
	for i := 0; i < maxFailures-1; i++ {
		login(t, h, "Igor", "wrong")
	}
	if c, _ := login(t, h, "Igor", "potrzebie"); c == nil {
		t.Error("the failure count was not cleared by a success")
	}
}

func TestStatusNeedsALogin(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect to /login", w.Code)
	}

	c, _ := login(t, h, "Igor", "potrzebie")
	if c == nil {
		t.Fatal("could not log in")
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Players") {
		t.Errorf("the status page is missing its counts:\n%s",
			w.Body.String())
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	c, _ := login(t, h, "Igor", "potrzebie")
	if c == nil {
		t.Fatal("could not log in")
	}
	r := httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Error("the session outlived the logout")
	}
}

// TestReadOnlyWhileTheServerRuns is the rule the whole configurator
// rests on. It is checked at the middleware, not in the templates: a
// form posted from a page loaded before the server started has to be
// refused, and so does a request made by hand.
func TestReadOnlyWhileTheServerRuns(t *testing.T) {
	srv, st := testServer(t)
	h := srv.Handler()
	ctx := context.Background()

	c, _ := login(t, h, "Igor", "potrzebie")
	if c == nil {
		t.Fatal("could not log in")
	}

	lease, err := st.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	// A write while the world is leased is refused.
	r := httptest.NewRequest(http.MethodPost, "/players", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("a write during a running server gave %d, want 409",
			w.Code)
	}

	// Reading still works, and says so.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reading gave %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "read-only") {
		t.Errorf("the page does not say it is read-only:\n%s",
			w.Body.String())
	}

	// With the world free, the same request reaches the router
	// and gets a 404 rather than a 409 -- the guard is out of the
	// way.
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/players", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusConflict {
		t.Error("writes are still refused with no server running")
	}
}

// TestLoginIsNotBlockedByTheLease checks the one exception: signing
// in is a POST, and refusing it while a server runs would make the
// interface unusable for exactly the case it exists to report on.
func TestLoginIsNotBlockedByTheLease(t *testing.T) {
	srv, st := testServer(t)
	h := srv.Handler()

	lease, err := st.AcquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	if c, w := login(t, h, "Igor", "potrzebie"); c == nil {
		t.Errorf("login was refused while the server runs: %d %s",
			w.Code, w.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if got := w.Header().Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q",
				header, got, want)
		}
	}
}

func TestSessionsExpire(t *testing.T) {
	s := newSessions(time.Minute)
	now := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return now }

	token, err := s.create(ref.Ref(1), "Igor")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.lookup(token); !ok {
		t.Fatal("a fresh session did not resolve")
	}

	// Working in the interface keeps it alive.
	now = now.Add(50 * time.Second)
	if _, ok := s.lookup(token); !ok {
		t.Error("the session expired while it was in use")
	}
	now = now.Add(50 * time.Second)
	if _, ok := s.lookup(token); !ok {
		t.Error("the expiry did not slide forward")
	}

	now = now.Add(2 * time.Minute)
	if _, ok := s.lookup(token); ok {
		t.Error("an idle session outlived its TTL")
	}
	if len(s.byID) != 0 {
		t.Error("the expired session was not dropped")
	}
}
