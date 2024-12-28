package commitgraph_test

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/commitgraph"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type CommitgraphFixtureSuite struct {
	fixtures.Suite
}

type CommitgraphSuite struct {
	suite.Suite
	CommitgraphFixtureSuite
}

func TestCommitgraphSuite(t *testing.T) {
	suite.Run(t, new(CommitgraphSuite))
}

func testDecodeHelper(s *CommitgraphSuite, fs billy.Filesystem, path string) {
	reader, err := fs.Open(path)
	s.NoError(err)
	defer reader.Close()
	index, err := commitgraph.OpenFileIndex(reader)
	s.NoError(err)

	// Root commit
	nodeIndex, err := index.GetIndexByHash(plumbing.NewHash("347c91919944a68e9413581a1bc15519550a3afe"))
	s.NoError(err)
	commitData, err := index.GetCommitDataByIndex(nodeIndex)
	s.NoError(err)
	s.Len(commitData.ParentIndexes, 0)
	s.Len(commitData.ParentHashes, 0)

	// Regular commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("e713b52d7e13807e87a002e812041f248db3f643"))
	s.NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.NoError(err)
	s.Len(commitData.ParentIndexes, 1)
	s.Len(commitData.ParentHashes, 1)
	s.Equal("347c91919944a68e9413581a1bc15519550a3afe", commitData.ParentHashes[0].String())

	// Merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("b29328491a0682c259bcce28741eac71f3499f7d"))
	s.NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.NoError(err)
	s.Len(commitData.ParentIndexes, 2)
	s.Len(commitData.ParentHashes, 2)
	s.Equal("e713b52d7e13807e87a002e812041f248db3f643", commitData.ParentHashes[0].String())
	s.Equal("03d2c021ff68954cf3ef0a36825e194a4b98f981", commitData.ParentHashes[1].String())

	// Octopus merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	s.NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.NoError(err)
	s.Len(commitData.ParentIndexes, 3)
	s.Len(commitData.ParentHashes, 3)
	s.Equal("ce275064ad67d51e99f026084e20827901a8361c", commitData.ParentHashes[0].String())
	s.Equal("bb13916df33ed23004c3ce9ed3b8487528e655c1", commitData.ParentHashes[1].String())
	s.Equal("a45273fe2d63300e1962a9e26a6b15c276cd7082", commitData.ParentHashes[2].String())

	// Check all hashes
	hashes := index.Hashes()
	s.Len(hashes, 11)
	s.Equal("03d2c021ff68954cf3ef0a36825e194a4b98f981", hashes[0].String())
	s.Equal("e713b52d7e13807e87a002e812041f248db3f643", hashes[10].String())
}

func (s *CommitgraphSuite) TestDecode() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()
		testDecodeHelper(s, dotgit, dotgit.Join("objects", "info", "commit-graph"))
	}
}

func (s *CommitgraphSuite) TestReencode() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		s.NoError(err)
		defer reader.Close()
		index, err := commitgraph.OpenFileIndex(reader)
		s.NoError(err)

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		s.NoError(err)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(index)
		s.NoError(err)
		writer.Close()

		testDecodeHelper(s, dotgit, tmpName)
	}
}

func (s *CommitgraphSuite) TestReencodeInMemory() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		s.NoError(err)
		index, err := commitgraph.OpenFileIndex(reader)
		s.NoError(err)
		memoryIndex := commitgraph.NewMemoryIndex()
		for i, hash := range index.Hashes() {
			commitData, err := index.GetCommitDataByIndex(i)
			s.NoError(err)
			memoryIndex.Add(hash, commitData)
		}
		reader.Close()

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		s.NoError(err)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(memoryIndex)
		s.NoError(err)
		writer.Close()

		testDecodeHelper(s, dotgit, tmpName)
	}
}
