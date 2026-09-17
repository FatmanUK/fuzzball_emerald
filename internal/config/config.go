// Package config holds the settings a server needs before it can open its
// database: listener addresses, TLS material and the Postgres DSN.
//
// These deliberately do not live in the @tune table. A TLS-only server cannot
// bootstrap its listeners from a database it has not opened yet, which is why
// Fuzzball 7's ssl_* parameters have no equivalent here.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CipherPolicy selects a TLS cipher suite and version floor by name, replacing
// Fuzzball 7's raw OpenSSL cipher string, which Go's crypto/tls cannot consume.
type CipherPolicy string

const (
	// PolicyModern requires TLS 1.3.
	PolicyModern CipherPolicy = "modern"
	// PolicyCompat allows TLS 1.2 with AEAD suites, for older clients.
	PolicyCompat CipherPolicy = "compat"
)

// TLS describes the server's TLS material.
type TLS struct {
	CertFile    string
	KeyFile     string
	KeyPassword string
	Policy      CipherPolicy
	// AutoReload watches the certificate files and swaps them in without a
	// restart, replacing @reconfigure_ssl.
	AutoReload bool
}

// Config is the full pre-database configuration.
type Config struct {
	// LineAddr is the raw TLS listener, which existing MUCK clients speak.
	LineAddr string
	// WSSAddr is the WebSocket-over-TLS listener.
	WSSAddr string
	// WSSPath is the URL path the WebSocket listener serves.
	WSSPath string

	TLS TLS

	// DatabaseURL is the Postgres DSN.
	DatabaseURL string

	// FlushInterval bounds how much work a crash can lose. It replaces
	// Fuzzball 7's dump_interval, which froze the world for the length of
	// a full database write.
	FlushInterval time.Duration

	LogLevel  string
	LogFormat string // "text" or "json"
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
	}
}

// FromEnv layers environment variables over the defaults. Every variable is
// prefixed FBE_.
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
	if v, ok := os.LookupEnv("FBE_FLUSH_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("FBE_FLUSH_INTERVAL: %w", err)
		}
		c.FlushInterval = d
	}
	return c, nil
}

// Validate reports whether the configuration can start a server. It is
// deliberately strict about TLS: there is no cleartext fallback to degrade to.
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
	return nil
}
