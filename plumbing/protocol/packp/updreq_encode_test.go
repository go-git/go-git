package packp

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UpdReqEncodeSuite struct {
	suite.Suite
}

func TestUpdReqEncodeSuite(t *testing.T) {
	suite.Run(t, new(UpdReqEncodeSuite))
}

func (s *UpdReqEncodeSuite) testEncode(input *UpdateRequests,
	expected []byte,
) {
	var buf bytes.Buffer
	s.Nil(input.Encode(&buf))
	obtained := buf.Bytes()

	comment := fmt.Sprintf("\nobtained = %s\nexpected = %s\n", string(obtained), string(expected))
	s.Equal(expected, obtained, comment)
}

func (s *UpdReqEncodeSuite) TestZeroValue() {
	r := &UpdateRequests{}
	var buf bytes.Buffer
	s.Equal(ErrEmptyCommands, r.Encode(&buf))

	r = NewUpdateRequests()
	s.Equal(ErrEmptyCommands, r.Encode(&buf))
}

func (s *UpdReqEncodeSuite) TestOneUpdateCommand() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	r := NewUpdateRequests()
	r.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}

	expected := pktlines(s.T(),
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	)

	s.testEncode(r, expected)
}

func (s *UpdReqEncodeSuite) TestMultipleCommands() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	r := NewUpdateRequests()
	r.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}

	expected := pktlines(s.T(),
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	)

	s.testEncode(r, expected)
}

func (s *UpdReqEncodeSuite) TestMultipleCommandsAndCapabilities() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	r := NewUpdateRequests()
	r.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	r.Capabilities.Add("shallow")

	expected := pktlines(s.T(),
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	)

	s.testEncode(r, expected)
}

func (s *UpdReqEncodeSuite) TestMultipleCommandsAndCapabilitiesShallow() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	r := NewUpdateRequests()
	r.Commands = []*Command{
		{Name: plumbing.ReferenceName("myref1"), Old: hash1, New: hash2},
		{Name: plumbing.ReferenceName("myref2"), Old: plumbing.ZeroHash, New: hash2},
		{Name: plumbing.ReferenceName("myref3"), Old: hash1, New: plumbing.ZeroHash},
	}
	r.Capabilities.Add("shallow")
	r.Shallow = &hash1

	expected := pktlines(s.T(),
		"shallow 1ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref1\x00shallow",
		"0000000000000000000000000000000000000000 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref2",
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 0000000000000000000000000000000000000000 myref3",
		"",
	)

	s.testEncode(r, expected)
}

/*
func (s *UpdReqEncodeSuite) TestWithPackfile() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	packfileContent := []byte("PACKabc")
	packfileReader := bytes.NewReader(packfileContent)
	packfileReadCloser := io.NopCloser(packfileReader)

	r := NewUpdateRequests()
	r.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}
	r.Packfile = packfileReadCloser

	expected := pktlines(s.T(),
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00",
		"",
	)
	expected = append(expected, packfileContent...)

	s.testEncode(r, expected)
}
*/

func (s *UpdReqEncodeSuite) TestPushAtomic() {
	hash1 := plumbing.NewHash("1ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hash2 := plumbing.NewHash("2ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	name := plumbing.ReferenceName("myref")

	r := NewUpdateRequests()
	r.Capabilities.Set(capability.Atomic)
	r.Commands = []*Command{
		{Name: name, Old: hash1, New: hash2},
	}

	expected := pktlines(s.T(),
		"1ecf0ef2c2dffb796033e5a02219af86ec6584e5 2ecf0ef2c2dffb796033e5a02219af86ec6584e5 myref\x00atomic",
		"",
	)

	s.testEncode(r, expected)
}
