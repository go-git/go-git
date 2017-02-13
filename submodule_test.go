package git

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/src-d/go-git-fixtures"

	. "gopkg.in/check.v1"
	"srcd.works/go-git.v4/plumbing"
)

type SubmoduleSuite struct {
	BaseSuite
	Worktree *Worktree
	path     string
}

var _ = Suite(&SubmoduleSuite{})

func (s *SubmoduleSuite) SetUpTest(c *C) {
	path := fixtures.ByTag("submodule").One().Worktree().Base()

	dir, err := ioutil.TempDir("", "submodule")
	c.Assert(err, IsNil)

	r, err := PlainClone(dir, false, &CloneOptions{
		URL: fmt.Sprintf("file://%s", filepath.Join(path)),
	})

	c.Assert(err, IsNil)

	s.Repository = r
	s.Worktree, err = r.Worktree()
	c.Assert(err, IsNil)

	s.path = path
}

func (s *SubmoduleSuite) TearDownTest(c *C) {
	err := os.RemoveAll(s.path)
	c.Assert(err, IsNil)
}

func (s *SubmoduleSuite) TestInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	_, err = sm.r.Reference(plumbing.HEAD, true)
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)

	err = sm.Init()
	c.Assert(err, IsNil)

	ref, err := sm.r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	w, err := sm.r.Worktree()
	c.Assert(err, IsNil)

	status, err := w.Status()
	c.Assert(err, IsNil)
	c.Assert(status.IsClean(), Equals, true)
}

func (s *SubmoduleSuite) TestUpdate(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	_, err = sm.r.Reference(plumbing.HEAD, true)
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)

	err = sm.Init()
	c.Assert(err, IsNil)

	idx, err := s.Repository.Storer.Index()
	c.Assert(err, IsNil)

	for i, e := range idx.Entries {
		if e.Name == "basic" {
			e.Hash = plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")
		}

		idx.Entries[i] = e
	}

	err = s.Repository.Storer.SetIndex(idx)
	c.Assert(err, IsNil)

	err = sm.Update()
	c.Assert(err, IsNil)

	ref, err := sm.r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")

}

func (s *SubmoduleSuite) TestSubmodulesInit(c *C) {
	sm, err := s.Worktree.Submodules()
	c.Assert(err, IsNil)

	err = sm.Init()
	c.Assert(err, IsNil)

	for _, m := range sm {
		ref, err := m.r.Reference(plumbing.HEAD, true)
		c.Assert(err, IsNil)
		c.Assert(ref.Hash(), Not(Equals), plumbing.ZeroHash)
	}
}
