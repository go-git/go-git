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
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48 or 72, got 7$")

	payloads = []string{
		"shallow ",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48 or 72, got 8$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec65",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48 or 72, got 44$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e54",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow line length: expected 48 or 72, got 49$")

	payloads = []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584eu",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid shallow object id: invalid hash: .*")
}

func (s *UpdReqDecodeSuite) TestShallowWithTrailingNewline() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref"), Old: hash1, New: hash2},
	}
	expected.Capabilities.Add("shallow")
	expected.Shallows = []plumbing.Hash{hash1}

	// Shallow line with trailing newline (49 bytes), as sent by reference Git.
	payloads := []string{
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e5\n",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00shallow",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
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
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid old object id: invalid hash: ")

	payloads = []string{
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e myref\x00",
		"",
	}
	s.testDecoderErrorMatches(toPktLines(s.T(), payloads), "^malformed request: invalid new object id: invalid hash: ")

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
	expected.Shallows = []plumbing.Hash{hash1}

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

func (s *UpdReqDecodeSuite) TestMultipleShallowLines() {
	hash1 := plumbing.NewHash("0c22e5ae8f25b3b070c9cad6e998ab388c636091")
	hash2 := plumbing.NewHash("1a129f5ef8baf66eaf9fc391c8104d0e7d6f95f5")
	hash3 := plumbing.NewHash("2c218c2559bf07b9af9276d419690d00f67ece4d")
	hash4 := plumbing.NewHash("58d4ed64098e7a4c94624983bafc84f996a546d0")
	hash5 := plumbing.NewHash("5e33f7c8d9421227b8f38caebb504a43d77cb8e0")
	hash6 := plumbing.NewHash("66b871af13b1a8ce0f3089492594012ca95c7354")
	hash7 := plumbing.NewHash("75b86200e94bf7d91dbe4e2b4f26a1bed0763929")
	hash8 := plumbing.NewHash("8e7c8bc9de53042384b28391442c1bc4a8e02663")
	hash9 := plumbing.NewHash("960b7ffc8b4a1170a3f6d763e22c8edd0c09f649")
	hash10 := plumbing.NewHash("a453d871a501e760fc7de453116bb9bcc74ff3fe")
	hash11 := plumbing.NewHash("b935865729bd5561d7583674114e0686a5e2a8da")
	hash12 := plumbing.NewHash("bac8a6bb3bb99813cc1982c98046bd38a0214b97")
	hash13 := plumbing.NewHash("ef046a102e1a97b2d670e83bced72612b6de0bd5")

	expected := NewUpdateRequests()
	expected.Commands = []*Command{
		{Name: plumbing.ReferenceName("refs/heads/func_add_report_displayVersion"), Old: plumbing.NewHash("0f5a36a5437539a952ca965e5ade1249a854efb8"), New: plumbing.NewHash("aa7bf2479778e73f18843ee7133eb05ab177522f")},
	}
	expected.Capabilities.Add("report-status-v2")
	expected.Capabilities.Add("side-band-64k")
	expected.Capabilities.Add("object-format", "sha1")
	expected.Capabilities.Add("agent", "git/2.39.5.(Apple.Git-154)")
	expected.Shallows = []plumbing.Hash{
		hash1, hash2, hash3, hash4, hash5, hash6, hash7,
		hash8, hash9, hash10, hash11, hash12, hash13,
	}

	payloads := []string{
		"shallow 0c22e5ae8f25b3b070c9cad6e998ab388c636091",
		"shallow 1a129f5ef8baf66eaf9fc391c8104d0e7d6f95f5",
		"shallow 2c218c2559bf07b9af9276d419690d00f67ece4d",
		"shallow 58d4ed64098e7a4c94624983bafc84f996a546d0",
		"shallow 5e33f7c8d9421227b8f38caebb504a43d77cb8e0",
		"shallow 66b871af13b1a8ce0f3089492594012ca95c7354",
		"shallow 75b86200e94bf7d91dbe4e2b4f26a1bed0763929",
		"shallow 8e7c8bc9de53042384b28391442c1bc4a8e02663",
		"shallow 960b7ffc8b4a1170a3f6d763e22c8edd0c09f649",
		"shallow a453d871a501e760fc7de453116bb9bcc74ff3fe",
		"shallow b935865729bd5561d7583674114e0686a5e2a8da",
		"shallow bac8a6bb3bb99813cc1982c98046bd38a0214b97",
		"shallow ef046a102e1a97b2d670e83bced72612b6de0bd5",
		"0f5a36a5437539a952ca965e5ade1249a854efb8 aa7bf2479778e73f18843ee7133eb05ab177522f refs/heads/func_add_report_displayVersion\x00report-status-v2 side-band-64k object-format=sha1 agent=git/2.39.5.(Apple.Git-154)",
		"",
	}

	s.testDecodeOkExpected(expected, payloads)
}

func (s *UpdReqDecodeSuite) testDecodeOkExpected(expected *UpdateRequests, payloads []string) {
	req := s.testDecodeOK(payloads)
	// s.NotNil(req.Packfile)
	// s.compareReaders(req.Packfile, expected.Packfile)
	// req.Packfile = nil
	// expected.Packfile = nil
	s.Equal(expected, req)
}
