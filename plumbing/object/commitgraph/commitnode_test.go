package commitgraph

import (
	"path"
	"testing"

	"github.com/grahambrooks/go-git/v5/plumbing"
	"github.com/grahambrooks/go-git/v5/plumbing/cache"
	commitgraph "github.com/grahambrooks/go-git/v5/plumbing/format/commitgraph/v2"
	"github.com/grahambrooks/go-git/v5/plumbing/format/packfile"
	"github.com/grahambrooks/go-git/v5/storage/filesystem"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CommitNodeSuite struct {
	fixtures.Suite
}

var _ = Suite(&CommitNodeSuite{})

func unpackRepository(f *fixtures.Fixture) *filesystem.Storage {
	storer := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	p := f.Packfile()
	defer p.Close()
	packfile.UpdateObjectStorage(storer, p)
	return storer
}

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

func testCommitAndTree(c *C, nodeIndex CommitNodeIndex) {
	merge3node, err := nodeIndex.Get(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	c.Assert(err, IsNil)
	merge3commit, err := merge3node.Commit()
	c.Assert(err, IsNil)
	c.Assert(merge3node.ID().String(), Equals, merge3commit.ID().String())
	tree, err := merge3node.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.ID().String(), Equals, merge3commit.TreeHash.String())
}

func (s *CommitNodeSuite) TestObjectGraph(c *C) {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)

	nodeIndex := NewObjectCommitNodeIndex(storer)
	testWalker(c, nodeIndex)
	testParents(c, nodeIndex)
	testCommitAndTree(c, nodeIndex)
}

func (s *CommitNodeSuite) TestCommitGraph(c *C) {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)
	reader, err := storer.Filesystem().Open(path.Join("objects", "info", "commit-graph"))
	c.Assert(err, IsNil)
	defer reader.Close()
	index, err := commitgraph.OpenFileIndex(reader)
	c.Assert(err, IsNil)
	defer index.Close()

	nodeIndex := NewGraphCommitNodeIndex(index, storer)
	testWalker(c, nodeIndex)
	testParents(c, nodeIndex)
	testCommitAndTree(c, nodeIndex)
}

func (s *CommitNodeSuite) TestMixedGraph(c *C) {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)

	// Take the commit-graph file and copy it to memory index without the last commit
	reader, err := storer.Filesystem().Open(path.Join("objects", "info", "commit-graph"))
	c.Assert(err, IsNil)
	defer reader.Close()
	fileIndex, err := commitgraph.OpenFileIndex(reader)
	c.Assert(err, IsNil)
	defer fileIndex.Close()

	memoryIndex := commitgraph.NewMemoryIndex()
	defer memoryIndex.Close()

	for i, hash := range fileIndex.Hashes() {
		if hash.String() != "b9d69064b190e7aedccf84731ca1d917871f8a1c" {
			node, err := fileIndex.GetCommitDataByIndex(uint32(i))
			c.Assert(err, IsNil)
			memoryIndex.Add(hash, node)
		}
	}

	nodeIndex := NewGraphCommitNodeIndex(memoryIndex, storer)
	testWalker(c, nodeIndex)
	testParents(c, nodeIndex)
	testCommitAndTree(c, nodeIndex)
}
