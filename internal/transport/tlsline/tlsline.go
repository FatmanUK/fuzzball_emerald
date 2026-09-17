// Package tlsline serves the raw TLS line protocol that existing MUCK clients
// speak.
//
// There is no cleartext listener and no STARTTLS: a connection is encrypted
// from its first byte or it does not exist.
package tlsline

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
)

// readLimit bounds a single line of input, so a client cannot make the server
// buffer without limit by never sending a newline.
const readLimit = 8192

// handshakeTimeout bounds how long a connection may take to complete TLS,
// which stops an idle opener from holding a slot indefinitely.
const handshakeTimeout = 20 * time.Second

// Server listens for TLS connections.
type Server struct {
	game *game.Server
	log  *slog.Logger
	ln   net.Listener
}

// New returns a listener bound to addr.
func New(addr string, cfg *tls.Config, g *game.Server, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	return &Server{game: g, log: log, ln: ln}, nil
}

// Addr reports where the listener is bound, which a test needs when it asked
// for port 0.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.ln.Close()
	}()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // a clean shutdown
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(ctx, conn)
	}
}

// Close stops the listener.
func (s *Server) Close() error { return s.ln.Close() }

// handle runs one connection.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	host := hostOf(conn.RemoteAddr())

	// Complete the handshake before doing anything else, so a connection
	// that never negotiates cannot occupy a descriptor.
	if tc, ok := conn.(*tls.Conn); ok {
		if err := tc.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
			return
		}
		if err := tc.HandshakeContext(ctx); err != nil {
			s.log.Debug("tls handshake failed", "host", host, "error", err)
			return
		}
		if err := tc.SetDeadline(time.Time{}); err != nil {
			return
		}
	}

	d, err := s.game.Connect(session.TransportLine, host)
	if err != nil {
		return
	}
	defer s.game.Disconnect(d)

	// The decoder strips telnet control sequences and reports window sizes.
	dec := session.NewDecoder(func(ws session.WindowSize) {
		s.game.Resize(d, ws)
	})

	// Writer: drains the descriptor's output until it closes.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		bw := bufio.NewWriter(conn)
		write := func(text string) bool {
			if _, err := bw.Write(session.EncodeLine(text)); err != nil {
				return false
			}
			// Flush per line: a MUCK is interactive, and holding
			// output back to fill a buffer would be felt.
			return bw.Flush() == nil
		}
		for {
			select {
			case text := <-d.Output():
				if !write(text) {
					conn.Close()
					return
				}
			case <-d.Done():
				// Deliver whatever is still queued, so a
				// parting message is not lost to the race
				// between it and the disconnect.
				for _, text := range d.Drain() {
					if !write(text) {
						break
					}
				}
				conn.Close()
				return
			}
		}
	}()

	if _, err := conn.Write(session.InitialNegotiation()); err != nil {
		return
	}

	s.readLoop(conn, d, dec)

	d.Close()
	<-writeDone
}

// readLoop turns the byte stream into lines of input.
func (s *Server) readLoop(conn net.Conn, d *session.Descriptor, dec *session.Decoder) {
	buf := make([]byte, 4096)
	var line []byte

	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := dec.Decode(buf[:n])
			if reply := dec.TakeReply(); len(reply) > 0 {
				if _, werr := conn.Write(reply); werr != nil {
					return
				}
			}
			for _, c := range data {
				switch c {
				case '\n':
					s.game.Input(d, strings.TrimRight(string(line), "\r"))
					line = line[:0]
				default:
					if len(line) < readLimit {
						line = append(line, c)
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !d.Closed() {
				s.log.Debug("read error", "descriptor", d.ID, "error", err)
			}
			return
		}
		if d.Closed() {
			return
		}
	}
}

// hostOf renders a remote address for logging and WHO, without the port.
func hostOf(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
