package packp

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCapabilities struct{}

var _ = Suite(&SuiteCapabilities{})

func (s *SuiteCapabilities) TestDecode(c *C) {
	cap := NewCapabilities()
	cap.Decode("symref=foo symref=qux thin-pack")

	c.Assert(cap.m, HasLen, 2)
	c.Assert(cap.Get("symref").Values, DeepEquals, []string{"foo", "qux"})
	c.Assert(cap.Get("thin-pack").Values, DeepEquals, []string{""})
}

func (s *SuiteCapabilities) TestSet(c *C) {
	cap := NewCapabilities()
	cap.Add("symref", "foo", "qux")
	cap.Set("symref", "bar")

	c.Assert(cap.m, HasLen, 1)
	c.Assert(cap.Get("symref").Values, DeepEquals, []string{"bar"})
}

func (s *SuiteCapabilities) TestSetEmpty(c *C) {
	cap := NewCapabilities()
	cap.Set("foo", "bar")

	c.Assert(cap.Get("foo").Values, HasLen, 1)
}

func (s *SuiteCapabilities) TestAdd(c *C) {
	cap := NewCapabilities()
	cap.Add("symref", "foo", "qux")
	cap.Add("thin-pack")

	c.Assert(cap.String(), Equals, "symref=foo symref=qux thin-pack")
}
