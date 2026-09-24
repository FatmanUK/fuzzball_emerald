// Package config holds the settings a server needs before it can open
// its database: listener addresses, TLS material and the Postgres
// DSN.
//
// These deliberately do not live in the @tune table. A TLS-only
// server cannot bootstrap its listeners from a database it has not
// opened yet, which is why Fuzzball 7's ssl_* parameters have no
// equivalent here.
package config

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// CipherPolicy selects a TLS cipher suite and version floor by name,
// replacing Fuzzball 7's raw OpenSSL cipher string, which Go's
// crypto/tls cannot consume.
type CipherPolicy string

const (
	// PolicyModern requires TLS 1.3.
	PolicyModern CipherPolicy = "modern"
	// PolicyCompat allows TLS 1.2 with AEAD suites, for older
	// clients.
	PolicyCompat CipherPolicy = "compat"
)

// TLS describes the server's TLS material.
type TLS struct {
	CertFile    string
	KeyFile     string
	KeyPassword string
	Policy      CipherPolicy
	// AutoReload watches the certificate files and swaps them in
	// without a restart, replacing @reconfigure_ssl.
	AutoReload bool
}

// Config is the full pre-database configuration.
type Config struct {
	// LineAddr is the raw TLS listener, which existing MUCK
	// clients speak.
	LineAddr string
	// WSSAddr is the WebSocket-over-TLS listener.
	WSSAddr string
	// WSSPath is the URL path the WebSocket listener serves.
	WSSPath string

	TLS TLS

	// DatabaseURL is the Postgres DSN.
	DatabaseURL string

	// FlushInterval bounds how much work a crash can lose. It
	// replaces Fuzzball 7's dump_interval, which froze the world
	// for the length of a full database write.
	FlushInterval time.Duration

	LogLevel  string
	LogFormat string // "text" or "json"

	// Limits bound what one peer may open. They are refused at
	// accept time, before the TLS handshake and before the world
	// hears about the connection — see internal/admit for why
	// they are not @tune parameters.
	Limits Limits

	// Web configures the optional configurator, which is a
	// separate binary against the same database and the same
	// FBE_* variables.
	Web Web

	// PprofAddr, when set, serves net/http/pprof there. It is
	// refused unless it binds to a loopback address: the handlers
	// expose goroutine stacks and heap contents, which is a
	// debugging aid on a host an operator already has and a
	// disclosure to anyone else.
	PprofAddr string
}

// Web configures the configurator. It is separate from the MUCK
// listener's settings because the two are validated apart: a
// configurator needs no MUCK listener, and a server needs no web
// listener.
type Web struct {
	// Addr is where the configurator listens. It defaults to
	// loopback, and binding it anywhere else is a deliberate act:
	// this interface can rewrite passwords and ownership.
	Addr string

	// CertFile and KeyFile default to the MUCK listener's pair,
	// so a deployment that already has a certificate needs no
	// second one.
	CertFile string
	KeyFile  string

	// SessionTTL is how long a login lasts.
	SessionTTL time.Duration
}

// Limits bound incoming connections. A zero field means that limit is
// off.
type Limits struct {
	// MaxConnections is the ceiling on concurrent connections in
	// total.
	MaxConnections int
	// MaxPerHost is the ceiling on concurrent connections from
	// one address.
	MaxPerHost int
	// ConnectRate is how many new connections one address may
	// open per ConnectWindow.
	ConnectRate   int
	ConnectWindow time.Duration
}

// Default returns the configuration used when nothing is set.
func Default() Config {
	return Config{
		LineAddr:      ":4202",
		WSSAddr:       ":4203",
		WSSPath:       "/muck",
		TLS:           TLS{Policy: PolicyModern},
		FlushInterval: time.Second,
		LogLevel:      "info",
		LogFormat:     "text",
		Web: Web{
			Addr:       "127.0.0.1:4204",
			SessionTTL: 2 * time.Hour,
		},
		// Chosen to be generous for a real player — several
		// clients and a reconnect or two — and stingy for a
		// script. A shared address behind NAT is the case
		// that needs raising, which is why these are
		// configurable rather than fixed.
		Limits: Limits{
			MaxConnections: 1024,
			MaxPerHost:     16,
			ConnectRate:    30,
			ConnectWindow:  time.Minute,
		},
	}
}

