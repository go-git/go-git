package noder

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.org/x/text/unicode/norm"
)

type PathSuite struct {
	suite.Suite
}

func TestPathSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PathSuite))
}

func (s *PathSuite) TestShortFile() {
	f := &noderMock{
		name:  "1",
		isDir: false,
	}
	p := Path([]Noder{f})
	s.Equal("1", p.String())
}

func (s *PathSuite) TestShortDir() {
	d := &noderMock{
		name:     "1",
		isDir:    true,
		children: NoChildren,
	}
	p := Path([]Noder{d})
	s.Equal("1", p.String())
}

func (s *PathSuite) TestLongFile() {
	n3 := &noderMock{
		name:  "3",
		isDir: false,
	}
	n2 := &noderMock{
		name:     "2",
		isDir:    true,
		children: []Noder{n3},
	}
	n1 := &noderMock{
		name:     "1",
		isDir:    true,
		children: []Noder{n2},
	}
	p := Path([]Noder{n1, n2, n3})
	s.Equal("1/2/3", p.String())
}

func (s *PathSuite) TestLongDir() {
	n3 := &noderMock{
		name:     "3",
		isDir:    true,
		children: NoChildren,
	}
	n2 := &noderMock{
		name:     "2",
		isDir:    true,
		children: []Noder{n3},
	}
	n1 := &noderMock{
		name:     "1",
		isDir:    true,
		children: []Noder{n2},
	}
	p := Path([]Noder{n1, n2, n3})
	s.Equal("1/2/3", p.String())
}

func (s *PathSuite) TestCompareDepth1() {
	p1 := Path([]Noder{&noderMock{name: "a"}})
	p2 := Path([]Noder{&noderMock{name: "b"}})
	s.Equal(-1, p1.Compare(p2))
	s.Equal(1, p2.Compare(p1))

	p1 = Path([]Noder{&noderMock{name: "a"}})
	p2 = Path([]Noder{&noderMock{name: "a"}})
	s.Equal(0, p1.Compare(p2))
	s.Equal(0, p2.Compare(p1))

	p1 = Path([]Noder{&noderMock{name: "a.go"}})
	p2 = Path([]Noder{&noderMock{name: "a"}})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))
}

func (s *PathSuite) TestCompareDepth2() {
	p1 := Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "b"},
	})
	p2 := Path([]Noder{
		&noderMock{name: "b"},
		&noderMock{name: "a"},
	})
	s.Equal(-1, p1.Compare(p2))
	s.Equal(1, p2.Compare(p1))

	p1 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "b"},
	})
	p2 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "b"},
	})
	s.Equal(0, p1.Compare(p2))
	s.Equal(0, p2.Compare(p1))

	p1 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "b"},
	})
	p2 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "a"},
	})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))
}

func (s *PathSuite) TestCompareMixedDepths() {
	p1 := Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "b"},
	})
	p2 := Path([]Noder{&noderMock{name: "b"}})
	s.Equal(-1, p1.Compare(p2))
	s.Equal(1, p2.Compare(p1))

	p1 = Path([]Noder{
		&noderMock{name: "b"},
		&noderMock{name: "b"},
	})
	p2 = Path([]Noder{&noderMock{name: "b"}})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))

	p1 = Path([]Noder{&noderMock{name: "a.go"}})
	p2 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "a.go"},
	})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))

	p1 = Path([]Noder{&noderMock{name: "b.go"}})
	p2 = Path([]Noder{
		&noderMock{name: "a"},
		&noderMock{name: "a.go"},
	})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))
}

func (s *PathSuite) TestCompareNormalization() {
	p1 := Path([]Noder{&noderMock{name: norm.NFKC.String("페")}})
	p2 := Path([]Noder{&noderMock{name: norm.NFKD.String("페")}})
	s.Equal(1, p1.Compare(p2))
	s.Equal(-1, p2.Compare(p1))
	p1 = Path([]Noder{&noderMock{name: "TestAppWithUnicodéPath"}})
	p2 = Path([]Noder{&noderMock{name: "TestAppWithUnicodéPath"}})
	s.Equal(-1, p1.Compare(p2))
	s.Equal(1, p2.Compare(p1))
}
