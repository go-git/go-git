package config

import (
	"testing"
	"time"
)

func TestGitSizeUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"1k", 1024},
		{"1K", 1024},
		{"512k", 512 * 1024},
		{"1m", 1024 * 1024},
		{"2M", 2 * 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"4G", 4 * 1024 * 1024 * 1024},
		{"256m", 256 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			var s GitSize
			if err := s.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Int64() != tt.want {
				t.Errorf("NewGitSize(%q).Int64() = %d, want %d", tt.input, s.Int64(), tt.want)
			}
		})
	}
}

func TestGitSizeUnmarshalPlainInt(t *testing.T) {
	t.Parallel()
	var s GitSize
	if err := s.UnmarshalGitConfig([]byte("4096")); err != nil {
		t.Fatal(err)
	}
	if s.Int64() != 4096 {
		t.Errorf("got %d, want 4096", s.Int64())
	}
}

func TestGitSizeUnmarshalInvalidSuffix(t *testing.T) {
	t.Parallel()
	var s GitSize
	err := s.UnmarshalGitConfig([]byte("1x"))
	if err == nil {
		t.Error("expected error for unknown suffix")
	}
}

func TestGitSizeUnmarshalEmpty(t *testing.T) {
	t.Parallel()
	var s GitSize
	if err := s.UnmarshalGitConfig([]byte("")); err != nil {
		t.Fatal(err)
	}
	if s.Int64() != 0 {
		t.Errorf("got %d, want 0", s.Int64())
	}
}

func TestGitSizeMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []string{"1k", "512m", "2G", "100"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var s GitSize
			if err := s.UnmarshalGitConfig([]byte(input)); err != nil {
				t.Fatal(err)
			}
			got, err := s.MarshalGitConfig()
			if err != nil {
				t.Fatal(err)
			}
			if got != input {
				t.Errorf("round-trip: got %q, want %q", got, input)
			}
		})
	}
}

func TestGitSizeMarshalFromInt(t *testing.T) {
	t.Parallel()
	s := NewGitSizeInt(4096)
	got, err := s.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "4096" {
		t.Errorf("got %q, want %q", got, "4096")
	}
}

func TestNewGitSize(t *testing.T) {
	t.Parallel()
	s, err := NewGitSize("10m")
	if err != nil {
		t.Fatal(err)
	}
	if s.Int64() != 10*1024*1024 {
		t.Errorf("got %d, want %d", s.Int64(), 10*1024*1024)
	}
	got, _ := s.MarshalGitConfig()
	if got != "10m" {
		t.Errorf("round-trip: got %q, want %q", got, "10m")
	}
}

func TestNewGitSizeInvalid(t *testing.T) {
	t.Parallel()
	_, err := NewGitSize("abc")
	if err == nil {
		t.Error("expected error for invalid size")
	}
}

func TestBoolOrIntBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		isBool bool
		boolVal bool
	}{
		{"true", true, true},
		{"false", true, false},
		{"yes", true, true},
		{"no", true, false},
		{"1", true, true},
		{"0", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			var b BoolOrInt
			if err := b.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatal(err)
			}
			if tt.isBool {
				if b.Bool == nil {
					t.Fatal("expected Bool to be set")
				}
				if *b.Bool != tt.boolVal {
					t.Errorf("Bool = %v, want %v", *b.Bool, tt.boolVal)
				}
				if b.Int != nil {
					t.Error("expected Int to be nil")
				}
			}
		})
	}
}

func TestBoolOrIntInt(t *testing.T) {
	t.Parallel()
	var b BoolOrInt
	if err := b.UnmarshalGitConfig([]byte("42")); err != nil {
		t.Fatal(err)
	}
	if b.Int == nil {
		t.Fatal("expected Int to be set")
	}
	if *b.Int != 42 {
		t.Errorf("Int = %d, want 42", *b.Int)
	}
	if b.Bool != nil {
		t.Error("expected Bool to be nil")
	}
}

func TestBoolOrIntInvalid(t *testing.T) {
	t.Parallel()
	var b BoolOrInt
	err := b.UnmarshalGitConfig([]byte("notaboolorint"))
	if err == nil {
		t.Error("expected error for invalid bool-or-int")
	}
}

