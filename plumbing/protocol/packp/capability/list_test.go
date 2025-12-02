package capability

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SuiteCapabilities struct {
	suite.Suite
}

func TestSuiteCapabilities(t *testing.T) {
	suite.Run(t, new(SuiteCapabilities))
}

func (s *SuiteCapabilities) TestIsEmpty() {
	caps := NewList()
	s.True(caps.IsEmpty())
}

func (s *SuiteCapabilities) TestDecode() {
	caps := NewList()
	err := caps.Decode([]byte("symref=foo symref=qux thin-pack"))
	s.NoError(err)

	s.Len(caps.m, 2)
	s.Equal([]string{"foo", "qux"}, caps.Get(SymRef))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithLeadingSpace() {
	caps := NewList()
	err := caps.Decode([]byte(" report-status"))
	s.NoError(err)

	s.Len(caps.m, 1)
	s.True(caps.Supports(ReportStatus))
}

func (s *SuiteCapabilities) TestDecodeEmpty() {
	caps := NewList()
	err := caps.Decode(nil)
	s.NoError(err)
	s.Equal(NewList(), caps)
}

func (s *SuiteCapabilities) TestDecodeWithErrArguments() {
	caps := NewList()
	err := caps.Decode([]byte("thin-pack=foo"))
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestDecodeWithEqual() {
	caps := NewList()
	err := caps.Decode([]byte("agent=foo=bar"))
	s.NoError(err)

	s.Len(caps.m, 1)
	s.Equal([]string{"foo=bar"}, caps.Get(Agent))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapability() {
	caps := NewList()
	err := caps.Decode([]byte("foo"))
	s.NoError(err)
	s.True(caps.Supports(Capability("foo")))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithArgument() {
	caps := NewList()
	err := caps.Decode([]byte("oldref=HEAD:refs/heads/v2 thin-pack"))
	s.NoError(err)

	s.Len(caps.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2"}, caps.Get("oldref"))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithMultipleArgument() {
	caps := NewList()
	err := caps.Decode([]byte("foo=HEAD:refs/heads/v2 foo=HEAD:refs/heads/v1 thin-pack"))
	s.NoError(err)

	s.Len(caps.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2", "HEAD:refs/heads/v1"}, caps.Get("foo"))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestString() {
	caps := NewList()
	caps.Set(Agent, "bar")
	caps.Set(SymRef, "foo:qux")
	caps.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestStringSort() {
	caps := NewList()
	caps.Set(Agent, "bar")
	caps.Set(SymRef, "foo:qux")
	caps.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestSet() {
	caps := NewList()
	err := caps.Add(SymRef, "foo", "qux")
	s.NoError(err)
	err = caps.Set(SymRef, "bar")
	s.NoError(err)

	s.Len(caps.m, 1)
	s.Equal([]string{"bar"}, caps.Get(SymRef))
}

func (s *SuiteCapabilities) TestSetEmpty() {
	caps := NewList()
	err := caps.Set(Agent, "bar")
	s.NoError(err)

	s.Len(caps.Get(Agent), 1)
}

func (s *SuiteCapabilities) TestSetDuplicate() {
	caps := NewList()
	err := caps.Set(Agent, "baz")
	s.NoError(err)

	err = caps.Set(Agent, "bar")
	s.NoError(err)

	s.Equal("agent=bar", caps.String())
}

func (s *SuiteCapabilities) TestGetEmpty() {
	caps := NewList()
	s.Len(caps.Get(Agent), 0)
}

func (s *SuiteCapabilities) TestDelete() {
	caps := NewList()
	caps.Delete(SymRef)

	err := caps.Add(Sideband)
	s.NoError(err)
	err = caps.Set(SymRef, "bar")
	s.NoError(err)
	err = caps.Set(Sideband64k)
	s.NoError(err)

	caps.Delete(SymRef)

	s.Equal("side-band side-band-64k", caps.String())
}

func (s *SuiteCapabilities) TestAdd() {
	caps := NewList()
	err := caps.Add(SymRef, "foo", "qux")
	s.NoError(err)

	err = caps.Add(ThinPack)
	s.NoError(err)

	s.Equal("symref=foo symref=qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestAddUnknownCapability() {
	caps := NewList()
	err := caps.Add(Capability("foo"))
	s.NoError(err)
	s.True(caps.Supports(Capability("foo")))
}

func (s *SuiteCapabilities) TestAddErrArgumentsRequired() {
	caps := NewList()
	err := caps.Add(SymRef)
	s.ErrorIs(err, ErrArgumentsRequired)
}

func (s *SuiteCapabilities) TestAddErrArgumentsNotAllowed() {
	caps := NewList()
	err := caps.Add(OFSDelta, "foo")
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestAddErrArguments() {
	caps := NewList()
	err := caps.Add(SymRef, "")
	s.ErrorIs(err, ErrEmptyArgument)
}

func (s *SuiteCapabilities) TestAddErrMultipleArguments() {
	caps := NewList()
	err := caps.Add(Agent, "foo")
	s.NoError(err)

	err = caps.Add(Agent, "bar")
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestAddErrMultipleArgumentsAtTheSameTime() {
	caps := NewList()
	err := caps.Add(Agent, "foo", "bar")
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestAll() {
	caps := NewList()
	s.Nil(NewList().All())

	caps.Add(Agent, "foo")
	s.Equal([]Capability{Agent}, caps.All())

	caps.Add(OFSDelta)
	s.Equal([]Capability{Agent, OFSDelta}, caps.All())
}
