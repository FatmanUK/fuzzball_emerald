// Package e2e drives a real server over a real TLS socket.
//
// It is the M3 acceptance check: dial the listener, log in as the
// starter world's #1, walk the world and read what comes back.
package e2e

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/transport/tlsline"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// selfSignedTLS builds a throwaway certificate for the test listener.
func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}},
		MinVersion: tls.VersionTLS13,
	}
}

// testServer is a running server with its address.
type testServer struct {
	addr string
	tls  *tls.Config
	w    *world.World
}

// startServer loads the starter world and serves it over TLS on a
// free port.
func startServer(t *testing.T) *testServer {
	t.Helper()

	res, err := importer.Load(importer.Source{
		DumpPath: "../../../testdata/starterdb/starterdb.db",
	})
	if err != nil {
		t.Skipf("starter world unavailable: %v", err)
	}
	w := res.World
	for _, prog := range res.Programs {
		w.SetSource(prog.Ref, prog.Source)
	}
	macros := make([]world.Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, world.Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      m.Owner,
		})
	}
	w.SetMacros(macros)

	engine := world.NewEngine(w, world.Options{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	worldDone := make(chan error, 1)
	go func() { worldDone <- engine.Run(ctx) }()

	gs := game.New(engine, game.Options{})

	cfg := selfSignedTLS(t)

	ln, err := tlsline.New("127.0.0.1:0", cfg, gs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ln.Serve(ctx) }()

	t.Cleanup(func() {
		ln.Close()
		cancel()
		select {
		case <-worldDone:
		case <-time.After(5 * time.Second):
			t.Error("the world goroutine did not stop")
		}
	})

	return &testServer{
		addr: ln.Addr().String(),
		tls:  &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13},
		w:    w,
	}
}

// client is a test connection to the server.
type client struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
}

// dial opens a TLS connection.
func (ts *testServer) dial(t *testing.T) *client {
	t.Helper()
	conn, err := tls.Dial("tcp", ts.addr, ts.tls)
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &client{t: t, conn: conn, br: bufio.NewReader(conn)}
}

// send writes one line.
func (c *client) send(line string) {
	c.t.Helper()
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		c.t.Fatalf("writing %q: %v", line, err)
	}
}

// readUntil collects output until want appears, or the deadline
// passes. It returns everything read, so a failure can show the whole
// transcript.
func (c *client) readUntil(want string, timeout time.Duration) (string, bool) {
	c.t.Helper()
	var sb strings.Builder
	deadline := time.Now().Add(timeout)
	for {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return sb.String(), false
		}
		line, err := c.br.ReadString('\n')
		sb.WriteString(line)
		if strings.Contains(sb.String(), want) {
			return sb.String(), true
		}
		if err != nil {
			return sb.String(), false
		}
		if time.Now().After(deadline) {
			return sb.String(), false
		}
	}
}

// expect reads until want appears, failing the test if it does not.
func (c *client) expect(want string) string {
	c.t.Helper()
	got, ok := c.readUntil(want, 5*time.Second)
	if !ok {
		c.t.Fatalf("did not see %q; transcript:\n%s", want, got)
	}
	return got
}

// drain reads whatever is available, so later reads are not confused
// by it.
func (c *client) drain(d time.Duration) string {
	var sb strings.Builder
	_ = c.conn.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 4096)
	for {
		n, err := c.conn.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String()
		}
	}
}

// dbrefIn extracts the "#123" a creation message reports.
func dbrefIn(t *testing.T, transcript string) string {
	t.Helper()
	// Creation messages name the new object the way examine does,
	// as "Name(#123FLAGS)", so the dbref runs from the '#' to the
	// first non-digit after it.
	i := strings.Index(transcript, "(#")
	if i < 0 {
		t.Fatalf("no dbref in %q", transcript)
	}
	rest := transcript[i+1:]
	end := 1
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 1 {
		t.Fatalf("no dbref in %q", transcript)
	}
	return rest[:end]
}