func TestBoolOrIntMarshalBool(t *testing.T) {
	t.Parallel()
	b := NewBoolOrIntBool(true)
	got, err := b.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Errorf("got %q, want %q", got, "true")
	}
}

func TestBoolOrIntMarshalInt(t *testing.T) {
	t.Parallel()
	b := NewBoolOrIntInt(7)
	got, err := b.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "7" {
		t.Errorf("got %q, want %q", got, "7")
	}
}

func TestBoolOrIntMarshalEmpty(t *testing.T) {
	t.Parallel()
	var b BoolOrInt
	got, err := b.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestAutoBoolUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  AutoBool
	}{
		{"always", AutoBoolAlways},
		{"true", AutoBoolTrue},
		{"auto", AutoBoolAuto},
		{"false", AutoBoolFalse},
		{"never", AutoBoolNever},
		{"ALWAYS", AutoBoolAlways},
		{"True", AutoBoolTrue},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			var a AutoBool
			if err := a.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatal(err)
			}
			if a != tt.want {
				t.Errorf("got %q, want %q", a, tt.want)
			}
		})
	}
}

func TestAutoBoolMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range []AutoBool{AutoBoolAlways, AutoBoolTrue, AutoBoolAuto, AutoBoolFalse, AutoBoolNever} {
		got, err := v.MarshalGitConfig()
		if err != nil {
			t.Fatal(err)
		}
		if got != string(v) {
			t.Errorf("round-trip: got %q, want %q", got, string(v))
		}
	}
}

func TestColorUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		fg     string
		bg     string
		attrs  []string
	}{
		{"foreground only", "red", "red", "", nil},
		{"fg and bg", "red green", "red", "green", nil},
		{"fg bg attr", "red blue bold", "red", "blue", []string{"bold"}},
		{"fg bg multi-attr", "red blue bold ul", "red", "blue", []string{"bold", "ul"}},
		{"numeric color", "196", "196", "", nil},
		{"hex color", "#ff0000", "#ff0000", "", nil},
		{"empty", "", "", "", nil},
		{"normal", "normal", "", "", nil},
		{"reset", "reset", "", "", []string{"reset"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var c Color
			if err := c.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatal(err)
			}
			if c.Foreground != tt.fg {
				t.Errorf("Foreground = %q, want %q", c.Foreground, tt.fg)
			}
			if c.Background != tt.bg {
				t.Errorf("Background = %q, want %q", c.Background, tt.bg)
			}
			if len(c.Attributes) != len(tt.attrs) {
				t.Errorf("Attributes = %v, want %v", c.Attributes, tt.attrs)
			} else {
				for i := range tt.attrs {
					if c.Attributes[i] != tt.attrs[i] {
						t.Errorf("Attributes[%d] = %q, want %q", i, c.Attributes[i], tt.attrs[i])
					}
				}
			}
		})
	}
}

func TestColorMarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    Color
		want string
	}{
		{"fg only", Color{Foreground: "red"}, "red"},
		{"fg and bg", Color{Foreground: "red", Background: "green"}, "red green"},
		{"fg bg attrs", NewColor("red", "blue", "bold"), "red blue bold"},
		{"empty", Color{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.c.MarshalGitConfig()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestColorRoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{"red", "red green", "red blue bold ul", "196", "#ff0000"}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var c Color
			if err := c.UnmarshalGitConfig([]byte(input)); err != nil {
				t.Fatal(err)
			}
			got, err := c.MarshalGitConfig()
			if err != nil {
				t.Fatal(err)
			}
			if got != input {
				t.Errorf("round-trip: got %q, want %q", got, input)
			}
		})
	}
}

func TestNewColor(t *testing.T) {
	t.Parallel()
	c := NewColor("red", "green", "bold", "ul")
	if c.Foreground != "red" {
		t.Errorf("Foreground = %q, want %q", c.Foreground, "red")
	}
	if c.Background != "green" {
		t.Errorf("Background = %q, want %q", c.Background, "green")
	}
	if len(c.Attributes) != 2 {
		t.Fatalf("Attributes = %v, want 2 items", c.Attributes)
	}
	if c.Attributes[0] != "bold" || c.Attributes[1] != "ul" {
		t.Errorf("Attributes = %v, want [bold ul]", c.Attributes)
	}
}

