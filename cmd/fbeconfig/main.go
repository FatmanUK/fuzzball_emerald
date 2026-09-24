// Command fbeconfig is Fuzzball Emerald's optional web configurator.
//
// It is a second binary against the same Postgres database and the
// same FBE_* variables as the server, so a deployment that wants one
// adds a container and changes nothing else. It is read-only while a
// server holds the world's lease, and read-write when none does.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/config"
	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
	"github.com/FatmanUK/fuzzball_emerald/internal/web"
)

// version is stamped at build time with -ldflags "-X
// main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "fbeconfig:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("fbeconfig", flag.ContinueOnError)
	c, err := config.FromEnv()
	if err != nil {
		return err
	}
	fs.StringVar(&c.Web.Addr, "addr", c.Web.Addr,
		"address to serve on")
	fs.StringVar(&c.DatabaseURL, "database-url", c.DatabaseURL,
		"Postgres connection string")
	fs.StringVar(&c.Web.CertFile, "tls-cert", c.Web.CertFile,
		"path to the TLS certificate")
	fs.StringVar(&c.Web.KeyFile, "tls-key", c.Web.KeyFile,
		"path to the TLS private key")
	fs.DurationVar(&c.Web.SessionTTL, "session-ttl",
		c.Web.SessionTTL, "how long a login lasts")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel,
		"debug, info, warn or error")
	fs.StringVar(&c.LogFormat, "log-format", c.LogFormat,
		"text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := c.ValidateWeb(); err != nil {
		return err
	}

	base, err := logging.New(os.Stderr, c.LogLevel, c.LogFormat)
	if err != nil {
		return err
	}
	log := logging.On(base, logging.Status)

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	// The schema is not migrated from here. A configurator that
	// created tables could bring a database up to a schema the
	// running server does not know, and it has no business being
	// the thing that decides what the world looks like.

	srv, err := web.New(web.Options{
		Store:      st,
		Logger:     base,
		SessionTTL: c.Web.SessionTTL,
		Version:    version,
	})
	if err != nil {
		return err
	}

	tlsCfg, err := c.WebTLS(base)
	if err != nil {
		return err
	}

	held, err := st.LeaseHeld(ctx)
	if err != nil {
		log.Warn("could not read the world's lease", "error", err)
	}
	log.Info("starting", "version", version, "addr", c.Web.Addr,
		"server_running", held, "session_ttl",
		c.Web.SessionTTL.String())
	if !isLoopback(c.Web.Addr) {
		log.Warn("the configurator is not bound to loopback; "+
			"anyone who can reach it and knows a wizard's "+
			"password has the world",
			"addr", c.Web.Addr)
	}

	return serve(ctx, c.Web.Addr, tlsCfg, srv.Handler(), log)
}

// serve runs the listener until the context is cancelled, then closes
// it gracefully.
func serve(ctx context.Context, addr string, tlsCfg *tls.Config,
	h http.Handler, log *slog.Logger) error {

	hs := &http.Server{
		Addr:      addr,
		Handler:   h,
		TLSConfig: tlsCfg,
		// Generous enough for a slow database and short
		// enough that a stuck client does not hold a
		// connection for ever.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog: slog.NewLogLogger(
			log.Handler(), slog.LevelWarn),
	}

	done := make(chan error, 1)
	go func() {
		// The certificate comes from TLSConfig, so the paths
		// here are empty.
		done <- hs.ListenAndServeTLS("", "")
	}()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second)
		defer cancel()
		log.Info("shutting down")
		return hs.Shutdown(shutCtx)
	}
}

// isLoopback reports whether an address binds only to this machine.
// Unlike the pprof listener, which is refused outright, this is a
// warning: a configurator behind a reverse proxy or on a private
// network is a reasonable thing to want.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	return host == "localhost" || host == "127.0.0.1" ||
		host == "::1"
}
