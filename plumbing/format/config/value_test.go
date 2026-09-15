package config

import (
	"math"
	"testing"
)

func TestParseBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value   string
		want    bool
		wantErr bool
	}{
		{"true", true, false},
		{"TRUE", true, false},
		{"yes", true, false},
		{"On", true, false},
		{"false", false, false},
		{"No", false, false},
		{"off", false, false},
		{"", true, false}, // bare-key shorthand
		{"1", true, false},
		{"0", false, false},
		{"-1", true, false},
		{"42", true, false},
		{"0x10", true, false},
		{"0x0", false, false},
		{"010", true, false},
		{"maybe", false, true},
		{"t", false, true},
		{"f", false, true},
		{"truthy", false, true},
	}
	for _, tt := range tests {
		got, err := ParseBool(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseBool(%q) err = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseBool(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestParseInt64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value   string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"10", 10, false},
		{"-5", -5, false},
		{"0x10", 16, false},
		{"010", 8, false}, // base 0: octal
		{"1k", 1024, false},
		{"1K", 1024, false},
		{"2m", 2 * 1024 * 1024, false},
		{"3g", 3 * 1024 * 1024 * 1024, false},
		{"-1k", -1024, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1t", 0, true},
		{"12x", 0, true},
		{"9223372036854775807", math.MaxInt64, false},
		{"9999999999999999999g", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseInt64(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseInt64(%q) err = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseInt64(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestParseUint64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value   string
		want    uint64
		wantErr bool
	}{
		{"0", 0, false},
		{"10", 10, false},
		{"1k", 1024, false},
		{"-1", 0, true},
		{"-1k", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseUint64(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseUint64(%q) err = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseUint64(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func FuzzParseValue(f *testing.F) {
	for _, s := range []string{"", "true", "no", "0x1f", "1k", "-3g", "42", "abc", "  ", "9999999999999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// The parsers must never panic on arbitrary input.
		_, _ = ParseBool(s)
		_, _ = ParseInt(s)
		_, _ = ParseInt64(s)
		_, _ = ParseUint(s)
		_, _ = ParseUint64(s)

		if v, err := ParseInt64(s); err == nil {
			// A value that parses as an integer must parse as a bool too,
			// and agree on truthiness.
			b, berr := ParseBool(s)
			if berr != nil {
				t.Fatalf("ParseInt64(%q)=%d ok but ParseBool errored: %v", s, v, berr)
			}
			if b != (v != 0) {
				t.Fatalf("ParseBool(%q)=%v inconsistent with int %d", s, b, v)
			}
		}
	})
}