func TestExpiryDateNever(t *testing.T) {
	t.Parallel()
	e := NewExpiryDateNever()
	if !e.Never {
		t.Error("expected Never = true")
	}
	if e.Now {
		t.Error("expected Now = false")
	}
	got, err := e.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "never" {
		t.Errorf("got %q, want %q", got, "never")
	}
	ref := time.Now()
	parsed, err := e.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.IsZero() {
		t.Error("expected zero time for never")
	}
}

func TestExpiryDateNow(t *testing.T) {
	t.Parallel()
	e := NewExpiryDateNow()
	if e.Never {
		t.Error("expected Never = false")
	}
	if !e.Now {
		t.Error("expected Now = true")
	}
	got, err := e.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "now" {
		t.Errorf("got %q, want %q", got, "now")
	}
	ref := time.Now()
	parsed, err := e.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(ref) {
		t.Errorf("Parse(now) = %v, want %v", parsed, ref)
	}
}

func TestExpiryDateRaw(t *testing.T) {
	t.Parallel()
	e := NewExpiryDate("2.weeks.ago")
	if e.Never || e.Now {
		t.Error("expected Never and Now to be false")
	}
	if e.Raw() != "2.weeks.ago" {
		t.Errorf("Raw() = %q, want %q", e.Raw(), "2.weeks.ago")
	}
}

func TestExpiryDateUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		never bool
		now   bool
	}{
		{"never", true, false},
		{"NEVER", true, false},
		{"now", false, true},
		{"Now", false, true},
		{"2.weeks.ago", false, false},
		{"90.days", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			var e ExpiryDate
			if err := e.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatal(err)
			}
			if e.Never != tt.never {
				t.Errorf("Never = %v, want %v", e.Never, tt.never)
			}
			if e.Now != tt.now {
				t.Errorf("Now = %v, want %v", e.Now, tt.now)
			}
		})
	}
}

func TestExpiryDateMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{"never", "now", "2.weeks.ago", "90.days", "1.day"}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var e ExpiryDate
			if err := e.UnmarshalGitConfig([]byte(input)); err != nil {
				t.Fatal(err)
			}
			got, err := e.MarshalGitConfig()
			if err != nil {
				t.Fatal(err)
			}
			if got != input {
				t.Errorf("round-trip: got %q, want %q", got, input)
			}
		})
	}
}

func TestExpiryDateMarshalFromNever(t *testing.T) {
	t.Parallel()
	e := ExpiryDate{Never: true}
	got, err := e.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "never" {
		t.Errorf("got %q, want %q", got, "never")
	}
}

func TestExpiryDateMarshalFromNow(t *testing.T) {
	t.Parallel()
	e := ExpiryDate{Now: true}
	got, err := e.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "now" {
		t.Errorf("got %q, want %q", got, "now")
	}
}

func TestExpiryDateMarshalEmpty(t *testing.T) {
	t.Parallel()
	var e ExpiryDate
	got, err := e.MarshalGitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExpiryDateParseRelative(t *testing.T) {
	t.Parallel()
	ref := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		input string
		want  time.Duration
	}{
		{"2.weeks.ago", 2 * 7 * 24 * time.Hour},
		{"90.days", 90 * 24 * time.Hour},
		{"1.day", 24 * time.Hour},
		{"3.months.ago", 3 * 30 * 24 * time.Hour},
		{"1.hour", time.Hour},
		{"5.minutes.ago", 5 * time.Minute},
		{"1.year", 365 * 24 * time.Hour},
		{"30.seconds", 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			e := NewExpiryDate(tt.input)
			parsed, err := e.Parse(ref)
			if err != nil {
				t.Fatal(err)
			}
			expected := ref.Add(-tt.want)
			if !parsed.Equal(expected) {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, parsed, expected)
			}
		})
	}
}

func TestExpiryDateParseBareInt(t *testing.T) {
	t.Parallel()
	ref := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	e := NewExpiryDate("3600")
	parsed, err := e.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	expected := ref.Add(-3600 * time.Second)
	if !parsed.Equal(expected) {
		t.Errorf("Parse(3600) = %v, want %v", parsed, expected)
	}
}

