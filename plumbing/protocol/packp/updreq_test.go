package packp

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UpdReqSuite struct {
	suite.Suite
}

func TestUpdReqSuite(t *testing.T) {
	suite.Run(t, new(UpdReqSuite))
}

func (s *UpdReqSuite) TestNewReferenceUpdateRequestFromCapabilities() {
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
	s.Equal("agent=go-git/5.x report-status",
		r.Capabilities.String(),
	)

	cap = capability.NewList()
	cap.Set(capability.Agent, "foo")

	r = NewReferenceUpdateRequestFromCapabilities(cap)
	s.Equal("agent=go-git/5.x", r.Capabilities.String())

	cap = capability.NewList()

	r = NewReferenceUpdateRequestFromCapabilities(cap)
	s.Equal("", r.Capabilities.String())
}
