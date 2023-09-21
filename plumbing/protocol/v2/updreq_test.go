package packp

import (
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

type UpdReqSuite struct{}

var _ = Suite(&UpdReqSuite{})

func (s *UpdReqSuite) TestNewReferenceUpdateRequestFromCapabilities(c *C) {
	cap := capability.NewList()
	cap.Set(capability.Sideband)
	cap.Set(capability.Sideband64k)
	cap.Set(capability.Quiet)
	cap.Set(capability.ReportStatus)
	cap.Set(capability.DeleteRefs)
	cap.Set(capability.PushCert, "foo")
	cap.Set(capability.Atomic)
	cap.Set(capability.Agent, "foo")

	r := NewReferenceUpdateRequestFromCapabilities(cap)
	c.Assert(r.Capabilities.String(), Equals,
		"agent=go-git/5.x report-status",
	)

	cap = capability.NewList()
	cap.Set(capability.Agent, "foo")

	r = NewReferenceUpdateRequestFromCapabilities(cap)
	c.Assert(r.Capabilities.String(), Equals, "agent=go-git/5.x")

	cap = capability.NewList()

	r = NewReferenceUpdateRequestFromCapabilities(cap)
	c.Assert(r.Capabilities.String(), Equals, "")
}
