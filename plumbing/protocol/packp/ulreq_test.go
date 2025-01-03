package packp

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UlReqSuite struct {
	suite.Suite
}

func TestUlReqSuite(t *testing.T) {
	suite.Run(t, new(UlReqSuite))
}

func (s *UlReqSuite) TestNewUploadRequestFromCapabilities() {
	cap := capability.NewList()
	cap.Set(capability.Sideband)
	cap.Set(capability.Sideband64k)
	cap.Set(capability.MultiACK)
	cap.Set(capability.MultiACKDetailed)
	cap.Set(capability.ThinPack)
	cap.Set(capability.OFSDelta)
	cap.Set(capability.Agent, "foo")

	r := NewUploadRequestFromCapabilities(cap)
	s.Equal("multi_ack_detailed side-band-64k thin-pack ofs-delta agent=go-git/5.x",
		r.Capabilities.String(),
	)
}

func (s *UlReqSuite) TestValidateWants() {
	r := NewUploadRequest()
	err := r.Validate()
	s.NotNil(err)

	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	err = r.Validate()
	s.NoError(err)
}

func (s *UlReqSuite) TestValidateShallows() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Shallows = append(r.Shallows, plumbing.NewHash("2222222222222222222222222222222222222222"))
	err := r.Validate()
	s.NotNil(err)

	r.Capabilities.Set(capability.Shallow)
	err = r.Validate()
	s.NoError(err)
}

func (s *UlReqSuite) TestValidateDepthCommits() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthCommits(42)

	err := r.Validate()
	s.NotNil(err)

	r.Capabilities.Set(capability.Shallow)
	err = r.Validate()
	s.NoError(err)
}

func (s *UlReqSuite) TestValidateDepthReference() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthReference("1111111111111111111111111111111111111111")

	err := r.Validate()
	s.NotNil(err)

	r.Capabilities.Set(capability.DeepenNot)
	err = r.Validate()
	s.NoError(err)
}

func (s *UlReqSuite) TestValidateDepthSince() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthSince(time.Now())

	err := r.Validate()
	s.NotNil(err)

	r.Capabilities.Set(capability.DeepenSince)
	err = r.Validate()
	s.NoError(err)
}

func (s *UlReqSuite) TestValidateConflictSideband() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Capabilities.Set(capability.Sideband)
	r.Capabilities.Set(capability.Sideband64k)
	err := r.Validate()
	s.NotNil(err)
}

func (s *UlReqSuite) TestValidateConflictMultiACK() {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Capabilities.Set(capability.MultiACK)
	r.Capabilities.Set(capability.MultiACKDetailed)
	err := r.Validate()
	s.NotNil(err)
}
