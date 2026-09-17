package tune

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func TestTableIsPopulated(t *testing.T) {
	// 169 upstream parameters, less the 8 TLS/STARTTLS ones the plan drops.
	const want = 161
	if len(params) != want {
		t.Errorf("params has %d entries, want %d", len(params), want)
	}
	for i := range params {
		p := &params[i]
		if p.Name == "" || p.Label == "" || p.Group == "" {
			t.Errorf("params[%d] (%q) has an empty name, label or group", i, p.Name)
		}
		if strings.Contains(strings.ToLower(p.Name), "ssl") {
			t.Errorf("parameter %q still says SSL", p.Name)
		}
	}
}

func TestRenamedParamResolvesByLegacyName(t *testing.T) {
	p, ok := Lookup("smtp_ssl_type")
	if !ok {
		t.Fatal("legacy name smtp_ssl_type should still resolve")
	}
	if p.Name != "smtp_tls_mode" {
		t.Errorf("smtp_ssl_type resolved to %q, want smtp_tls_mode", p.Name)
	}
	if _, ok := Lookup("smtp_tls_mode"); !ok {
		t.Error("smtp_tls_mode should resolve by its current name")
	}
}

func TestDroppedParamsAreExplained(t *testing.T) {
	for _, name := range []string{"ssl_cert_file", "ssl_key_file", "starttls_allow"} {
		if _, ok := Lookup(name); ok {
			t.Errorf("%q should not be a live parameter", name)
		}
		if _, ok := DroppedReplacement(name); !ok {
			t.Errorf("%q should be listed as dropped, with a replacement", name)
		}
	}
}

func TestDefaultsMatchSpotChecks(t *testing.T) {
	s := NewSet()
	if got := s.Ref("default_room_parent"); got != ref.GlobalEnvironment {
		t.Errorf("default_room_parent = %v, want #0", got)
	}
	if got := s.Duration("dump_interval"); got != 4*time.Hour {
		t.Errorf("dump_interval = %v, want 4h", got)
	}
	if got := s.String("penny"); got == "" {
		t.Error("penny should have a non-empty default")
	}
	if !s.IsDefault("penny") {
		t.Error("a fresh Set should report every parameter as default")
	}
}

func TestSetAndReset(t *testing.T) {
	s := NewSet()
	if err := s.SetString("penny", "Groat"); err != nil {
		t.Fatal(err)
	}
	if got := s.String("penny"); got != "Groat" {
		t.Errorf("penny = %q, want Groat", got)
	}
	if s.IsDefault("penny") {
		t.Error("penny should no longer be default")
	}
	if err := s.Reset("penny"); err != nil {
		t.Fatal(err)
	}
	if !s.IsDefault("penny") {
		t.Error("penny should be default again after Reset")
	}
}

func TestBooleanSpellings(t *testing.T) {
	p, _ := Lookup("wiz_vehicles")
	for _, in := range []string{"yes", "Y", "true", "1", "on"} {
		v, err := p.Parse(in)
		if err != nil || !v.Bool {
			t.Errorf("Parse(%q) = %v, %v; want true", in, v.Bool, err)
		}
	}
	for _, in := range []string{"no", "N", "false", "0", "off"} {
		v, err := p.Parse(in)
		if err != nil || v.Bool {
			t.Errorf("Parse(%q) = %v, %v; want false", in, v.Bool, err)
		}
	}
	if _, err := p.Parse("maybe"); err == nil {
		t.Error("Parse(\"maybe\") should fail")
	}
}

