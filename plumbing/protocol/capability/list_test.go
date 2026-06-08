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
	cases := []struct {
		name         string
		add          [][]string
		wantLen      int
		wantGet      map[string][]string
		wantSupports []string
		wantIsEmpty  bool
	}{
		{
			name:        "empty",
			wantIsEmpty: true,
		},
		{
			name: "symref and thin-pack",
			add: [][]string{
				{SymRef, "foo", "qux"},
				{ThinPack},
			},
			wantLen: 2,
			wantGet: map[string][]string{
				SymRef:   {"foo", "qux"},
				ThinPack: nil,
			},
		},
		{
			name: "with leading space",
			add: [][]string{
				{ReportStatus},
			},
			wantLen: 1,
			wantSupports: []string{
				ReportStatus,
			},
		},
		{
			name: "with equal in argument",
			add: [][]string{
				{Agent, "foo=bar"},
			},
			wantLen: 1,
			wantGet: map[string][]string{
				Agent: {"foo=bar"},
			},
		},
		{
			name: "unknown capability",
			add: [][]string{
				{"foo"},
			},
			wantSupports: []string{
				"foo",
			},
		},
		{
			name: "unknown capability with argument",
			add: [][]string{
				{"oldref", "HEAD:refs/heads/v2"},
				{ThinPack},
			},
			wantLen: 2,
			wantGet: map[string][]string{
				"oldref": {"HEAD:refs/heads/v2"},
				ThinPack: nil,
			},
		},
		{
			name: "unknown capability with multiple arguments",
			add: [][]string{
				{"foo", "HEAD:refs/heads/v2", "HEAD:refs/heads/v1"},
				{ThinPack},
			},
			wantLen: 2,
			wantGet: map[string][]string{
				"foo":    {"HEAD:refs/heads/v2", "HEAD:refs/heads/v1"},
				ThinPack: nil,
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			caps := &List{}
			for _, args := range tc.add {
				caps.Add(args[0], args[1:]...)
			}

			if tc.wantLen > 0 {
				s.Len(caps.m, tc.wantLen)
			}
			if tc.wantIsEmpty {
				s.True(caps.IsEmpty())
			}
			for name, want := range tc.wantGet {
				if want == nil {
					s.Nil(caps.Get(name))
				} else {
					s.Equal(want, caps.Get(name))
				}
			}
			for _, name := range tc.wantSupports {
				s.True(caps.Supports(name))
			}
		})
	}
}

func (s *SuiteCapabilities) TestString() {
	cases := []struct {
		name       string
		set        [][]string
		wantString string
	}{
		{
			name: "sorted output",
			set: [][]string{
				{Agent, "bar"},
				{SymRef, "foo:qux"},
				{ThinPack},
			},
			wantString: "agent=bar symref=foo:qux thin-pack",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			caps := &List{}
			for _, args := range tc.set {
				caps.Set(args[0], args[1:]...)
			}

			s.Equal(tc.wantString, caps.String())
		})
	}
}

func (s *SuiteCapabilities) TestSet() {
	cases := []struct {
		name       string
		add        [][]string
		set        [][]string
		wantLen    int
		wantGet    map[string][]string
		wantString string
	}{
		{
			name: "overwrites add",
			add: [][]string{
				{SymRef, "foo", "qux"},
			},
			set: [][]string{
				{SymRef, "bar"},
			},
			wantLen: 1,
			wantGet: map[string][]string{
				SymRef: {"bar"},
			},
		},
		{
			name: "with argument",
			set: [][]string{
				{Agent, "bar"},
			},
			wantGet: map[string][]string{
				Agent: {"bar"},
			},
		},
		{
			name: "duplicate set keeps last",
			set: [][]string{
				{Agent, "baz"},
				{Agent, "bar"},
			},
			wantString: "agent=bar",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			caps := &List{}
			for _, args := range tc.add {
				caps.Add(args[0], args[1:]...)
			}
			for _, args := range tc.set {
				caps.Set(args[0], args[1:]...)
			}

			if tc.wantLen > 0 {
				s.Len(caps.m, tc.wantLen)
			}
			for name, want := range tc.wantGet {
				s.Equal(want, caps.Get(name))
			}
			if tc.wantString != "" {
				s.Equal(tc.wantString, caps.String())
			}
		})
	}
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
