package commitgraph

import (
	"path"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

type CommitNodeSuite struct {
	suite.Suite
}

func TestCommitNodeSuite(t *testing.T) {
	suite.Run(t, new(CommitNodeSuite))
}

func unpackRepository(f *fixtures.Fixture) *filesystem.Storage {
	storer := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	p := f.Packfile()
	defer p.Close()
	packfile.UpdateObjectStorage(storer, p)
	return storer
}

func testWalker(s *CommitNodeSuite, nodeIndex CommitNodeIndex) {
	head, err := nodeIndex.Get(plumbing.NewHash("b9d69064b190e7aedccf84731ca1d917871f8a1c"))
	s.NoError(err)

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

	s.Len(commits, 9)

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
		s.Equal(expected[i], commit.ID().String())
	}
}

func testParents(s *CommitNodeSuite, nodeIndex CommitNodeIndex) {
	merge3, err := nodeIndex.Get(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	s.NoError(err)

	var parents []CommitNode
	merge3.ParentNodes().ForEach(func(c CommitNode) error {
		parents = append(parents, c)
		return nil
	})

	s.Len(parents, 3)

	expected := []string{
		"ce275064ad67d51e99f026084e20827901a8361c",
		"bb13916df33ed23004c3ce9ed3b8487528e655c1",
		"a45273fe2d63300e1962a9e26a6b15c276cd7082",
	}
	for i, parent := range parents {
		s.Equal(expected[i], parent.ID().String())
	}
}

func testCommitAndTree(s *CommitNodeSuite, nodeIndex CommitNodeIndex) {
	merge3node, err := nodeIndex.Get(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	s.NoError(err)
	merge3commit, err := merge3node.Commit()
	s.NoError(err)
	s.Equal(merge3commit.ID().String(), merge3node.ID().String())
	tree, err := merge3node.Tree()
	s.NoError(err)
	s.Equal(merge3commit.TreeHash.String(), tree.ID().String())
}

func (s *CommitNodeSuite) TestObjectGraph() {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)

	nodeIndex := NewObjectCommitNodeIndex(storer)
	testWalker(s, nodeIndex)
	testParents(s, nodeIndex)
	testCommitAndTree(s, nodeIndex)
}

func (s *CommitNodeSuite) TestCommitGraph() {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)
	reader, err := storer.Filesystem().Open(path.Join("objects", "info", "commit-graph"))
	s.NoError(err)
	defer reader.Close()
	index, err := commitgraph.OpenFileIndex(reader)
	s.NoError(err)
	defer index.Close()

	nodeIndex := NewGraphCommitNodeIndex(index, storer)
	testWalker(s, nodeIndex)
	testParents(s, nodeIndex)
	testCommitAndTree(s, nodeIndex)
}

func (s *CommitNodeSuite) TestMixedGraph() {
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)

	// Take the commit-graph file and copy it to memory index without the last commit
	reader, err := storer.Filesystem().Open(path.Join("objects", "info", "commit-graph"))
	s.NoError(err)
	defer reader.Close()
	fileIndex, err := commitgraph.OpenFileIndex(reader)
	s.NoError(err)
	defer fileIndex.Close()

	memoryIndex := commitgraph.NewMemoryIndex()
	defer memoryIndex.Close()

	for i, hash := range fileIndex.Hashes() {
		if hash.String() != "b9d69064b190e7aedccf84731ca1d917871f8a1c" {
			node, err := fileIndex.GetCommitDataByIndex(uint32(i))
			s.NoError(err)
			memoryIndex.Add(hash, node)
		}
	}

	nodeIndex := NewGraphCommitNodeIndex(memoryIndex, storer)
	testWalker(s, nodeIndex)
	testParents(s, nodeIndex)
	testCommitAndTree(s, nodeIndex)
}
