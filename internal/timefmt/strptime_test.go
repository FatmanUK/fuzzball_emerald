package timefmt

import "testing"

func TestSeconds(t *testing.T) {
	tests := []struct {
		value, format string
		want          int64
		ok            bool
	}{
		// CONVTIME's own fixed format, with the year both ways round: the
		// four-digit case is the one upstream rewrites the format for.
		{"12:00:00 01/01/2000", "%T%t%D", 946728000, true},
		{"12:00:00 01/01/00", "%T%t%D", 946728000, true},
		{"00:00:00 06/15/1998", "%T%t%D", 897868800, true},

		// An arbitrary format, which is all FMTTIME adds.
		{"2000-01-01", "%F", 946684800, true},
		{"2000-01-01 12:00", "%F %R", 946728000, true},
		{"Jan 01 2000", "%b %d %Y", 946684800, true},
		{"January 01 2000", "%b %d %Y", 946684800, true},
		{"01:00:00 PM 01/01/2000", "%I:%M:%S %p %m/%d/%Y", 946731600, true},
		{"12:00:00 AM 01/01/2000", "%I:%M:%S %p %m/%d/%Y", 946684800, true},

		{"nonsense", "%T%t%D", 0, false},
		{"12:00:00", "%T%t%D", 0, false},
		{"2000-01-01", "%T", 0, false},
	}
	for _, tt := range tests {
		got, ok := Seconds(tt.value, tt.format)
		if ok != tt.ok {
			t.Errorf("Seconds(%q, %q) ok = %v, want %v",
				tt.value, tt.format, ok, tt.ok)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("Seconds(%q, %q) = %d, want %d",
				tt.value, tt.format, got, tt.want)
		}
	}
}
