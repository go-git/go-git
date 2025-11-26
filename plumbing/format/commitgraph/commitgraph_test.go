package commitgraph_test

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type CommitgraphSuite struct {
	suite.Suite
}

func TestCommitgraphSuite(t *testing.T) {
	suite.Run(t, new(CommitgraphSuite))
}

func testReadIndex(s *CommitgraphSuite, fs billy.Filesystem, path string) commitgraph.Index {
	reader, err := fs.Open(path)
	s.Require().NoError(err)
	index, err := commitgraph.OpenFileIndex(reader)
	s.Require().NoError(err)
	s.NotNil(index)
	return index
}

func testDecodeHelper(s *CommitgraphSuite, index commitgraph.Index) {
	// Root commit
	nodeIndex, err := index.GetIndexByHash(plumbing.NewHash("347c91919944a68e9413581a1bc15519550a3afe"))
	s.Require().NoError(err)
	commitData, err := index.GetCommitDataByIndex(nodeIndex)
	s.Require().NoError(err)
	s.Len(commitData.ParentIndexes, 0)
	s.Len(commitData.ParentHashes, 0)

	// Regular commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("e713b52d7e13807e87a002e812041f248db3f643"))
	s.Require().NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.Require().NoError(err)
	s.Len(commitData.ParentIndexes, 1)
	s.Len(commitData.ParentHashes, 1)
	s.Equal("347c91919944a68e9413581a1bc15519550a3afe", commitData.ParentHashes[0].String())

	// Merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("b29328491a0682c259bcce28741eac71f3499f7d"))
	s.Require().NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.Require().NoError(err)
	s.Len(commitData.ParentIndexes, 2)
	s.Len(commitData.ParentHashes, 2)
	s.Equal("e713b52d7e13807e87a002e812041f248db3f643", commitData.ParentHashes[0].String())
	s.Equal("03d2c021ff68954cf3ef0a36825e194a4b98f981", commitData.ParentHashes[1].String())

	// Octopus merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	s.Require().NoError(err)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	s.Require().NoError(err)
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

func (s *CommitgraphSuite) TestDecodeMultiChain() {
	for _, f := range fixtures.ByTag("commit-graph-chain-2") {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		storer := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
		p := f.Packfile()
		defer p.Close()

		err = packfile.UpdateObjectStorage(storer, p)
		s.Require().NoError(err)

		for idx, hash := range index.Hashes() {
			idx2, err := index.GetIndexByHash(hash)
			s.Require().NoError(err)
			s.Require().Equal(uint32(idx), idx2)
			hash2, err := index.GetHashByIndex(idx2)
			s.Require().NoError(err)
			s.Equal(hash.String(), hash2.String())

			commitData, err := index.GetCommitDataByIndex(uint32(idx))
			s.Require().NoError(err)
			commit, err := object.GetCommit(storer, hash)
			s.Require().NoError(err)

			for i, parent := range commit.ParentHashes {
				s.Equal(hash.String()+":"+commitData.ParentHashes[i].String(), hash.String()+":"+parent.String())
			}
		}
	}
}

func (s *CommitgraphSuite) TestDecode() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()
		index := testReadIndex(s, dotgit, dotgit.Join("objects", "info", "commit-graph"))
		defer index.Close()
		testDecodeHelper(s, index)
	}
}

func (s *CommitgraphSuite) TestDecodeChain() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		testDecodeHelper(s, index)
	}

	for _, f := range fixtures.ByTag("commit-graph-chain") {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		testDecodeHelper(s, index)
	}
}

func (s *CommitgraphSuite) TestReencode() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		s.Require().NoError(err)
		defer reader.Close()
		index, err := commitgraph.OpenFileIndex(reader)
		s.Require().NoError(err)
		defer index.Close()

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		s.Require().NoError(err)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(index)
		s.Require().NoError(err)
		writer.Close()

		tmpIndex := testReadIndex(s, dotgit, tmpName)
		defer tmpIndex.Close()
		testDecodeHelper(s, tmpIndex)
	}
}

func (s *CommitgraphSuite) TestReencodeInMemory() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		s.Require().NoError(err)
		index, err := commitgraph.OpenFileIndex(reader)
		s.Require().NoError(err)

		memoryIndex := commitgraph.NewMemoryIndex()
		defer memoryIndex.Close()
		for i, hash := range index.Hashes() {
			commitData, err := index.GetCommitDataByIndex(uint32(i))
			s.Require().NoError(err)
			memoryIndex.Add(hash, commitData)
		}
		index.Close()

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		s.Require().NoError(err)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(memoryIndex)
		s.Require().NoError(err)
		writer.Close()

		tmpIndex := testReadIndex(s, dotgit, tmpName)
		defer tmpIndex.Close()
		testDecodeHelper(s, tmpIndex)
	}
}
