package dotgit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/fs"

	"github.com/alcortesm/tgz"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

var initFixtures = [...]struct {
	name         string
	tgz          string
	capabilities [][2]string
	packfile     string
	idxfile      string
}{
	{
		name: "spinnaker",
		tgz:  "fixtures/spinnaker-gc.tgz",
		capabilities: [][2]string{
			{"symref", "HEAD:refs/heads/master"},
		},
		packfile: "objects/pack/pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack",
		idxfile:  "objects/pack/pack-584416f86235cac0d54bfabbdc399fb2b09a5269.idx",
	}, {
		name: "no-packfile-no-idx",
		tgz:  "fixtures/no-packfile-no-idx.tgz",
	}, {
		name: "empty",
		tgz:  "fixtures/empty-gitdir.tgz",
	},
}

type fixture struct {
	installDir   string
	fs           fs.FS
	path         string               // repo names to paths of the extracted tgz
	capabilities *common.Capabilities // expected capabilities
	packfile     string               // path of the packfile
	idxfile      string               // path of the idxfile
}

type SuiteDotGit struct {
	fixtures map[string]fixture
}

var _ = Suite(&SuiteDotGit{})

func (s *SuiteDotGit) SetUpSuite(c *C) {
	s.fixtures = make(map[string]fixture, len(initFixtures))

	for _, init := range initFixtures {
		com := Commentf("fixture name = %s\n", init.name)

		path, err := tgz.Extract(init.tgz)
		c.Assert(err, IsNil, com)

		f := fixture{}

		f.installDir = path
		f.fs = fs.NewOS()
		f.path = f.fs.Join(path, ".git")

		f.capabilities = common.NewCapabilities()
		for _, pair := range init.capabilities {
			f.capabilities.Add(pair[0], pair[1])
		}

		f.packfile = init.packfile
		f.idxfile = init.idxfile

		s.fixtures[init.name] = f
	}
}

func (s *SuiteDotGit) TearDownSuite(c *C) {
	for n, f := range s.fixtures {
		err := os.RemoveAll(f.installDir)
		c.Assert(err, IsNil, Commentf("cannot delete tmp dir for fixture %s: %s\n",
			n, f.installDir))
	}
}

func (s *SuiteDotGit) TestNewErrors(c *C) {
	for i, test := range [...]struct {
		input string
		err   error
	}{
		{
			input: "./tmp/foo",
			err:   ErrNotFound,
		}, {
			input: "./tmp/foo/.git",
			err:   ErrNotFound,
		},
	} {
		com := Commentf("subtest %d", i)

		_, err := New(fs.NewOS(), test.input)
		c.Assert(err, Equals, test.err, com)
	}
}

func (s *SuiteDotGit) TestRefsFromPackedRefs(c *C) {
	_, d := s.newFixtureDir(c, "spinnaker")
	refs, err := d.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/tags/v0.37.0")
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "85ec60477681933961c9b64c18ada93220650ac5")

}
func (s *SuiteDotGit) TestRefsFromReferenceFile(c *C) {
	_, d := s.newFixtureDir(c, "spinnaker")
	refs, err := d.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/remotes/origin/master")

}

func (s *SuiteDotGit) TestRefsFromHEADFile(c *C) {
	_, d := s.newFixtureDir(c, "spinnaker")
	refs, err := d.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/heads/master")
}

func findReference(refs []*core.Reference, name string) *core.Reference {
	n := core.ReferenceName(name)
	for _, ref := range refs {
		if ref.Name() == n {
			return ref
		}
	}

	return nil
}

func (s *SuiteDotGit) newFixtureDir(c *C, fixName string) (*fixture, *DotGit) {
	f, ok := s.fixtures[fixName]
	c.Assert(ok, Equals, true)

	d, err := New(fs.NewOS(), f.path)
	c.Assert(err, IsNil)

	return &f, d
}

func (s *SuiteDotGit) TestPackfile(c *C) {
	packfile := func(d *DotGit) (fs.FS, string, error) {
		return d.Packfile()
	}
	idxfile := func(d *DotGit) (fs.FS, string, error) {
		return d.Idxfile()
	}
	for _, test := range [...]struct {
		fixture string
		fn      getPathFn
		err     string // error regexp
	}{
		{
			fixture: "spinnaker",
			fn:      packfile,
		}, {
			fixture: "spinnaker",
			fn:      idxfile,
		}, {
			fixture: "empty",
			fn:      packfile,
			err:     ".* no such file or directory",
		}, {
			fixture: "empty",
			fn:      idxfile,
			err:     ".* no such file or directory",
		}, {
			fixture: "no-packfile-no-idx",
			fn:      packfile,
			err:     "packfile not found",
		}, {
			fixture: "no-packfile-no-idx",
			fn:      idxfile,
			err:     "idx file not found",
		},
	} {
		com := Commentf("fixture = %s", test.fixture)

		fix, dir := s.newFixtureDir(c, test.fixture)

		_, path, err := test.fn(dir)

		if test.err != "" {
			c.Assert(err, ErrorMatches, test.err, com)
		} else {
			c.Assert(err, IsNil, com)
			c.Assert(strings.HasSuffix(noExt(path), noExt(fix.packfile)),
				Equals, true, com)
		}
	}
}

type getPathFn func(*DotGit) (fs.FS, string, error)

func noExt(path string) string {
	ext := filepath.Ext(path)
	return path[0 : len(path)-len(ext)]
}