// TestConnectAndWalkTheWorld is the M3 acceptance check.
func TestConnectAndWalkTheWorld(t *testing.T) {
	ts := startServer(t)
	c := ts.dial(t)

	// The welcome banner arrives unprompted.
	c.expect("Fuzzball Emerald")

	// #1's password is documented in the starter world's README.
	c.send("connect One potrzebie")

	// Logging in shows the room, so its name proves both the
	// login and the look worked.
	transcript := c.expect("Room Zero")
	if strings.Contains(transcript, "Either that player") {
		t.Fatalf("login was refused:\n%s", transcript)
	}
	t.Logf("login transcript:\n%s", transcript)

	// The room has a description.
	c.send("look")
	got := c.expect("Room Zero")
	if !strings.Contains(got, "dark") &&
		!strings.Contains(got, "You see nothing special") {
		t.Errorf("look produced no description:\n%s", got)
	}

	// WHO lists the connected player.
	c.send("WHO")
	got = c.expect("player connected")
	if !strings.Contains(got, "One") {
		t.Errorf("WHO did not list One:\n%s", got)
	}

	// Speech is echoed back. The starter world defines its own
	// "say" exit, which wins over the built-in. A wizard's "!"
	// prefix skips exit matching to reach ours.
	c.send("!say hello world")
	c.expect(`You say, "hello world"`)

	// A pose uses the player's name.
	c.send(":waves")
	c.expect("One waves")

	// Inventory works even when empty.
	c.send("inventory")
	c.expect("carrying")

	// Building: make a room, an exit into it, and walk through.
	c.send("@dig Test Chamber")
	got = c.expect("created.")
	t.Logf("dig: %s", strings.TrimSpace(got))
	// A room that was just dug is somewhere else entirely, so it
	// is linked by dbref, which is the normal idiom.
	room := dbrefIn(t, got)

	c.send("@open testexit=" + room)
	c.expect("Linked to")

	c.send("testexit")
	c.expect("Test Chamber")

	// And back out again, by teleporting home. This world's
	// "@tel" exit lists "@teleport" among its aliases, so the
	// built-in needs the wizard override to reach.
	c.send("!@teleport me=#0")
	c.expect("Room Zero")

	c.send("QUIT")
	c.expect("Goodbye")
}

// TestBadPasswordIsRefused checks the failure path does not leak
// which half was wrong.
func TestBadPasswordIsRefused(t *testing.T) {
	ts := startServer(t)
	c := ts.dial(t)
	c.expect("Fuzzball Emerald")

	c.send("connect One wrongpassword")
	got := c.expect("player does not exist")
	if strings.Contains(got, "Room Zero") {
		t.Errorf("a bad password logged in:\n%s", got)
	}

	// The same message for an unknown player, so the two are not
	// distinguishable from outside.
	c.send("connect NoSuchPlayer whatever")
	c.expect("player does not exist")
}

// TestLegacyPasswordIsUpgraded checks that logging in with a password
// stored in Fuzzball's format rewrites it as Argon2id.
func TestLegacyPasswordIsUpgraded(t *testing.T) {
	ts := startServer(t)

	one, ok := ts.w.PlayerNamed("One")
	if !ok {
		t.Fatal("player One is missing from the starter world")
	}
	before := ts.w.Get(one).PasswordHash
	if !strings.HasPrefix(before, "$fbmd5$") {
		t.Skipf("the fixture's password is not in the legacy format: %q", before)
	}

	c := ts.dial(t)
	c.expect("Fuzzball Emerald")
	c.send("connect One potrzebie")
	c.expect("Room Zero")

	// Give the upgrade a moment to land on the world goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.HasPrefix(ts.w.Get(one).PasswordHash, "$argon2id$") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the password was not upgraded; it is still %q",
		ts.w.Get(one).PasswordHash[:16])
}

// TestTwoPlayersSeeEachOther checks the notification fan-out.
func TestTwoPlayersSeeEachOther(t *testing.T) {
	ts := startServer(t)

	a := ts.dial(t)
	a.expect("Fuzzball Emerald")
	a.send("connect One potrzebie")
	a.expect("Room Zero")

	// Put Keeper in the same room as One, so they can see each
	// other. Registration is on in the starter world, which means
	// characters are made out of band rather than at the login
	// screen. Turn it off, as an operator would.
	a.send("@tune registration=no")
	a.expect("registration set to")

	b := ts.dial(t)
	b.expect("Fuzzball Emerald")
	b.send("create Visitor hunter2")
	// A new character starts wherever player_start points, which
	// in the starter world is not Room Zero.
	b.expect("Cave of Awakening")

	// One is a wizard, so can bring the newcomer along.
	a.send("!@teleport Visitor=#0")
	a.expect("Teleported")
	b.drain(300 * time.Millisecond)
	a.drain(300 * time.Millisecond)

	// One speaks and Visitor listens. The wizard override is
	// needed to reach the built-in past this world's "say" exit,
	// and only One has it.
	a.send("!say knock knock")
	got := b.expect("knock knock")
	if !strings.Contains(got, "One says") {
		t.Errorf("Visitor did not hear One:\n%s", got)
	}
}
