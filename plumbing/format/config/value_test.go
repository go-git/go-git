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
		{"", true, false}, // bare key shorthand
		{"1", true, false},
		{"0", false, false},
		{"-1", true, false},
		{"42", true, false},
		{"0x10", true, false},
		{"maybe", false, true},
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
		{"010", 8, false},
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

func TestParseUint(t *testing.T) {
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
		got, err := ParseUint(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseUint(%q) err = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseUint(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestSplitKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key        string
		section    string
		subsection string
		name       string
	}{
		{"core.bare", "core", "", "bare"},
		{"remote.origin.url", "remote", "origin", "url"},
		{"url.git@github.com:.insteadOf", "url", "git@github.com:", "insteadOf"},
		{"core", "core", "", ""},
		{"a.b.c.d", "a", "b.c", "d"},
	}
	for _, tt := range tests {
		section, subsection, name := SplitKey(tt.key)
		if section != tt.section || subsection != tt.subsection || name != tt.name {
			t.Errorf("SplitKey(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tt.key, section, subsection, name, tt.section, tt.subsection, tt.name)
		}
	}
}

func TestConfigAccessors(t *testing.T) {
	t.Parallel()
	c := New()
	c.Set("core.bare", "true")
	c.Set("remote.origin.url", "git@github.com:go-git/go-git.git")
	c.Add("remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	c.Add("remote.origin.fetch", "+refs/tags/*:refs/tags/*")
	c.Set("pack.window", "16")

	if got := c.Get("remote.origin.url"); got != "git@github.com:go-git/go-git.git" {
		t.Errorf("Get url = %q", got)
	}
	if !c.Bool("core.bare", false) {
		t.Error("core.bare should be true")
	}
	if c.Bool("core.missing", true) != true {
		t.Error("missing key should return default")
	}
	if all := c.GetAll("remote.origin.fetch"); len(all) != 2 {
		t.Errorf("GetAll fetch len = %d, want 2", len(all))
	}
	if n, err := c.Int("pack.window", 10); err != nil || n != 16 {
		t.Errorf("Int pack.window = %d, %v", n, err)
	}
	if n, err := c.Int("pack.depth", 50); err != nil || n != 50 {
		t.Errorf("Int default = %d, %v", n, err)
	}

	c.Set("remote.origin.fetch", "single")
	if all := c.GetAll("remote.origin.fetch"); len(all) != 1 {
		t.Errorf("Set should collapse multivalue, got %d", len(all))
	}

	c.Unset("core.bare")
	if c.Has("core.bare") {
		t.Error("core.bare should be unset")
	}
}

func FuzzParseValue(f *testing.F) {
	seeds := []string{"", "true", "no", "0x1f", "1k", "-3g", "42", "abc", "  ", "9999999999999999999"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// The parsers must never panic on arbitrary input.
		_, _ = ParseBool(s)
		_, _ = ParseInt(s)
		_, _ = ParseInt64(s)
		_, _ = ParseUint(s)

		if v, err := ParseInt64(s); err == nil {
			// A successful integer parse must be a valid boolean too.
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
