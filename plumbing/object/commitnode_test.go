package object

import (
	"path"

	"golang.org/x/exp/mmap"
	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git-fixtures.v3"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/plumbing/format/commitgraph"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
)

type CommitNodeSuite struct {
	fixtures.Suite
}

var _ = Suite(&CommitNodeSuite{})

func testWalker(c *C, nodeIndex CommitNodeIndex) {
	head, err := nodeIndex.Get(plumbing.NewHash("b9d69064b190e7aedccf84731ca1d917871f8a1c"))
	c.Assert(err, IsNil)

	iter := NewCommitNodeIterCTime(
		head,
		nil,
		nil,
	)

	var commits []CommitNode
	iter.ForEach(func(c CommitNode) error {
		commits = append(commits, c)
		return nil
	})

	c.Assert(commits, HasLen, 9)

	expected := []string{
		"b9d69064b190e7aedccf84731ca1d917871f8a1c",
		"6f6c5d2be7852c782be1dd13e36496dd7ad39560",
		"a45273fe2d63300e1962a9e26a6b15c276cd7082",
		"c0edf780dd0da6a65a7a49a86032fcf8a0c2d467",
		"bb13916df33ed23004c3ce9ed3b8487528e655c1",
		"03d2c021ff68954cf3ef0a36825e194a4b98f981",
		"ce275064ad67d51e99f026084e20827901a8361c",
		"e713b52d7e13807e87a002e812041f248db3f643",
		"347c91919944a68e9413581a1bc15519550a3afe",
	}
	for i, commit := range commits {
		c.Assert(commit.ID().String(), Equals, expected[i])
	}
}

func testParents(c *C, nodeIndex CommitNodeIndex) {
	merge3, err := nodeIndex.Get(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	c.Assert(err, IsNil)

	var parents []CommitNode
	merge3.ParentNodes().ForEach(func(c CommitNode) error {
		parents = append(parents, c)
		return nil
	})

	c.Assert(parents, HasLen, 3)

	expected := []string{
		"ce275064ad67d51e99f026084e20827901a8361c",
		"bb13916df33ed23004c3ce9ed3b8487528e655c1",
		"a45273fe2d63300e1962a9e26a6b15c276cd7082",
	}
	for i, parent := range parents {
		c.Assert(parent.ID().String(), Equals, expected[i])
	}
}

func (s *CommitNodeSuite) TestObjectGraph(c *C) {
	f := fixtures.ByTag("commit-graph").One()
	storer := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	p := f.Packfile()
	defer p.Close()
	err := packfile.UpdateObjectStorage(storer, p)
	c.Assert(err, IsNil)

	nodeIndex := NewObjectCommitNodeIndex(storer)
	testWalker(c, nodeIndex)
	testParents(c, nodeIndex)
}

func (s *CommitNodeSuite) TestCommitGraph(c *C) {
	f := fixtures.ByTag("commit-graph").One()
	dotgit := f.DotGit()
	storer := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	reader, err := mmap.Open(path.Join(dotgit.Root(), "objects", "info", "commit-graph"))
	c.Assert(err, IsNil)
	defer reader.Close()
	index, err := commitgraph.OpenFileIndex(reader)
	c.Assert(err, IsNil)

	nodeIndex := NewGraphCommitNodeIndex(index, storer)
	testWalker(c, nodeIndex)
	testParents(c, nodeIndex)
}
