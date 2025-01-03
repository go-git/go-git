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
	cap := NewList()
	s.True(cap.IsEmpty())
}

func (s *SuiteCapabilities) TestDecode() {
	cap := NewList()
	err := cap.Decode([]byte("symref=foo symref=qux thin-pack"))
	s.NoError(err)

	s.Len(cap.m, 2)
	s.Equal([]string{"foo", "qux"}, cap.Get(SymRef))
	s.Nil(cap.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithLeadingSpace() {
	cap := NewList()
	err := cap.Decode([]byte(" report-status"))
	s.NoError(err)

	s.Len(cap.m, 1)
	s.True(cap.Supports(ReportStatus))
}

func (s *SuiteCapabilities) TestDecodeEmpty() {
	cap := NewList()
	err := cap.Decode(nil)
	s.NoError(err)
	s.Equal(NewList(), cap)
}

func (s *SuiteCapabilities) TestDecodeWithErrArguments() {
	cap := NewList()
	err := cap.Decode([]byte("thin-pack=foo"))
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestDecodeWithEqual() {
	cap := NewList()
	err := cap.Decode([]byte("agent=foo=bar"))
	s.NoError(err)

	s.Len(cap.m, 1)
	s.Equal([]string{"foo=bar"}, cap.Get(Agent))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapability() {
	cap := NewList()
	err := cap.Decode([]byte("foo"))
	s.NoError(err)
	s.True(cap.Supports(Capability("foo")))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithArgument() {
	cap := NewList()
	err := cap.Decode([]byte("oldref=HEAD:refs/heads/v2 thin-pack"))
	s.NoError(err)

	s.Len(cap.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2"}, cap.Get("oldref"))
	s.Nil(cap.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithMultipleArgument() {
	cap := NewList()
	err := cap.Decode([]byte("foo=HEAD:refs/heads/v2 foo=HEAD:refs/heads/v1 thin-pack"))
	s.NoError(err)

	s.Len(cap.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2", "HEAD:refs/heads/v1"}, cap.Get("foo"))
	s.Nil(cap.Get(ThinPack))
}

func (s *SuiteCapabilities) TestString() {
	cap := NewList()
	cap.Set(Agent, "bar")
	cap.Set(SymRef, "foo:qux")
	cap.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", cap.String())
}

func (s *SuiteCapabilities) TestStringSort() {
	cap := NewList()
	cap.Set(Agent, "bar")
	cap.Set(SymRef, "foo:qux")
	cap.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", cap.String())
}

func (s *SuiteCapabilities) TestSet() {
	cap := NewList()
	err := cap.Add(SymRef, "foo", "qux")
	s.NoError(err)
	err = cap.Set(SymRef, "bar")
	s.NoError(err)

	s.Len(cap.m, 1)
	s.Equal([]string{"bar"}, cap.Get(SymRef))
}

func (s *SuiteCapabilities) TestSetEmpty() {
	cap := NewList()
	err := cap.Set(Agent, "bar")
	s.NoError(err)

	s.Len(cap.Get(Agent), 1)
}

func (s *SuiteCapabilities) TestSetDuplicate() {
	cap := NewList()
	err := cap.Set(Agent, "baz")
	s.NoError(err)

	err = cap.Set(Agent, "bar")
	s.NoError(err)

	s.Equal("agent=bar", cap.String())
}

func (s *SuiteCapabilities) TestGetEmpty() {
	cap := NewList()
	s.Len(cap.Get(Agent), 0)
}

func (s *SuiteCapabilities) TestDelete() {
	cap := NewList()
	cap.Delete(SymRef)

	err := cap.Add(Sideband)
	s.NoError(err)
	err = cap.Set(SymRef, "bar")
	s.NoError(err)
	err = cap.Set(Sideband64k)
	s.NoError(err)

	cap.Delete(SymRef)

	s.Equal("side-band side-band-64k", cap.String())
}

func (s *SuiteCapabilities) TestAdd() {
	cap := NewList()
	err := cap.Add(SymRef, "foo", "qux")
	s.NoError(err)

	err = cap.Add(ThinPack)
	s.NoError(err)

	s.Equal("symref=foo symref=qux thin-pack", cap.String())
}

func (s *SuiteCapabilities) TestAddUnknownCapability() {
	cap := NewList()
	err := cap.Add(Capability("foo"))
	s.NoError(err)
	s.True(cap.Supports(Capability("foo")))
}

func (s *SuiteCapabilities) TestAddErrArgumentsRequired() {
	cap := NewList()
	err := cap.Add(SymRef)
	s.ErrorIs(err, ErrArgumentsRequired)
}

func (s *SuiteCapabilities) TestAddErrArgumentsNotAllowed() {
	cap := NewList()
	err := cap.Add(OFSDelta, "foo")
	s.ErrorIs(err, ErrArguments)
}

func (s *SuiteCapabilities) TestAddErrArguments() {
	cap := NewList()
	err := cap.Add(SymRef, "")
	s.ErrorIs(err, ErrEmptyArgument)
}

func (s *SuiteCapabilities) TestAddErrMultipleArguments() {
	cap := NewList()
	err := cap.Add(Agent, "foo")
	s.NoError(err)

	err = cap.Add(Agent, "bar")
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestAddErrMultipleArgumentsAtTheSameTime() {
	cap := NewList()
	err := cap.Add(Agent, "foo", "bar")
	s.ErrorIs(err, ErrMultipleArguments)
}

func (s *SuiteCapabilities) TestAll() {
	cap := NewList()
	s.Nil(NewList().All())

	cap.Add(Agent, "foo")
	s.Equal([]Capability{Agent}, cap.All())

	cap.Add(OFSDelta)
	s.Equal([]Capability{Agent, OFSDelta}, cap.All())
}
