package git

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/src-d/go-git-fixtures"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/plumbing"
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

	s.path = dir
}

func (s *SubmoduleSuite) TearDownTest(c *C) {
	err := os.RemoveAll(s.path)
	c.Assert(err, IsNil)
}

func (s *SubmoduleSuite) TestInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Init()
	c.Assert(err, IsNil)

	cfg, err := s.Repository.Config()
	c.Assert(err, IsNil)

	c.Assert(cfg.Submodules, HasLen, 1)
	c.Assert(cfg.Submodules["basic"], NotNil)
}

func (s *SubmoduleSuite) TestUpdate(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})

	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

}

func (s *SubmoduleSuite) TestUpdateWithoutInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{})
	c.Assert(err, Equals, ErrSubmoduleNotInitialized)
}

func (s *SubmoduleSuite) TestUpdateWithNotFetch(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:    true,
		NoFetch: true,
	})

	// Since we are not fetching, the object is not there
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *SubmoduleSuite) TestUpdateWithRecursion(c *C) {
	sm, err := s.Worktree.Submodule("itself")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:              true,
		RecurseSubmodules: 2,
	})

	c.Assert(err, IsNil)

	_, err = s.Worktree.fs.Stat("itself/basic/LICENSE")
	c.Assert(err, IsNil)
}

func (s *SubmoduleSuite) TestUpdateWithInitAndUpdate(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})
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

	err = sm.Update(&SubmoduleUpdateOptions{})
	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")

}

func (s *SubmoduleSuite) TestSubmodulesInit(c *C) {
	sm, err := s.Worktree.Submodules()
	c.Assert(err, IsNil)

	err = sm.Init()
	c.Assert(err, IsNil)

	sm, err = s.Worktree.Submodules()
	c.Assert(err, IsNil)

	for _, m := range sm {
		c.Assert(m.initialized, Equals, true)
	}
}
