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

func (s *EncoderSuite) TestEncodeRejectsUnrepresentableSubsectionNames() {
	for _, name := range []string{"a\nb", "a\x00b"} {
		cfg := New()
		cfg.Section("submodule").Subsection(name).SetOption("url", "https://example.com/x.git")

		err := NewEncoder(&bytes.Buffer{}).Encode(cfg)

		s.ErrorIs(err, ErrInvalidSubsectionName, "name %q", name)
	}
}

// A newline in a subsection name terminates the header early, so the file the
// encoder produced could not be read back by go-git or by git itself.
func (s *EncoderSuite) TestEncodeNeverWritesUnreadableSubsectionHeader() {
	cfg := New()
	cfg.Section("submodule").Subsection("a\nb").SetOption("url", "https://example.com/x.git")

	buf := &bytes.Buffer{}
	if err := NewEncoder(buf).Encode(cfg); err != nil {
		return
	}

	s.NoError(NewDecoder(bytes.NewReader(buf.Bytes())).Decode(New()),
		"encoder wrote a config that the decoder cannot read: %q", buf.String())
}
