package packp

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UploadPackRequestSuite struct {
	suite.Suite
}

func TestUploadPackRequestSuite(t *testing.T) {
	suite.Run(t, new(UploadPackRequestSuite))
}

func (s *UploadPackRequestSuite) TestNewUploadPackRequestFromCapabilities() {
	cap := capability.NewList()
	cap.Set(capability.Agent, "foo")

	r := NewUploadPackRequestFromCapabilities(cap)
	s.Equal("agent=go-git/5.x", r.Capabilities.String())
}

func (s *UploadPackRequestSuite) TestIsEmpty() {
	r := NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Haves = append(r.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	s.False(r.IsEmpty())

	r = NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Haves = append(r.Haves, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))

	s.False(r.IsEmpty())

	r = NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Haves = append(r.Haves, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))

	s.True(r.IsEmpty())

	r = NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Haves = append(r.Haves, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Shallows = append(r.Shallows, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))

	s.False(r.IsEmpty())
}

type UploadHavesSuite struct {
	suite.Suite
}

func TestUploadHavesSuite(t *testing.T) {
	suite.Run(t, new(UploadHavesSuite))
}

func (s *UploadHavesSuite) TestEncode() {
	uh := &UploadHaves{}
	uh.Haves = append(uh.Haves,
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
	)

	buf := bytes.NewBuffer(nil)
	err := uh.Encode(buf, true)
	s.NoError(err)
	s.Equal(""+
		"0032have 1111111111111111111111111111111111111111\n"+
		"0032have 2222222222222222222222222222222222222222\n"+
		"0032have 3333333333333333333333333333333333333333\n"+
		"0000",
		buf.String(),
	)
}
