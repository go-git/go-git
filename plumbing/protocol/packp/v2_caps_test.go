package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type V2CapabilitiesSuite struct {
	suite.Suite
}

func TestV2CapabilitiesSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(V2CapabilitiesSuite))
}

func (s *V2CapabilitiesSuite) advertisement(lines ...string) *bytes.Buffer {
	buf := bytes.NewBuffer(nil)
	for _, l := range lines {
		_, err := pktline.Writeln(buf, l)
		s.Require().NoError(err)
	}
	s.Require().NoError(pktline.WriteFlush(buf))
	return buf
}

func (s *V2CapabilitiesSuite) TestDecode() {
	buf := s.advertisement(
		"version 2",
		"agent=git/2.40.1",
		"ls-refs=unborn",
		"fetch=shallow wait-for-done filter",
		"object-format=sha1",
		"server-option",
	)

	var c V2Capabilities
	s.Require().NoError(c.Decode(buf))

	s.True(c.Supports("agent"))
	s.Equal("git/2.40.1", c.Get("agent"))
	s.Equal("sha1", c.Get("object-format"))

	s.True(c.Supports("server-option"))
	s.Equal("", c.Get("server-option"))

	s.True(c.SupportsArgument("ls-refs", "unborn"))
	s.True(c.SupportsArgument("fetch", "filter"))
	s.True(c.SupportsArgument("fetch", "wait-for-done"))
	s.False(c.SupportsArgument("fetch", "packfile-uris"))
	s.False(c.SupportsArgument("ls-refs", "symrefs"))
}

func (s *V2CapabilitiesSuite) TestDecodeMissingVersion() {
	buf := s.advertisement("agent=git/2.40.1")

	var c V2Capabilities
	err := c.Decode(buf)
	s.Error(err)
}

func (s *V2CapabilitiesSuite) TestDecodeWrongVersion() {
	buf := s.advertisement("version 1", "agent=git/2.40.1")

	var c V2Capabilities
	err := c.Decode(buf)
	s.Error(err)
}

func (s *V2CapabilitiesSuite) TestSupportsUnknown() {
	var c V2Capabilities
	buf := s.advertisement("version 2")
	s.Require().NoError(c.Decode(buf))

	s.False(c.Supports("fetch"))
	s.Equal("", c.Get("fetch"))
	s.False(c.SupportsArgument("fetch", "filter"))
}
