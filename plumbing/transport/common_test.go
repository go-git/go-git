package transport

import (
	"testing"

	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

func (s *SuiteCommon) TestNewEndpoint(c *C) {
	e, err := NewEndpoint("ssh://git@github.com/user/repository.git")
	c.Assert(err, IsNil)
	c.Assert(e.String(), Equals, "ssh://git@github.com/user/repository.git")
}

func (s *SuiteCommon) TestNewEndpointSCPLike(c *C) {
	e, err := NewEndpoint("git@github.com:user/repository.git")
	c.Assert(err, IsNil)
	c.Assert(e.String(), Equals, "ssh://git@github.com/user/repository.git")
}

func (s *SuiteCommon) TestNewEndpointWrongForgat(c *C) {
	e, err := NewEndpoint("foo")
	c.Assert(err, Not(IsNil))
	c.Assert(e.Host, Equals, "")
}

func (s *SuiteCommon) TestFilterUnsupportedCapabilities(c *C) {
	l := capability.NewList()
	l.Set(capability.MultiACK)

	FilterUnsupportedCapabilities(l)
	c.Assert(l.Supports(capability.MultiACK), Equals, false)
}
