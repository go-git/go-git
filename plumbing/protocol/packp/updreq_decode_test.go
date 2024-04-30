package packp

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"

	. "gopkg.in/check.v1"
)

type UpdReqDecodeSuite struct{}

var _ = Suite(&UpdReqDecodeSuite{})

func (s *UpdReqDecodeSuite) TestEmpty(c *C) {
	r := NewUpdateRequests()
	var buf bytes.Buffer
	c.Assert(r.Decode(&buf), Equals, ErrEmpty)
	c.Assert(r, DeepEquals, NewUpdateRequests())
}

func (s *UpdReqDecodeSuite) TestInvalidPktlines(c *C) {
	r := NewUpdateRequests()
	input := bytes.NewReader([]byte("xxxxxxxxxx"))
	c.Assert(r.Decode(input), ErrorMatches, "invalid pkt-len found")
}

func (s *UpdReqDecodeSuite) TestInvalidShadow(c *C) {
	payloads := []string{
		"shallow",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid shallow line length: expected 48, got 7$")

	payloads = []string{
		"shallow ",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid shallow line length: expected 48, got 8$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec65",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid shallow line length: expected 48, got 44$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e54",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid shallow line length: expected 48, got 49$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584eu",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid shallow object id: invalid hash: .*")
}

func (s *UpdReqDecodeSuite) TestMalformedCommand(c *C) {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5x2ecf0ef2c2dffb796033e5a02219af86ec6584e5xmyref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: malformed command: EOF$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5x2ecf0ef2c2dffb796033e5a02219af86ec6584e5xmyref",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: malformed command: EOF$")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandInvalidHash(c *C) {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid old object id: invalid hash size: expected 40, got 39$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid new object id: invalid hash size: expected 40, got 39$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86e 2ecf0ef2c2dffb796033e5a02219af86ec6 m\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 72$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584eu 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid old object id: invalid hash: .*$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584eu myref\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid new object id: invalid hash: .*$")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandMissingNullDelimiter(c *C) {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "capabilities delimiter not found")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandMissingName(c *C) {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5\x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 82$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 \x00",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 83$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid command line length: expected at least 83, got 81$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 ",
		"",
	}
	s.testDecoderErrorMatches(c, toPktLines(c, payloads), "^malformed request: invalid command line length: expected at least 83, got 82$")
}

func (s *UpdReqDecodeSuite) TestOneUpdateCommand(c *C) {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}
	// expected.Packfile = io.NopCloser(bytes.NewReader([]byte{}))

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}

	s.testDecodeOkExpected(c, expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommands(c *C) {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	// expected.Packfile = io.NopCloser(bytes.NewReader([]byte{}))

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(c, expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommandsAndCapabilities(c *C) {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	expected.Capabilities.Add("shallow")
	// expected.Packfile = io.NopCloser(bytes.NewReader([]byte{}))

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(c, expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommandsAndCapabilitiesShallow(c *C) {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	expected.Capabilities.Add("shallow")
	expected.Shallow = &hash1
	// expected.Packfile = io.NopCloser(bytes.NewReader([]byte{}))

	payloads := []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(c, expected, payloads)
}

func (s *UpdReqDecodeSuite) TestWithPackfile(c *C) {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}
	packfileContent := []byte("PACKabc")
	// expected.Packfile = io.NopCloser(bytes.NewReader(packfileContent))

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			c.Assert(pktline.WriteFlush(&buf), IsNil)
		} else {
			_, err := pktline.WriteString(&buf, p)
			c.Assert(err, IsNil)
		}
	}
	buf.Write(packfileContent)

	s.testDecodeOkRaw(c, expected, buf.Bytes())
}

func (s *UpdReqDecodeSuite) testDecoderErrorMatches(c *C, input io.Reader, pattern string) {
	r := NewUpdateRequests()
	c.Assert(r.Decode(input), ErrorMatches, pattern)
}

func (s *UpdReqDecodeSuite) testDecodeOK(c *C, payloads []string) *UpdateRequests {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			c.Assert(pktline.WriteFlush(&buf), IsNil)
		} else {
			_, err := pktline.WriteString(&buf, p)
			c.Assert(err, IsNil)
		}
	}

	r := NewUpdateRequests()
	c.Assert(r.Decode(&buf), IsNil)

	return r
}

func (s *UpdReqDecodeSuite) testDecodeOkRaw(c *C, expected *UpdateRequests, raw []byte) {
	req := NewUpdateRequests()
	c.Assert(req.Decode(bytes.NewBuffer(raw)), IsNil)
	// c.Assert(req.Packfile, NotNil)
	// s.compareReaders(c, req.Packfile, expected.Packfile)
	// req.Packfile = nil
	// expected.Packfile = nil
	c.Assert(req, DeepEquals, expected)
}

func (s *UpdReqDecodeSuite) testDecodeOkExpected(c *C, expected *UpdateRequests, payloads []string) {
	req := s.testDecodeOK(c, payloads)
	// c.Assert(req.Packfile, NotNil)
	// s.compareReaders(c, req.Packfile, expected.Packfile)
	// req.Packfile = nil
	// expected.Packfile = nil
	c.Assert(req, DeepEquals, expected)
}

func (s *UpdReqDecodeSuite) compareReaders(c *C, a io.ReadCloser, b io.ReadCloser) {
	pba, err := io.ReadAll(a)
	c.Assert(err, IsNil)
	c.Assert(a.Close(), IsNil)
	pbb, err := io.ReadAll(b)
	c.Assert(err, IsNil)
	c.Assert(b.Close(), IsNil)
	c.Assert(pba, DeepEquals, pbb)
}
