package capability

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SuiteCapabilities struct {
	suite.Suite
}

func TestSuiteCapabilities(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteCapabilities))
}

func (s *SuiteCapabilities) TestIsEmpty() {
	caps := &List{}
	s.True(caps.IsEmpty())
}

func (s *SuiteCapabilities) TestDecode() {
	caps := &List{}
	// symref=foo symref=qux thin-pack
	caps.Add("symref", "foo", "qux")
	caps.Add("thin-pack")

	s.Len(caps.m, 2)
	s.Equal([]string{"foo", "qux"}, caps.Get(SymRef))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithLeadingSpace() {
	caps := &List{}
	caps.Add("report-status")

	s.Len(caps.m, 1)
	s.True(caps.Supports(ReportStatus))
}

func (s *SuiteCapabilities) TestDecodeEmpty() {
	caps := &List{}
	s.Equal(&List{}, caps)
}

func (s *SuiteCapabilities) TestDecodeWithEqual() {
	caps := &List{}
	caps.Add("agent", "foo=bar")

	s.Len(caps.m, 1)
	s.Equal([]string{"foo=bar"}, caps.Get(Agent))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapability() {
	caps := &List{}
	caps.Add("foo")
	s.True(caps.Supports("foo"))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithArgument() {
	caps := &List{}
	caps.Add("oldref", "HEAD:refs/heads/v2")
	caps.Add("thin-pack")

	s.Len(caps.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2"}, caps.Get("oldref"))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestDecodeWithUnknownCapabilityWithMultipleArgument() {
	caps := &List{}
	caps.Add("foo", "HEAD:refs/heads/v2", "HEAD:refs/heads/v1")
	caps.Add("thin-pack")

	s.Len(caps.m, 2)
	s.Equal([]string{"HEAD:refs/heads/v2", "HEAD:refs/heads/v1"}, caps.Get("foo"))
	s.Nil(caps.Get(ThinPack))
}

func (s *SuiteCapabilities) TestString() {
	caps := &List{}
	caps.Set(Agent, "bar")
	caps.Set(SymRef, "foo:qux")
	caps.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestStringSort() {
	caps := &List{}
	caps.Set(Agent, "bar")
	caps.Set(SymRef, "foo:qux")
	caps.Set(ThinPack)

	s.Equal("agent=bar symref=foo:qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestSet() {
	caps := &List{}
	caps.Add(SymRef, "foo", "qux")
	caps.Set(SymRef, "bar")

	s.Len(caps.m, 1)
	s.Equal([]string{"bar"}, caps.Get(SymRef))
}

func (s *SuiteCapabilities) TestSetEmpty() {
	caps := &List{}
	caps.Set(Agent, "bar")

	s.Len(caps.Get(Agent), 1)
}

func (s *SuiteCapabilities) TestSetDuplicate() {
	caps := &List{}
	caps.Set(Agent, "baz")
	caps.Set(Agent, "bar")

	s.Equal("agent=bar", caps.String())
}

func (s *SuiteCapabilities) TestGetEmpty() {
	caps := &List{}
	s.Len(caps.Get(Agent), 0)
}

func (s *SuiteCapabilities) TestDelete() {
	caps := &List{}
	caps.Delete(SymRef)

	caps.Add(Sideband)
	caps.Set(SymRef, "bar")
	caps.Set(Sideband64k)

	caps.Delete(SymRef)

	s.Equal("side-band side-band-64k", caps.String())
}

func (s *SuiteCapabilities) TestAdd() {
	caps := &List{}
	caps.Add(SymRef, "foo", "qux")
	caps.Add(ThinPack)

	s.Equal("symref=foo symref=qux thin-pack", caps.String())
}

func (s *SuiteCapabilities) TestAddUnknownCapability() {
	caps := &List{}
	caps.Add("foo")
	s.True(caps.Supports("foo"))
}

func (s *SuiteCapabilities) TestAll() {
	caps := &List{}
	s.Nil((&List{}).All())

	caps.Add(Agent, "foo")
	s.Equal([]string{Agent}, caps.All())

	caps.Add(OFSDelta)
	s.Equal([]string{Agent, OFSDelta}, caps.All())
}

func (s *SuiteCapabilities) TestZeroValueSafe() {
	var caps List

	s.True(caps.IsEmpty())
	s.Nil(caps.All())
	s.Nil(caps.Get(Agent))
	s.False(caps.Supports(Agent))

	caps.Add(Agent, "foo")
	s.False(caps.IsEmpty())
	s.True(caps.Supports(Agent))
	s.Equal([]string{"foo"}, caps.Get(Agent))
	s.Equal([]string{Agent}, caps.All())
	s.Equal("agent=foo", caps.String())

	caps.Delete(Agent)
	s.True(caps.IsEmpty())

	caps.Set(OFSDelta)
	s.True(caps.Supports(OFSDelta))
	s.Equal("ofs-delta", caps.String())
}
