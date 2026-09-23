package config

import (
	"testing"
	"time"
)

func TestFromEnvOverridesDefaults(t *testing.T) {
	t.Setenv("FBE_LINE_ADDR", ":9999")
	t.Setenv("FBE_DATABASE_URL", "postgres://x/y")
	t.Setenv("FBE_FLUSH_INTERVAL", "250ms")
	t.Setenv("FBE_TLS_CIPHER_POLICY", "compat")
	t.Setenv("FBE_TLS_AUTO_RELOAD", "true")

	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.LineAddr != ":9999" {
		t.Errorf("LineAddr = %q", c.LineAddr)
	}
	if c.FlushInterval != 250*time.Millisecond {
		t.Errorf("FlushInterval = %v", c.FlushInterval)
	}
	if c.TLS.Policy != PolicyCompat {
		t.Errorf("Policy = %q", c.TLS.Policy)
	}
	if !c.TLS.AutoReload {
		t.Error("AutoReload should be true")
	}
	// Untouched settings keep their defaults.
	if c.WSSPath != "/muck" {
		t.Errorf("WSSPath = %q, want /muck", c.WSSPath)
	}
}

func TestFromEnvRejectsBadValues(t *testing.T) {
	t.Setenv("FBE_TLS_CIPHER_POLICY", "sslv3")
	if _, err := FromEnv(); err == nil {
		t.Error("an unknown cipher policy should be rejected")
	}
}

func TestValidate(t *testing.T) {
	good := Default()
	good.DatabaseURL = "postgres://x/y"
	good.TLS.CertFile = "cert.pem"
	good.TLS.KeyFile = "key.pem"
	if err := good.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cases := map[string]func(*Config){
		"missing database": func(c *Config) { c.DatabaseURL = "" },
		"missing cert":     func(c *Config) { c.TLS.CertFile = "" },
		"missing key":      func(c *Config) { c.TLS.KeyFile = "" },
		"no listeners":     func(c *Config) { c.LineAddr = ""; c.WSSAddr = "" },
		"zero flush":       func(c *Config) { c.FlushInterval = 0 },
	}
	for name, breakIt := range cases {
		c := good
		breakIt(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: Validate() should have failed", name)
		}
	}
}

func TestPprofAddrMustBeLoopback(t *testing.T) {
	base := Default()
	base.DatabaseURL = "postgres://x"
	base.TLS.CertFile = "cert.pem"
	base.TLS.KeyFile = "key.pem"

	for _, addr := range []string{"127.0.0.1:6060", "localhost:6060", "[::1]:6060"} {
		c := base
		c.PprofAddr = addr
		if err := c.Validate(); err != nil {
			t.Errorf("Validate() rejected the loopback address %q: %v", addr, err)
		}
	}

	// A bare port binds every interface, which is the case that matters:
	// the pprof handlers hand out goroutine stacks and heap dumps.
	for _, addr := range []string{":6060", "0.0.0.0:6060", "192.168.1.5:6060", "nonsense"} {
		c := base
		c.PprofAddr = addr
		if err := c.Validate(); err == nil {
			t.Errorf("Validate() accepted the non-loopback address %q", addr)
		}
	}
}

func TestLimitsFromEnv(t *testing.T) {
	t.Setenv("FBE_MAX_CONNECTIONS", "10")
	t.Setenv("FBE_MAX_PER_HOST", "2")
	t.Setenv("FBE_CONNECT_RATE", "5")
	t.Setenv("FBE_CONNECT_WINDOW", "30s")

	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := Limits{MaxConnections: 10, MaxPerHost: 2, ConnectRate: 5, ConnectWindow: 30 * time.Second}
	if c.Limits != want {
		t.Errorf("Limits = %+v, want %+v", c.Limits, want)
	}
}

func TestNegativeLimitIsRejected(t *testing.T) {
	t.Setenv("FBE_MAX_PER_HOST", "-1")
	if _, err := FromEnv(); err == nil {
		t.Error("a negative limit should be an error")
	}
}
