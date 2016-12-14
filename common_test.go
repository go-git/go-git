package git

import (
	"fmt"
	"testing"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/client"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type BaseSuite struct {
	fixtures.Suite
	Repository *Repository
	Storer     storer.EncodedObjectStorer

	cache map[string]*Repository
}

func (s *BaseSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)
	s.installMockProtocol(c)
	s.buildBasicRepository(c)

	s.cache = make(map[string]*Repository, 0)
}

func (s *BaseSuite) buildBasicRepository(c *C) {
	f := fixtures.Basic().One()
	s.Repository = s.NewRepository(f)
	s.Storer = s.Repository.s
}

func (s *BaseSuite) NewRepository(f *fixtures.Fixture) *Repository {
	r, err := NewFilesystemRepository(f.DotGit().Base())
	if err != nil {
		panic(err)
	}

	return r
}

func (s *BaseSuite) NewRepositoryFromPackfile(f *fixtures.Fixture) *Repository {
	h := f.PackfileHash.String()
	if r, ok := s.cache[h]; ok {
		return r
	}

	r := NewMemoryRepository()

	p := f.Packfile()
	defer p.Close()

	n := packfile.NewScanner(p)
	d, err := packfile.NewDecoder(n, r.s)
	if err != nil {
		panic(err)
	}

	_, err = d.Decode()
	if err != nil {
		panic(err)
	}

	s.cache[h] = r
	return r
}

func (s *BaseSuite) installMockProtocol(c *C) {
	client.InstallProtocol("https", nil)
}

func (s *BaseSuite) GetBasicLocalRepositoryURL() string {
	fixture := fixtures.Basic().One()
	return s.GetLocalRepositoryURL(fixture)
}

func (s *BaseSuite) GetLocalRepositoryURL(f *fixtures.Fixture) string {
	path := f.DotGit().Base()
	return fmt.Sprintf("file://%s", path)
}

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

var countLinesTests = [...]struct {
	i string // the string we want to count lines from
	e int    // the expected number of lines in i
}{
	{"", 0},
	{"a", 1},
	{"a\n", 1},
	{"a\nb", 2},
	{"a\nb\n", 2},
	{"a\nb\nc", 3},
	{"a\nb\nc\n", 3},
	{"a\n\n\nb\n", 4},
	{"first line\n\tsecond line\nthird line\n", 3},
}

func (s *SuiteCommon) TestCountLines(c *C) {
	for i, t := range countLinesTests {
		o := countLines(t.i)
		c.Assert(o, Equals, t.e, Commentf("subtest %d, input=%q", i, t.i))
	}
}
