package packp

import (
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

type UlReqSuite struct{}

var _ = Suite(&UlReqSuite{})

func (s *UlReqSuite) TestNewUploadRequestFromCapabilities(c *C) {
	cap := capability.NewList()
	cap.Set(capability.Sideband)
	cap.Set(capability.Sideband64k)
	cap.Set(capability.MultiACK)
	cap.Set(capability.MultiACKDetailed)
	cap.Set(capability.ThinPack)
	cap.Set(capability.OFSDelta)
	cap.Set(capability.Agent, "foo")

	r := NewUploadRequestFromCapabilities(cap)
	c.Assert(r.Capabilities.String(), Equals,
		"multi_ack_detailed side-band-64k thin-pack ofs-delta agent=go-git/5.x",
	)
}

func (s *UlReqSuite) TestValidateWants(c *C) {
	r := NewUploadRequest()
	err := r.Validate()
	c.Assert(err, NotNil)

	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	err = r.Validate()
	c.Assert(err, IsNil)
}

func (s *UlReqSuite) TestValidateShallows(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Shallows = append(r.Shallows, plumbing.NewHash("2222222222222222222222222222222222222222"))
	err := r.Validate()
	c.Assert(err, NotNil)

	r.Capabilities.Set(capability.Shallow)
	err = r.Validate()
	c.Assert(err, IsNil)
}

func (s *UlReqSuite) TestValidateDepthCommits(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthCommits(42)

	err := r.Validate()
	c.Assert(err, NotNil)

	r.Capabilities.Set(capability.Shallow)
	err = r.Validate()
	c.Assert(err, IsNil)
}

func (s *UlReqSuite) TestValidateDepthReference(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthReference("1111111111111111111111111111111111111111")

	err := r.Validate()
	c.Assert(err, NotNil)

	r.Capabilities.Set(capability.DeepenNot)
	err = r.Validate()
	c.Assert(err, IsNil)
}

func (s *UlReqSuite) TestValidateDepthSince(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Depth = DepthSince(time.Now())

	err := r.Validate()
	c.Assert(err, NotNil)

	r.Capabilities.Set(capability.DeepenSince)
	err = r.Validate()
	c.Assert(err, IsNil)
}

func (s *UlReqSuite) TestValidateConflictSideband(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Capabilities.Set(capability.Sideband)
	r.Capabilities.Set(capability.Sideband64k)
	err := r.Validate()
	c.Assert(err, NotNil)
}

func (s *UlReqSuite) TestValidateConflictMultiACK(c *C) {
	r := NewUploadRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	r.Capabilities.Set(capability.MultiACK)
	r.Capabilities.Set(capability.MultiACKDetailed)
	err := r.Validate()
	c.Assert(err, NotNil)
}
