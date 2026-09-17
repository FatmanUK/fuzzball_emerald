// Command fbemerald is the Fuzzball Emerald MUCK server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/FatmanUK/fuzzball_emerald/internal/config"
	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "fbemerald:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `fbemerald %s - a MUCK server

usage: fbemerald <command> [flags]

commands:
  serve     run the server
  import    load a legacy Fuzzball database dump into Postgres
  migrate   create or update the Postgres schema, then exit
  tune      print the @tune parameter table
  version   print the version

Run "fbemerald <command> -h" for a command's flags.
`, version)
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command given")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(rest)
	case "import":
		return cmdImport(rest)
	case "migrate":
		return cmdMigrate(rest)
	case "tune":
		return cmdTune(rest)
	case "version":
		fmt.Println(version)
		return nil
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// notifyContext returns a context cancelled on SIGINT or SIGTERM. A second
// signal aborts immediately, so an operator is never stuck waiting on a
// shutdown that has wedged.
func notifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// loadConfig reads the environment and applies flag overrides.
func loadConfig(fs *flag.FlagSet, args []string) (config.Config, error) {
	c, err := config.FromEnv()
	if err != nil {
		return c, err
	}
	fs.StringVar(&c.LineAddr, "line-addr", c.LineAddr, "TLS listener address for MUCK clients")
	fs.StringVar(&c.WSSAddr, "wss-addr", c.WSSAddr, "WebSocket-over-TLS listener address")
	fs.StringVar(&c.DatabaseURL, "database-url", c.DatabaseURL, "Postgres connection string")
	fs.StringVar(&c.TLS.CertFile, "tls-cert", c.TLS.CertFile, "path to the TLS certificate")
	fs.StringVar(&c.TLS.KeyFile, "tls-key", c.TLS.KeyFile, "path to the TLS private key")
	fs.DurationVar(&c.FlushInterval, "flush-interval", c.FlushInterval, "how often dirty objects are written to Postgres")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "debug, info, warn or error")
	fs.StringVar(&c.LogFormat, "log-format", c.LogFormat, "text or json")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	return c, nil
}

func newLogger(c config.Config) (*slog.Logger, error) {
	return logging.New(os.Stderr, c.LogLevel, c.LogFormat)
}
