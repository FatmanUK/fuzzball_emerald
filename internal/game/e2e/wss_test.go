package e2e

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/transport/wss"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// startWSS serves the starter world over WebSocket-over-TLS.
func startWSS(t *testing.T) (url string, client *tls.Config) {
	t.Helper()

	res, err := importer.Load(importer.Source{
		DumpPath: "../../../testdata/starterdb/starterdb.db",
	})
	if err != nil {
		t.Skipf("starter world unavailable: %v", err)
	}

	for _, prog := range res.Programs {
		res.World.SetSource(prog.Ref, prog.Source)
	}
	macros := make([]world.Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, world.Macro{
			Name: m.Name, Definition: m.Definition, Owner: m.Owner,
		})
	}
	res.World.SetMacros(macros)

	engine := world.NewEngine(res.World, world.Options{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()

	gs := game.New(engine, game.Options{})

	serverTLS := selfSignedTLS(t)

	ls, err := wss.New("127.0.0.1:0", serverTLS, gs, wss.Options{Path: "/muck"})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ls.Serve(ctx) }()

	t.Cleanup(func() {
		ls.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the world goroutine did not stop")
		}
	})

	return "wss://" + ls.Addr().String() + "/muck",
		&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}
}

// wsClient wraps a WebSocket connection for the test.
type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context
}

func dialWSS(t *testing.T, url string, cfg *tls.Config) *wsClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: cfg},
		},
	})
	if err != nil {
		t.Fatalf("dialling %s: %v", url, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return &wsClient{t: t, conn: conn, ctx: ctx}
}

func (c *wsClient) send(line string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(line)); err != nil {
		c.t.Fatalf("writing %q: %v", line, err)
	}
}

// expect reads messages until want appears.
func (c *wsClient) expect(want string) string {
	c.t.Helper()
	var sb strings.Builder
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.t.Fatalf("did not see %q (%v); transcript:\n%s", want, err, sb.String())
		}
		sb.Write(data)
		sb.WriteByte('\n')
		if strings.Contains(sb.String(), want) {
			return sb.String()
		}
	}
}

// TestWSSConnectAndLook checks the WebSocket transport reaches the same world
// through the same descriptor abstraction as the line transport.
func TestWSSConnectAndLook(t *testing.T) {
	url, cfg := startWSS(t)
	c := dialWSS(t, url, cfg)

	c.expect("Fuzzball Emerald")

	c.send("connect One potrzebie")
	got := c.expect("Room Zero")
	if strings.Contains(got, "Either that player") {
		t.Fatalf("login was refused:\n%s", got)
	}

	// "say" is an exit in this world; "!" reaches the built-in.
	c.send("!say hello over websockets")
	c.expect(`You say, "hello over websockets"`)

	// The same interface-command rules apply on both transports.
	c.send("WHO")
	got = c.expect("player connected")
	if !strings.Contains(got, "One") {
		t.Errorf("WHO did not list One:\n%s", got)
	}
}