func TestTimespanRoundTrip(t *testing.T) {
	cases := []struct {
		text string
		want time.Duration
	}{
		{" 90d  0:00:00", 90 * 24 * time.Hour},
		{"  0d  0:15:00", 15 * time.Minute},
		{"  0d  4:00:00", 4 * time.Hour},
		{"  0d  0:00:00", 0},
		{"  1d 02:03:04", 24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second},
		// Shorter forms right-align: "5:00" is minutes and seconds.
		{"5:00", 5 * time.Minute},
		{"30", 30 * time.Second},
	}
	for _, c := range cases {
		got, err := ParseTimespan(c.text)
		if err != nil {
			t.Errorf("ParseTimespan(%q): %v", c.text, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTimespan(%q) = %v, want %v", c.text, got, c.want)
		}
	}
	// Canonical forms must survive a format/parse round trip.
	for _, c := range cases[:4] {
		if got := FormatTimespan(c.want); got != c.text {
			t.Errorf("FormatTimespan(%v) = %q, want %q", c.want, got, c.text)
		}
	}
	if _, err := ParseTimespan("later"); err == nil {
		t.Error("ParseTimespan(\"later\") should fail")
	}
}

func TestNonNullableStringRejectsEmpty(t *testing.T) {
	var nullable, plain *Param
	for i := range params {
		if params[i].Type != TypeString {
			continue
		}
		if params[i].Nullable && nullable == nil {
			nullable = &params[i]
		}
		if !params[i].Nullable && plain == nil {
			plain = &params[i]
		}
	}
	if nullable == nil || plain == nil {
		t.Fatal("expected both nullable and non-nullable string parameters")
	}
	if _, err := nullable.Parse(""); err != nil {
		t.Errorf("%s is nullable but rejected an empty value: %v", nullable.Name, err)
	}
	if _, err := plain.Parse(""); err == nil {
		t.Errorf("%s is not nullable but accepted an empty value", plain.Name)
	}
}

func TestTypedAccessorPanicsOnWrongType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("reading a boolean parameter as a string should panic")
		}
	}()
	NewSet().String("wiz_vehicles")
}

// TestAgainstRealDumpHeader parses the parameter block of the shipped minimal
// database. It is the real compatibility check: every name must resolve and
// every value must parse.
func TestAgainstRealDumpHeader(t *testing.T) {
	f, err := os.Open("../../testdata/minimal.db")
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines[0] != "***Foxen9 TinyMUCK DUMP Format***" {
		t.Fatalf("unexpected dump header %q", lines[0])
	}

	nparams, err := strconv.Atoi(strings.TrimSpace(lines[3]))
	if err != nil {
		t.Fatalf("parameter count: %v", err)
	}
	if nparams != 169 {
		t.Fatalf("fixture declares %d parameters, expected 169", nparams)
	}

	s := NewSet()
	var seen, dropped int
	for _, line := range lines[4 : 4+nparams] {
		// A leading '%' means the value is still the server default.
		isDefault := strings.HasPrefix(line, "%")
		line = strings.TrimPrefix(line, "%")
		name, raw, ok := strings.Cut(line, "=")
		if !ok {
			t.Errorf("malformed parameter line %q", line)
			continue
		}
		if _, gone := DroppedReplacement(name); gone {
			dropped++
			continue
		}
		p, found := Lookup(name)
		if !found {
			t.Errorf("dump parameter %q is not in the table", name)
			continue
		}
		seen++
		if err := s.SetString(name, raw); err != nil {
			t.Errorf("%s=%q: %v", name, raw, err)
			continue
		}
		// Where the dump says a value is the default, ours must agree.
		if isDefault {
			if got := p.Format(s.vals[p.Name]); got != p.Format(p.Default) {
				t.Errorf("%s: dump default %q, our default %q",
					name, got, p.Format(p.Default))
			}
		}
	}
	if seen != 161 {
		t.Errorf("matched %d live parameters, want 161", seen)
	}
	if dropped != 8 {
		t.Errorf("skipped %d dropped parameters, want 8", dropped)
	}
	// minimal.db sets two parameters explicitly, without the '%' default
	// marker: default_room_parent and player_start.
	if s.IsDefault("default_room_parent") {
		t.Error("default_room_parent is set explicitly in the fixture")
	}
}
