package config

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

type EncoderSuite struct {
	suite.Suite
}

func TestEncoderSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(EncoderSuite))
}

func (s *EncoderSuite) TestEncode() {
	for idx, fixture := range fixtures {
		buf := &bytes.Buffer{}
		e := NewEncoder(buf)
		err := e.Encode(fixture.Config)
		s.NoError(err, fmt.Sprintf("encoder error for fixture: %d", idx))
		s.Equal(fixture.Text, buf.String(), fmt.Sprintf("bad result for fixture: %d", idx))
	}
}

func (s *EncoderSuite) TestEncodeQuotesValuesGitQuotes() {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"carriage return at the end", "foo\r", "\tkey = \"foo\r\"\n"},
		{"carriage return in the middle", "foo\rbar", "\tkey = \"foo\rbar\"\n"},
		{"carriage return at the start", "\rfoo", "\tkey = \"\rfoo\"\n"},
		{"trailing space", "foo ", "\tkey = \"foo \"\n"},
		{"leading space", " foo", "\tkey = \" foo\"\n"},
		{"trailing tab", "foo\t", "\tkey = \"foo\\t\"\n"},
		{"form feed is not whitespace to git", "foo\f", "\tkey = foo\f\n"},
		{"plain value", "foo", "\tkey = foo\n"},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			cfg := New().AddOption("section", "", "key", tc.value)
			buf := &bytes.Buffer{}
			s.Require().NoError(NewEncoder(buf).Encode(cfg))
			s.Equal("[section]\n"+tc.want, buf.String())
		})
	}
}

func (s *EncoderSuite) TestEncodeRoundTripsValues() {
	values := []string{
		"foo ", " foo", "foo\t", "foo\f", "foo\v", "a\\b", "a\"b",
		"a;b", "a#b", "foo\n",
	}

	for _, value := range values {
		s.Run(fmt.Sprintf("%q", value), func() {
			s.Equal(value, s.roundTrip(value))
		})
	}
}

// TestEncodeRoundTripsValuesWithCR needs a gcfg that keeps a quoted CR
// (go-git/gcfg#20, v2.0.3 and later).
func (s *EncoderSuite) TestEncodeRoundTripsValuesWithCR() {
	values := []string{
		"foo\r", "foo\rbar", "\rfoo", "foo\r\n", "foo\\\r", "\r", "foo\r\r",
	}

	for _, value := range values {
		s.Run(fmt.Sprintf("%q", value), func() {
			s.Equal(value, s.roundTrip(value))
		})
	}
}

func (s *EncoderSuite) roundTrip(value string) string {
	buf := &bytes.Buffer{}
	s.Require().NoError(NewEncoder(buf).Encode(New().AddOption("section", "", "key", value)))

	got := New()
	s.Require().NoError(NewDecoder(buf).Decode(got))
	return got.Section("section").Option("key")
}

func (s *EncoderSuite) TestEncodeRefusesNamesGitRefuses() {
	tests := []struct {
		name string
		cfg  *Config
		want error
	}{
		{
			name: "key with newline would inject an option",
			cfg:  New().AddOption("core", "", "safe", "1").AddOption("core", "", "x\n\tinjected", "pwned"),
			want: ErrInvalidKey,
		},
		{
			name: "key with newline in subsection",
			cfg:  New().AddOption("remote", "origin", "url\nevil", "x"),
			want: ErrInvalidKey,
		},
		{name: "empty key", cfg: New().AddOption("core", "", "", "v"), want: ErrInvalidKey},
		{name: "key starting with digit", cfg: New().AddOption("core", "", "1foo", "v"), want: ErrInvalidKey},
		{name: "key with underscore", cfg: New().AddOption("core", "", "foo_bar", "v"), want: ErrInvalidKey},
		{name: "key with space", cfg: New().AddOption("core", "", "foo bar", "v"), want: ErrInvalidKey},
		{name: "key with dot", cfg: New().AddOption("core", "", "foo.bar", "v"), want: ErrInvalidKey},
		{name: "key with equals", cfg: New().AddOption("core", "", "foo=bar", "v"), want: ErrInvalidKey},
		{name: "key with non-ASCII letter", cfg: New().AddOption("core", "", "föo", "v"), want: ErrInvalidKey},
		{
			name: "subsection with newline",
			cfg:  New().AddOption("remote", "a\nb", "url", "x"),
			want: ErrInvalidSubsectionName,
		},
		{
			name: "subsection with newline and no options",
			cfg:  configWithEmptySubsection("remote", "a\n[core]"),
			want: ErrInvalidSubsectionName,
		},
		{
			name: "subsection with NUL",
			cfg:  New().AddOption("core", "sshcommand\x00x", "k", "cmd"),
			want: ErrInvalidSubsectionName,
		},
		{name: "dotted section", cfg: New().AddOption("remote.origin", "", "url", "x"), want: ErrInvalidSectionName},
		{name: "non-ASCII section", cfg: New().AddOption("cör", "", "k", "v"), want: ErrInvalidSectionName},
		{name: "empty section", cfg: New().AddOption("", "", "k", "v"), want: ErrInvalidSectionName},
		{name: "section with newline", cfg: New().AddOption("core]\n[evil", "", "k", "v"), want: ErrInvalidSectionName},
		{name: "section with underscore", cfg: New().AddOption("co_re", "", "k", "v"), want: ErrInvalidSectionName},
		{name: "section with space", cfg: New().AddOption("co re", "", "k", "v"), want: ErrInvalidSectionName},
		{name: "section with closing bracket", cfg: New().AddOption("core]", "", "k", "v"), want: ErrInvalidSectionName},
	}

	for _, tc := range tests {
		buf := &bytes.Buffer{}
		err := NewEncoder(buf).Encode(tc.cfg)
		s.ErrorIs(err, tc.want, tc.name)
		s.Empty(buf.String(), "%s: nothing may be written for a refused config", tc.name)
	}
}

func (s *EncoderSuite) TestEncodeAcceptsNamesGitAccepts() {
	cfg := New().
		AddOption("core-1", "", "foo-bar", "1").
		AddOption("Core-X", "", "Key9", "2").
		AddOption("remote", `we"ird\name]`, "url", "3").
		AddOption("remote", "tab\tand space", "url", "4").
		AddOption("remote", "", "url", "5")

	buf := &bytes.Buffer{}
	s.Require().NoError(NewEncoder(buf).Encode(cfg))

	decoded := New()
	s.Require().NoError(NewDecoder(buf).Decode(decoded))
	s.Equal("3", decoded.Section("remote").Subsection(`we"ird\name]`).Option("url"))
	s.Equal("4", decoded.Section("remote").Subsection("tab\tand space").Option("url"))
	s.Equal("1", decoded.Section("core-1").Option("foo-bar"))
}

func configWithEmptySubsection(section, subsection string) *Config {
	cfg := New()
	cfg.Section(section).Subsection(subsection)
	return cfg
}