// FromEnv layers environment variables over the defaults. Every
// variable is prefixed FBE_.
func FromEnv() (Config, error) {
	c := Default()

	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	str("FBE_LINE_ADDR", &c.LineAddr)
	str("FBE_WSS_ADDR", &c.WSSAddr)
	str("FBE_WSS_PATH", &c.WSSPath)
	str("FBE_DATABASE_URL", &c.DatabaseURL)
	str("FBE_TLS_CERT_FILE", &c.TLS.CertFile)
	str("FBE_TLS_KEY_FILE", &c.TLS.KeyFile)
	str("FBE_TLS_KEY_PASSWORD", &c.TLS.KeyPassword)
	str("FBE_LOG_LEVEL", &c.LogLevel)
	str("FBE_LOG_FORMAT", &c.LogFormat)
	str("FBE_PPROF_ADDR", &c.PprofAddr)
	str("FBE_WEB_ADDR", &c.Web.Addr)
	str("FBE_WEB_TLS_CERT_FILE", &c.Web.CertFile)
	str("FBE_WEB_TLS_KEY_FILE", &c.Web.KeyFile)

	num := func(key string, dst *int) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if n < 0 {
			return fmt.Errorf("%s: must not be negative, got %d", key, n)
		}
		*dst = n
		return nil
	}
	for key, dst := range map[string]*int{
		"FBE_MAX_CONNECTIONS": &c.Limits.MaxConnections,
		"FBE_MAX_PER_HOST":    &c.Limits.MaxPerHost,
		"FBE_CONNECT_RATE":    &c.Limits.ConnectRate,
	} {
		if err := num(key, dst); err != nil {
			return c, err
		}
	}
	if v, ok := os.LookupEnv("FBE_CONNECT_WINDOW"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("FBE_CONNECT_WINDOW: %w", err)
		}
		c.Limits.ConnectWindow = d
	}

	if v, ok := os.LookupEnv("FBE_TLS_CIPHER_POLICY"); ok {
		switch CipherPolicy(strings.ToLower(v)) {
		case PolicyModern:
			c.TLS.Policy = PolicyModern
		case PolicyCompat:
			c.TLS.Policy = PolicyCompat
		default:
			return c, fmt.Errorf("FBE_TLS_CIPHER_POLICY: %q is not %q or %q",
				v, PolicyModern, PolicyCompat)
		}
	}
	if v, ok := os.LookupEnv("FBE_TLS_AUTO_RELOAD"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("FBE_TLS_AUTO_RELOAD: %w", err)
		}
		c.TLS.AutoReload = b
	}
	if v, ok := os.LookupEnv("FBE_WEB_SESSION_TTL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf(
				"FBE_WEB_SESSION_TTL: %w", err)
		}
		c.Web.SessionTTL = d
	}

	if v, ok := os.LookupEnv("FBE_FLUSH_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("FBE_FLUSH_INTERVAL: %w", err)
		}
		c.FlushInterval = d
	}
	return c, nil
}

// Validate reports whether the configuration can start a server. It
// is deliberately strict about TLS: there is no cleartext fallback to
// degrade to.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("no database URL (set FBE_DATABASE_URL)")
	}
	if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
		return fmt.Errorf("TLS certificate and key are required (set FBE_TLS_CERT_FILE and FBE_TLS_KEY_FILE)")
	}
	if c.LineAddr == "" && c.WSSAddr == "" {
		return fmt.Errorf("no listeners configured")
	}
	if c.FlushInterval <= 0 {
		return fmt.Errorf("flush interval must be positive, got %v", c.FlushInterval)
	}
	if c.PprofAddr != "" && !isLoopback(c.PprofAddr) {
		return fmt.Errorf("FBE_PPROF_ADDR must bind to localhost, got %q", c.PprofAddr)
	}
	return nil
}

// ValidateWeb reports whether the configuration can start the
// configurator.
//
// It is separate from Validate rather than an extension of it: that
// one hard-requires the MUCK listener's TLS material and at least one
// MUCK listener, and the configurator has neither. What they share is
// the database URL.
//
// The TLS material falls back to the MUCK listener's, so a deployment
// that already has a certificate needs no second one, and is required
// either way — this interface carries a wizard's password and can
// rewrite anyone's.
func (c *Config) ValidateWeb() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf(
			"no database URL (set FBE_DATABASE_URL)")
	}
	if c.Web.CertFile == "" {
		c.Web.CertFile = c.TLS.CertFile
	}
	if c.Web.KeyFile == "" {
		c.Web.KeyFile = c.TLS.KeyFile
	}
	if c.Web.CertFile == "" || c.Web.KeyFile == "" {
		return fmt.Errorf("the configurator needs a TLS " +
			"certificate and key (set " +
			"FBE_WEB_TLS_CERT_FILE and " +
			"FBE_WEB_TLS_KEY_FILE)")
	}
	if c.Web.Addr == "" {
		return fmt.Errorf("no web address (set FBE_WEB_ADDR)")
	}
	if c.Web.SessionTTL <= 0 {
		return fmt.Errorf("the session lifetime must be "+
			"positive, got %v", c.Web.SessionTTL)
	}
	return nil
}

// WebTLS builds the configurator's TLS configuration from its own
// certificate pair, reusing the cipher policy and the loader the MUCK
// listener uses.
func (c Config) WebTLS(log *slog.Logger) (*tls.Config, error) {
	sub := c
	sub.TLS.CertFile = c.Web.CertFile
	sub.TLS.KeyFile = c.Web.KeyFile
	return sub.BuildTLS(log)
}

// isLoopback reports whether an address binds only to the local
// machine.
//
// A bare port or an empty host binds every interface, which is
// exactly what this must refuse: the pprof handlers hand out
// goroutine stacks and heap dumps to whoever asks.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
