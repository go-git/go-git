package object

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type PatchSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&PatchSuite{})

func (s *PatchSuite) TestStatsWithSubmodules(c *C) {
	storer := filesystem.NewStorage(
		fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit(), cache.NewObjectLRUDefault())

	commit, err := GetCommit(storer, plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4"))
	c.Assert(err, IsNil)

	tree, err := commit.Tree()
	c.Assert(err, IsNil)

	e, err := tree.entry("basic")
	c.Assert(err, IsNil)

	ch := &Change{
		From: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
		To: ChangeEntry{
			Name:      "basic",
			Tree:      tree,
			TreeEntry: *e,
		},
	}

	p, err := getPatch("", ch)
	c.Assert(err, IsNil)
	c.Assert(p, NotNil)
}
