// Package wss serves the same session protocol over WebSocket, for clients
// that run in a browser.
//
// A WebSocket message is already framed, so there is no telnet negotiation
// here: one message is one line. Everything after that is identical to the
// line transport, because both terminate into the same descriptor.
package wss

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
)

// readLimit bounds a single message.
const readLimit = 8192

// Server serves WebSocket connections over TLS.
type Server struct {
	game *game.Server
	log  *slog.Logger
	http *http.Server
	ln   net.Listener
	// origins lists the Host values a browser may connect from. Empty means
	// same-origin only, which is what the library enforces by default.
	origins []string
}

// Options configure the listener.
type Options struct {
	// Path is the URL the socket is served at.
	Path string
	// Origins lists additional allowed origins for browser clients.
	Origins []string
	Logger  *slog.Logger
}

// New returns a listener bound to addr. tlsConfig must be the same one the
// line transport uses; there is no cleartext WebSocket.
func New(addr string, tlsConfig *tls.Config, g *game.Server, opts Options) (*Server, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	path := opts.Path
	if path == "" {
		path = "/muck"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	s := &Server{game: g, log: log, ln: ln, origins: opts.Origins}
	mux := http.NewServeMux()
	mux.HandleFunc(path, s.handle)
	// A health endpoint that says nothing about the world, so it can be
	// exposed to a container runtime without leaking anything.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	s.http = &http.Server{
		Handler:   mux,
		TLSConfig: tlsConfig,
		// Bound how long a client may dawdle over the handshake. The
		// read timeout must not apply once the socket is upgraded, so
		// it is left off and handled per-message instead.
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          nil,
	}
	return s, nil
}

// Addr reports where the listener is bound.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutCtx)
	}()

	err := s.http.ServeTLS(s.ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the listener.
func (s *Server) Close() error { return s.http.Close() }

// handle upgrades one request and runs the session on it.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.origins,
	})
	if err != nil {
		s.log.Debug("websocket upgrade failed", "error", err)
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(readLimit)

	host := clientHost(r)
	d, err := s.game.Connect(session.TransportWSS, host)
	if err != nil {
		return
	}
	defer s.game.Disconnect(d)

	ctx := r.Context()

	// Writer: drains the descriptor until it closes.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		write := func(text string) bool {
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return conn.Write(wctx, websocket.MessageText, []byte(text)) == nil
		}
		for {
			select {
			case text := <-d.Output():
				if !write(text) {
					return
				}
			case <-d.Done():
				// Deliver whatever is still queued before
				// closing, so a parting message survives.
				for _, text := range d.Drain() {
					if !write(text) {
						break
					}
				}
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			continue
		}
		// A client may still send a trailing newline; one message is one
		// line either way.
		for _, line := range strings.Split(strings.TrimRight(string(data), "\r\n"), "\n") {
			s.game.Input(d, strings.TrimRight(line, "\r"))
		}
		if d.Closed() {
			break
		}
	}

	d.Close()
	<-writeDone
}

// clientHost reports where a request came from, preferring the address the
// connection actually arrived on.
//
// X-Forwarded-For is deliberately not trusted: anyone can set it, and a
// forged value would poison the audit log and any rate limiting built on it.
func clientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
