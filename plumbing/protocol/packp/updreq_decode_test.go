package packp

import (
	"bytes"
	"io"
	"regexp"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type UpdReqDecodeSuite struct {
	suite.Suite
}

func TestUpdReqDecodeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(UpdReqDecodeSuite))
}

func (s *UpdReqDecodeSuite) TestEmpty() {
	r := NewUpdateRequests()
	var buf bytes.Buffer
	s.ErrorIs(r.Decode(&buf), ErrEmpty)
	s.Equal(NewUpdateRequests(), r)
}

func (s *UpdReqDecodeSuite) TestInvalidPktlines() {
	r := NewUpdateRequests()
	input := bytes.NewReader([]byte("xxxxxxxxxx"))
	s.Regexp(regexp.MustCompile("invalid pkt-len found"), r.Decode(input))
}

func (s *UpdReqDecodeSuite) TestInvalidShadow() {
	payloads := []string{
		"shallow",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48, got 7$")

	payloads = []string{
		"shallow ",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48, got 8$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec65",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48, got 44$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e54",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48, got 49$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584eu",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow object id: invalid hash: .*")
}

func (s *UpdReqDecodeSuite) TestMalformedCommand() {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5x2ecf0ef2c2dffb796033e5a02219af86ec6584e5xmyref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: malformed command: EOF$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5x2ecf0ef2c2dffb796033e5a02219af86ec6584e5xmyref",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: malformed command: EOF$")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandInvalidHash() {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid old object id: invalid hash size: expected 40, got 39$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid new object id: invalid hash size: expected 40, got 39$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86e 2ecf0ef2c2dffb796033e5a02219af86ec6 m\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 72$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584eu 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid old object id: invalid hash: .*$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584eu myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid new object id: invalid hash: .*$")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandMissingNullDelimiter() {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "capabilities delimiter not found")
}

func (s *UpdReqDecodeSuite) TestInvalidCommandMissingName() {
	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 82$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 \x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid command and capabilities line length: expected at least 84, got 83$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid command line length: expected at least 83, got 81$")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 ",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid command line length: expected at least 83, got 82$")
}

func (s *UpdReqDecodeSuite) TestOneUpdateCommand() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommands() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommandsAndCapabilities() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	expected.Capabilities.Add("shallow")

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
}

func (s *UpdReqDecodeSuite) TestMultipleCommandsAndCapabilitiesShallow() {
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

	payloads := []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
}

/*
* TODO: Implement packfile tests in plumbing/transport/push_test.go and
* [transport.SendPack].
func (s *UpdReqDecodeSuite) TestWithPackfile() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}
	packfileContent := []byte("PACKabc")
	expected.Packfile = io.NopCloser(bytes.NewReader(packfileContent))

	payloads := []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			s.Nil(pktline.WriteFlush(&buf))
		} else {
			_, err := pktline.WriteString(&buf, p)
			s.NoError(err)
		}
	}
	buf.Write(packfileContent)

	s.testDecodeOkRaw(expected, buf.Bytes())
}
*/

func (s *UpdReqDecodeSuite) testDecoderErrorMatches(input io.Reader, pattern string) {
	r := NewUpdateRequests()
	s.Regexp(regexp.MustCompile(pattern), r.Decode(input))
}

func (s *UpdReqDecodeSuite) testDecodeOK(payloads []string) *UpdateRequests {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			s.NoError(pktline.WriteFlush(&buf))
		} else {
			_, err := pktline.WriteString(&buf, p)
			s.NoError(err)
		}
	}

	r := NewUpdateRequests()
	s.Nil(r.Decode(&buf))

	return r
}

func (s *UpdReqDecodeSuite) testDecodeOkExpected(expected *UpdateRequests, payloads []string) {
	req := s.testDecodeOK(payloads)
	// s.NotNil(req.Packfile)
	// s.compareReaders(req.Packfile, expected.Packfile)
	// req.Packfile = nil
	// expected.Packfile = nil
	s.Equal(expected, req)
}
