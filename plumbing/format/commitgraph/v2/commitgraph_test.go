package v2_test

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	commitgraph "github.com/go-git/go-git/v5/plumbing/format/commitgraph/v2"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CommitgraphSuite struct {
	fixtures.Suite
}

var _ = Suite(&CommitgraphSuite{})

func testReadIndex(c *C, fs billy.Filesystem, path string) commitgraph.Index {
	reader, err := fs.Open(path)
	c.Assert(err, IsNil)
	index, err := commitgraph.OpenFileIndex(reader)
	c.Assert(err, IsNil)
	c.Assert(index, NotNil)
	return index
}

func testDecodeHelper(c *C, index commitgraph.Index) {
	// Root commit
	nodeIndex, err := index.GetIndexByHash(plumbing.NewHash("347c91919944a68e9413581a1bc15519550a3afe"))
	c.Assert(err, IsNil)
	commitData, err := index.GetCommitDataByIndex(nodeIndex)
	c.Assert(err, IsNil)
	c.Assert(len(commitData.ParentIndexes), Equals, 0)
	c.Assert(len(commitData.ParentHashes), Equals, 0)

	// Regular commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("e713b52d7e13807e87a002e812041f248db3f643"))
	c.Assert(err, IsNil)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	c.Assert(err, IsNil)
	c.Assert(len(commitData.ParentIndexes), Equals, 1)
	c.Assert(len(commitData.ParentHashes), Equals, 1)
	c.Assert(commitData.ParentHashes[0].String(), Equals, "347c91919944a68e9413581a1bc15519550a3afe")

	// Merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("b29328491a0682c259bcce28741eac71f3499f7d"))
	c.Assert(err, IsNil)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	c.Assert(err, IsNil)
	c.Assert(len(commitData.ParentIndexes), Equals, 2)
	c.Assert(len(commitData.ParentHashes), Equals, 2)
	c.Assert(commitData.ParentHashes[0].String(), Equals, "e713b52d7e13807e87a002e812041f248db3f643")
	c.Assert(commitData.ParentHashes[1].String(), Equals, "03d2c021ff68954cf3ef0a36825e194a4b98f981")

	// Octopus merge commit
	nodeIndex, err = index.GetIndexByHash(plumbing.NewHash("6f6c5d2be7852c782be1dd13e36496dd7ad39560"))
	c.Assert(err, IsNil)
	commitData, err = index.GetCommitDataByIndex(nodeIndex)
	c.Assert(err, IsNil)
	c.Assert(len(commitData.ParentIndexes), Equals, 3)
	c.Assert(len(commitData.ParentHashes), Equals, 3)
	c.Assert(commitData.ParentHashes[0].String(), Equals, "ce275064ad67d51e99f026084e20827901a8361c")
	c.Assert(commitData.ParentHashes[1].String(), Equals, "bb13916df33ed23004c3ce9ed3b8487528e655c1")
	c.Assert(commitData.ParentHashes[2].String(), Equals, "a45273fe2d63300e1962a9e26a6b15c276cd7082")

	// Check all hashes
	hashes := index.Hashes()
	c.Assert(len(hashes), Equals, 11)
	c.Assert(hashes[0].String(), Equals, "03d2c021ff68954cf3ef0a36825e194a4b98f981")
	c.Assert(hashes[10].String(), Equals, "e713b52d7e13807e87a002e812041f248db3f643")
}

func (s *CommitgraphSuite) TestDecodeMultiChain(c *C) {
	fixtures.ByTag("commit-graph-chain-2").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		c.Assert(err, IsNil)
		defer index.Close()
		storer := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
		p := f.Packfile()
		defer p.Close()
		packfile.UpdateObjectStorage(storer, p)

		for idx, hash := range index.Hashes() {
			idx2, err := index.GetIndexByHash(hash)
			c.Assert(err, IsNil)
			c.Assert(idx2, Equals, uint32(idx))
			hash2, err := index.GetHashByIndex(idx2)
			c.Assert(err, IsNil)
			c.Assert(hash2.String(), Equals, hash.String())

			commitData, err := index.GetCommitDataByIndex(uint32(idx))
			c.Assert(err, IsNil)
			commit, err := object.GetCommit(storer, hash)
			c.Assert(err, IsNil)

			for i, parent := range commit.ParentHashes {
				c.Assert(hash.String()+":"+parent.String(), Equals, hash.String()+":"+commitData.ParentHashes[i].String())
			}
		}
	})
}

func (s *CommitgraphSuite) TestDecode(c *C) {
	fixtures.ByTag("commit-graph").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()
		index := testReadIndex(c, dotgit, dotgit.Join("objects", "info", "commit-graph"))
		defer index.Close()
		testDecodeHelper(c, index)
	})
}

func (s *CommitgraphSuite) TestDecodeChain(c *C) {
	fixtures.ByTag("commit-graph").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		c.Assert(err, IsNil)
		defer index.Close()
		testDecodeHelper(c, index)
	})

	fixtures.ByTag("commit-graph-chain").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		c.Assert(err, IsNil)
		defer index.Close()
		testDecodeHelper(c, index)
	})
}

func (s *CommitgraphSuite) TestReencode(c *C) {
	fixtures.ByTag("commit-graph").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		c.Assert(err, IsNil)
		defer reader.Close()
		index, err := commitgraph.OpenFileIndex(reader)
		c.Assert(err, IsNil)
		defer index.Close()

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		c.Assert(err, IsNil)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(index)
		c.Assert(err, IsNil)
		writer.Close()

		tmpIndex := testReadIndex(c, dotgit, tmpName)
		defer tmpIndex.Close()
		testDecodeHelper(c, tmpIndex)
	})
}

func (s *CommitgraphSuite) TestReencodeInMemory(c *C) {
	fixtures.ByTag("commit-graph").Test(c, func(f *fixtures.Fixture) {
		dotgit := f.DotGit()

		reader, err := dotgit.Open(dotgit.Join("objects", "info", "commit-graph"))
		c.Assert(err, IsNil)
		index, err := commitgraph.OpenFileIndex(reader)
		c.Assert(err, IsNil)

		memoryIndex := commitgraph.NewMemoryIndex()
		defer memoryIndex.Close()
		for i, hash := range index.Hashes() {
			commitData, err := index.GetCommitDataByIndex(uint32(i))
			c.Assert(err, IsNil)
			memoryIndex.Add(hash, commitData)
		}
		index.Close()

		writer, err := util.TempFile(dotgit, "", "commit-graph")
		c.Assert(err, IsNil)
		tmpName := writer.Name()
		defer os.Remove(tmpName)

		encoder := commitgraph.NewEncoder(writer)
		err = encoder.Encode(memoryIndex)
		c.Assert(err, IsNil)
		writer.Close()

		tmpIndex := testReadIndex(c, dotgit, tmpName)
		defer tmpIndex.Close()
		testDecodeHelper(c, tmpIndex)
	})
}
