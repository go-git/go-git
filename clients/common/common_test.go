package common

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

func (s *SuiteCommon) TestNewEndpoint(c *C) {
	e, err := NewEndpoint("git@github.com:user/repository.git")
	c.Assert(err, IsNil)
	c.Assert(e, Equals, Endpoint("https://github.com/user/repository.git"))
}

func (s *SuiteCommon) TestNewEndpointWrongForgat(c *C) {
	e, err := NewEndpoint("foo")
	c.Assert(err, Not(IsNil))
	c.Assert(e, Equals, Endpoint(""))
}

func (s *SuiteCommon) TestEndpointService(c *C) {
	e, _ := NewEndpoint("git@github.com:user/repository.git")
	c.Assert(e.Service("foo"), Equals, "https://github.com/user/repository.git/info/refs?service=foo")
}

const CapabilitiesFixture = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADmulti_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag multi_ack_detailed no-done symref=HEAD:refs/heads/master agent=git/2:2.4.8~dbussink-fix-enterprise-tokens-compilation-1167-gc7006cf"

func (s *SuiteCommon) TestCapabilitiesSymbolicReference(c *C) {
	cap := parseCapabilities(CapabilitiesFixture)
	c.Assert(cap.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}
