package config

import . "gopkg.in/check.v1"

type ModuleSuite struct{}

var _ = Suite(&ModuleSuite{})

func (s *ModuleSuite) TestModuleValidateMissingURL(c *C) {
	m := &Module{Path: "foo"}
	c.Assert(m.Validate(), Equals, ErrModuleEmptyURL)
}

func (s *ModuleSuite) TestModuleValidateMissingName(c *C) {
	m := &Module{URL: "bar"}
	c.Assert(m.Validate(), Equals, ErrModuleEmptyPath)
}

func (s *ModuleSuite) TestModuleValidateDefault(c *C) {
	m := &Module{Path: "foo", URL: "http://foo/bar"}
	c.Assert(m.Validate(), IsNil)
	c.Assert(m.Branch, Equals, DefaultModuleBranch)
}