func TestExpiryDateParseRFC3339(t *testing.T) {
	t.Parallel()
	ts := "2024-06-01T00:00:00Z"
	e := NewExpiryDate(ts)
	ref := time.Now()
	parsed, err := e.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := time.Parse(time.RFC3339, ts)
	if !parsed.Equal(expected) {
		t.Errorf("Parse(%q) = %v, want %v", ts, parsed, expected)
	}
}

func TestExpiryDateParseInvalid(t *testing.T) {
	t.Parallel()
	e := NewExpiryDate("not-a-date")
	_, err := e.Parse(time.Now())
	if err == nil {
		t.Error("expected error for invalid expiry date")
	}
}

func TestExpiryDateParseEmpty(t *testing.T) {
	t.Parallel()
	var e ExpiryDate
	parsed, err := e.Parse(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.IsZero() {
		t.Error("expected zero time for empty ExpiryDate")
	}
}

func TestParseRelativeDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  time.Duration
		ok    bool
	}{
		{"2.weeks.ago", 2 * 7 * 24 * time.Hour, true},
		{"1.day", 24 * time.Hour, true},
		{"90.days", 90 * 24 * time.Hour, true},
		{"3.months.ago", 3 * 30 * 24 * time.Hour, true},
		{"1.second", time.Second, true},
		{"5.minutes.ago", 5 * time.Minute, true},
		{"1.hour", time.Hour, true},
		{"1.year", 365 * 24 * time.Hour, true},
		{"0.days", 0, false},
		{"-1.days", 0, false},
		{"1.fortnight", 0, false},
		{"1.day.ago.extra", 0, false},
		{"1.day.notago", 0, false},
		{"plain", 0, false},
		{"2", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, ok := parseRelativeDuration(tt.input)
			if ok != tt.ok {
				t.Errorf("parseRelativeDuration(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("parseRelativeDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPathnameRoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{"/usr/bin/git", "~/.gitconfig", "relative/path", ""}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var p Pathname
			if err := p.UnmarshalGitConfig([]byte(input)); err != nil {
				t.Fatal(err)
			}
			got, err := p.MarshalGitConfig()
			if err != nil {
				t.Fatal(err)
			}
			if got != input {
				t.Errorf("round-trip: got %q, want %q", got, input)
			}
		})
	}
}

func TestPathnameString(t *testing.T) {
	t.Parallel()
	p := Pathname("/some/path")
	if string(p) != "/some/path" {
		t.Errorf("string conversion: got %q, want %q", string(p), "/some/path")
	}
}

func TestFollowRedirectsUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  FollowRedirects
	}{
		{"true", FollowRedirectsAll},
		{"false", FollowRedirectsNone},
		{"initial", FollowRedirectsInitial},
		{"True", FollowRedirectsAll},
		{"INITIAL", FollowRedirectsInitial},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			var f FollowRedirects
			if err := f.UnmarshalGitConfig([]byte(tt.input)); err != nil {
				t.Fatal(err)
			}
			if f != tt.want {
				t.Errorf("got %q, want %q", f, tt.want)
			}
		})
	}
}

func TestFollowRedirectsMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range []FollowRedirects{FollowRedirectsAll, FollowRedirectsNone, FollowRedirectsInitial} {
		got, err := v.MarshalGitConfig()
		if err != nil {
			t.Fatal(err)
		}
		if got != string(v) {
			t.Errorf("round-trip: got %q, want %q", got, string(v))
		}
	}
}

func TestFollowRedirectsConstants(t *testing.T) {
	t.Parallel()
	if FollowRedirectsAll != "true" {
		t.Errorf("FollowRedirectsAll = %q, want %q", FollowRedirectsAll, "true")
	}
	if FollowRedirectsNone != "false" {
		t.Errorf("FollowRedirectsNone = %q, want %q", FollowRedirectsNone, "false")
	}
	if FollowRedirectsInitial != "initial" {
		t.Errorf("FollowRedirectsInitial = %q, want %q", FollowRedirectsInitial, "initial")
	}
}