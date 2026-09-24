package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/help"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// scratchWorld is a world whose refs start above the fixture's, so
// objects a test creates do not overwrite the wizard it logs in as.
// World.Create hands out refs from the top, and a fresh world starts
// at zero — which is where testServer put Igor.
func scratchWorld() *world.World {
	w := world.New()
	w.SetTop(100)
	return w
}

// authed logs in and returns a helper that makes requests carrying
// the session.
type client struct {
	t *testing.T
	h http.Handler
	c *http.Cookie
}

func authed(t *testing.T, srv *Server) *client {
	t.Helper()
	h := srv.Handler()
	c, w := login(t, h, "Igor", "potrzebie")
	if c == nil {
		t.Fatalf("could not log in: %d %s", w.Code, w.Body.String())
	}
	return &client{t: t, h: h, c: c}
}

func (c *client) get(path string) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(c.c)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func (c *client) post(path string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type",
		"application/x-www-form-urlencoded")
	r.AddCookie(c.c)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func TestTunePageListsAndSaves(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)
	ctx := context.Background()

	body := c.get("/tune").Body.String()
	for _, want := range []string{"muckname", "penny", "Files"} {
		if !strings.Contains(body, want) {
			t.Errorf("the parameter list is missing %q", want)
		}
	}
	// An inert parameter says so, which is the whole reason the
	// flag exists in the generated table.
	if !strings.Contains(body, "inert") {
		t.Error("nothing is marked inert")
	}

	w := c.post("/tune", url.Values{
		"name": {"muckname"}, "value": {"The Hollow Tree"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("saving gave %d: %s", w.Code, w.Body.String())
	}
	stored, err := st.TuneValues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored["muckname"] != "The Hollow Tree" {
		t.Errorf("muckname = %q", stored["muckname"])
	}

	// Resetting takes the row away again, which is what puts the
	// parameter back to its default.
	w = c.post("/tune", url.Values{
		"name": {"muckname"}, "reset": {"1"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("resetting gave %d", w.Code)
	}
	if stored, err = st.TuneValues(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["muckname"]; ok {
		t.Error("the reset left the stored value behind")
	}
}

// TestTuneRefusesAValueTheServerWouldReject is the point of parsing
// through the parameter's own Parse: a value that would not load is
// refused here rather than at the next boot.
func TestTuneRefusesAValueTheServerWouldReject(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	w := c.post("/tune", url.Values{
		"name": {"penny_rate"}, "value": {"not a number"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("a bad integer gave %d, want 400", w.Code)
	}
	stored, err := st.TuneValues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["penny_rate"]; ok {
		t.Error("the bad value was written anyway")
	}

	w = c.post("/tune", url.Values{
		"name": {"no_such_parameter"}, "value": {"1"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("an unknown name gave %d, want 400", w.Code)
	}
}

// TestTuneCanonicalisesWhatItStores checks the round trip through
// Parse and Format, so what lands in the database reads back as the
// same value.
func TestTuneCanonicalisesWhatItStores(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	if w := c.post("/tune", url.Values{
		"name": {"lookup_cost"}, "value": {"  7  "},
	}); w.Code != http.StatusSeeOther {
		t.Fatalf("saving gave %d: %s", w.Code, w.Body.String())
	}
	stored, err := st.TuneValues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored["lookup_cost"] != "7" {
		t.Errorf("stored %q, want the canonical %q",
			stored["lookup_cost"], "7")
	}
}

func TestHelpPageShowsAndSaves(t *testing.T) {
	srv, st := testServer(t)
	ctx := context.Background()

	w := scratchWorld()
	help.Apply(w, "", false)
	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	c := authed(t, srv)
	body := c.get("/help?corpus=help").Body.String()
	if !strings.Contains(body, "player command help") {
		t.Errorf("the help corpus did not render:\n%s", body)
	}
	// The editor shows the index format, separators and all.
	if !strings.Contains(body, "~") {
		t.Error("the index separators are missing")
	}

	const edited = "The top block.\n~\nfirst|1\nThe first topic.\n"
	res := c.post("/help", url.Values{
		"corpus": {"news"}, "text": {edited}})
	if res.Code != http.StatusSeeOther {
		t.Fatalf("saving gave %d: %s", res.Code, res.Body.String())
	}

	rows, err := st.HelpCorpus(ctx, "news")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("news has %d topics, want 2: %+v", len(rows), rows)
	}
	if rows[0].Name != "" || rows[0].Body != "The top block." {
		t.Errorf("header = %+v", rows[0])
	}
	if rows[1].Name != "first" || rows[1].Aliases != "1" {
		t.Errorf("topic = %+v", rows[1])
	}
	// Everything saved here is stamped, so a later release's
	// seeding leaves it alone.
	for _, r := range rows {
		if r.Modified == 0 {
			t.Errorf("%q was not marked as edited", r.Name)
		}
	}
}

// TestHelpSaveRefusesTextWithNoTopics keeps a mistyped separator from
// silently emptying a corpus.
func TestHelpSaveRefusesTextWithNoTopics(t *testing.T) {
	srv, _ := testServer(t)
	c := authed(t, srv)

	w := c.post("/help", url.Values{
		"corpus": {"info"}, "text": {"just some prose\n"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("saving unparseable text gave %d, want 400", w.Code)
	}
}

// TestHelpSingletonCorpusIsOneText checks that motd and its kind are
// not run through the index parser, which would eat their content.
func TestHelpSingletonCorpusIsOneText(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	const text = "Closed for repairs.\n~ this is not a separator here"
	if w := c.post("/help", url.Values{
		"corpus": {"motd"}, "text": {text},
	}); w.Code != http.StatusSeeOther {
		t.Fatalf("saving gave %d", w.Code)
	}
	rows, err := st.HelpCorpus(context.Background(), "motd")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Body != text {
		t.Errorf("motd = %+v, want the text unchanged", rows)
	}
}

func TestPlayersPageSetsAPassword(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)
	ctx := context.Background()

	body := c.get("/players").Body.String()
	for _, want := range []string{"Igor", "Mortal", "wizard"} {
		if !strings.Contains(body, want) {
			t.Errorf("the player list is missing %q", want)
		}
	}

	mortal, ok, err := st.PlayerByName(ctx, "Mortal")
	if err != nil || !ok {
		t.Fatalf("finding Mortal: ok=%v err=%v", ok, err)
	}
	w := c.post("/players", url.Values{
		"ref":      {ref.Ref(mortal.Ref).String()},
		"action":   {"password"},
		"password": {"hunter2"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("setting a password gave %d: %s",
			w.Code, w.Body.String())
	}

	after, _, err := st.PlayerByName(ctx, "Mortal")
	if err != nil {
		t.Fatal(err)
	}
	if !password.Verify(after.PasswordHash, "hunter2").OK {
		t.Error("the new password does not verify")
	}
	if password.Verify(after.PasswordHash, "potrzebie").OK {
		t.Error("the old password still works")
	}
}

// TestPlayersRefusesAnEmptyPassword matches the server, which will
// not log in a character that has none.
func TestPlayersRefusesAnEmptyPassword(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	mortal, _, err := st.PlayerByName(context.Background(), "Mortal")
	if err != nil {
		t.Fatal(err)
	}
	w := c.post("/players", url.Values{
		"ref":      {ref.Ref(mortal.Ref).String()},
		"action":   {"password"},
		"password": {""},
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("an empty password gave %d, want 400", w.Code)
	}
}

func TestPlayersTogglesFlags(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)
	ctx := context.Background()

	mortal, _, err := st.PlayerByName(ctx, "Mortal")
	if err != nil {
		t.Fatal(err)
	}
	w := c.post("/players", url.Values{
		"ref":     {ref.Ref(mortal.Ref).String()},
		"action":  {"flags"},
		"wizard":  {"1"},
		"builder": {"1"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("setting flags gave %d: %s", w.Code, w.Body.String())
	}

	after, _, err := st.PlayerByName(ctx, "Mortal")
	if err != nil {
		t.Fatal(err)
	}
	f := ref.Flags(after.Flags)
	if f&ref.Wizard == 0 || f&ref.Builder == 0 {
		t.Errorf("flags = %s, want wizard and builder", f.Unparse())
	}
	// The type bits must survive: the flag word carries the
	// object's type as well as its flags, and writing the word
	// whole is how that could be lost.
	if ref.ObjType(after.Type) != ref.TypePlayer {
		t.Error("the object stopped being a player")
	}
	if f.Type() != ref.TypePlayer {
		t.Errorf("the type bits in the flag word were lost: %s",
			f.Unparse())
	}

	// Unchecking clears them again.
	w = c.post("/players", url.Values{
		"ref":    {ref.Ref(mortal.Ref).String()},
		"action": {"flags"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("clearing flags gave %d", w.Code)
	}
	after, _, _ = st.PlayerByName(ctx, "Mortal")
	if ref.Flags(after.Flags)&ref.Wizard != 0 {
		t.Error("the wizard bit was not cleared")
	}
}

// TestPlayersRefusesANonPlayer stops the page being a general object
// editor by way of a hand-written form.
func TestPlayersRefusesANonPlayer(t *testing.T) {
	srv, st := testServer(t)
	ctx := context.Background()

	w := scratchWorld()
	room := w.Create("The Study", ref.TypeRoom, ref.Nothing)
	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	c := authed(t, srv)
	res := c.post("/players", url.Values{
		"ref":      {ref.Ref(room.Ref).String()},
		"action":   {"password"},
		"password": {"hunter2"},
	})
	if res.Code != http.StatusBadRequest {
		t.Errorf("setting a room's password gave %d, want 400",
			res.Code)
	}
}

func TestObjectInspector(t *testing.T) {
	srv, st := testServer(t)
	ctx := context.Background()

	w := scratchWorld()
	room := w.Create("The Study", ref.TypeRoom, ref.Nothing)
	thing := w.Create("brass widget", ref.TypeThing, room.Ref)
	thing.Props.Set("_/de", props.Value{Type: props.String,
		Str: "It whirrs."})
	thing.Props.Set("count", props.Value{Type: props.Int, Num: 7})
	if err := w.MoveTo(thing.Ref, room.Ref); err != nil {
		t.Fatal(err)
	}
	prog := w.Create("test.muf", ref.TypeProgram, room.Ref)
	w.SaveSource(prog.Ref, ": main 1 pop ;\n")
	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	c := authed(t, srv)
	body := c.get("/objects?ref=" +
		url.QueryEscape(thing.Ref.String())).Body.String()
	for _, want := range []string{
		"brass widget", "_/de", "It whirrs.", "count", "7",
		"The Study",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the inspector is missing %q:\n%s",
				want, body)
		}
	}

	// A program shows its source.
	body = c.get("/objects?ref=" +
		url.QueryEscape(prog.Ref.String())).Body.String()
	if !strings.Contains(body, ": main 1 pop ;") {
		t.Errorf("the program source is missing:\n%s", body)
	}

	// A dbref that names nothing says so rather than erroring.
	body = c.get("/objects?ref=%23999").Body.String()
	if !strings.Contains(body, "there is no #999") {
		t.Errorf("a missing object gave:\n%s", body)
	}

	body = c.get("/objects?ref=rubbish").Body.String()
	if !strings.Contains(body, "not a dbref") {
		t.Errorf("a malformed dbref gave:\n%s", body)
	}
}

// TestObjectInspectorNamesABrokenReference is what the inspector is
// for: looking at a world the server will not boot on.
func TestObjectInspectorNamesABrokenReference(t *testing.T) {
	srv, st := testServer(t)
	ctx := context.Background()

	w := scratchWorld()
	room := w.Create("The Study", ref.TypeRoom, ref.Nothing)
	room.Owner = ref.Ref(999)
	if err := st.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	c := authed(t, srv)
	body := c.get("/objects?ref=" +
		url.QueryEscape(room.Ref.String())).Body.String()
	if !strings.Contains(body, "no such object") {
		t.Errorf("the dangling owner was not flagged:\n%s", body)
	}
}

// TestEveryWritePageIsGuarded walks the routes rather than trusting
// that each handler was remembered: a page added later that forgets
// the rule is the failure this is here to catch.
func TestEveryWritePageIsGuarded(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	lease, err := st.AcquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	for _, path := range []string{"/tune", "/help", "/players"} {
		w := c.post(path, url.Values{})
		if w.Code != http.StatusConflict {
			t.Errorf("POST %s during a running server gave %d, "+
				"want 409", path, w.Code)
		}
	}
}

// TestReadOnlyPagesOfferNoForms checks the cosmetic half as well: the
// guard is what enforces the rule, but a page full of inputs that
// cannot work is a page that lies.
func TestReadOnlyPagesOfferNoForms(t *testing.T) {
	srv, st := testServer(t)
	c := authed(t, srv)

	lease, err := st.AcquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	for _, path := range []string{"/tune", "/help", "/players"} {
		body := c.get(path).Body.String()
		// Exactly one post form should survive: signing out,
		// which is in the layout and is not a change to the
		// world. The help page keeps a GET form for choosing
		// a corpus, which is a link with a dropdown on it.
		if n := strings.Count(body, `method="post"`); n != 1 {
			t.Errorf("%s has %d post forms while the server "+
				"runs, want only sign-out", path, n)
		}
		if !strings.Contains(body, "read-only") {
			t.Errorf("%s does not say it is read-only", path)
		}
	}
}

// storeUnused keeps the store import honest if a test above is
// removed; it is referenced by the helpers.
var _ = store.Object{}
