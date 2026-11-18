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
