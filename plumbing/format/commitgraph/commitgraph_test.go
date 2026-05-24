package commitgraph_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	commitgraph "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/binary"
)

type CommitgraphSuite struct {
	suite.Suite
}

func TestCommitgraphSuite(t *testing.T) {
	t.Parallel()
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
		dotgit, err := f.DotGit()
		s.Require().NoError(err)
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		dotgit2, err := f.DotGit()
		s.Require().NoError(err)
		storer := filesystem.NewStorage(dotgit2, cache.NewObjectLRUDefault())
		defer func() { _ = storer.Close() }()
		p, err := f.Packfile()
		s.Require().NoError(err)
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
		dotgit, err := f.DotGit()
		s.Require().NoError(err)
		index := testReadIndex(s, dotgit, dotgit.Join("objects", "info", "commit-graph"))
		defer index.Close()
		testDecodeHelper(s, index)
	}
}

func (s *CommitgraphSuite) TestDecodeChain() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		testDecodeHelper(s, index)
	}

	for _, f := range fixtures.ByTag("commit-graph-chain") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)
		index, err := commitgraph.OpenChainOrFileIndex(dotgit)
		s.Require().NoError(err)
		defer index.Close()
		testDecodeHelper(s, index)
	}
}

func (s *CommitgraphSuite) TestReencode() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)

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

type discardCloseReader struct {
	io.ReaderAt
}

func (discardCloseReader) Close() error { return nil }

func (r discardCloseReader) Seek(offset int64, whence int) (int64, error) {
	if s, ok := r.ReaderAt.(io.Seeker); ok {
		return s.Seek(offset, whence)
	}
	return 0, errors.New("commitgraph test: inner reader does not support Seek")
}

func openIndexBytes(data []byte) (commitgraph.Index, error) {
	return commitgraph.OpenFileIndex(discardCloseReader{bytes.NewReader(data)})
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsChunkCountMismatch() {
	// File header declares 2 chunks, but the chunk table-of-contents
	// supplies five non-zero entries with no terminating zero entry.
	// Iteration that ignores the declared count would walk past the
	// declared table; iteration bounded by the declared count must
	// reject the trailing non-zero entry.
	chunks := []commitgraph.ChunkType{
		commitgraph.OIDFanoutChunk,
		commitgraph.OIDLookupChunk,
		commitgraph.CommitDataChunk,
		commitgraph.GenerationDataChunk,
		commitgraph.ExtraEdgeListChunk,
	}

	var buf bytes.Buffer
	buf.WriteString("CGPH") // signature
	buf.WriteByte(1)        // header version
	buf.WriteByte(1)        // hash version (SHA-1)
	buf.WriteByte(byte(2))  // declared num_chunks
	buf.WriteByte(0)        // base graphs
	offset := uint64(8 + len(chunks)*12)
	for _, c := range chunks {
		buf.Write(c.Signature())
		s.Require().NoError(binary.WriteUint64(&buf, offset))
		offset += 16
	}
	// Pad to the minimum size verifyFileSize requires for num_chunks=2:
	//   8 (header) + (2+1)*12 (toc) + 1024 (fanout) + 20 (SHA-1 hash) = 1088
	// Without this padding, verifyFileSize rejects the buffer before
	// readChunkHeaders can exercise the chunk-count mismatch logic.
	const minSize = 8 + (2+1)*12 + 1024 + 20
	if buf.Len() < minSize {
		buf.Write(make([]byte, minSize-buf.Len()))
	}

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsTruncatedFile() {
	// Header declares 3 chunks. Real Git's parse_commit_graph_v1 requires
	// the file to be at least
	//     8 (header) + (num_chunks+1)*12 (toc) + 1024 (fanout) + hashsz
	// bytes long. We supply only the header and chunk table, no fanout,
	// no trailing hash; the parser must reject before touching chunk data.
	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1) // header version
	buf.WriteByte(1) // SHA-1
	buf.WriteByte(3) // num_chunks
	buf.WriteByte(0) // base graphs
	// Three valid chunk entries plus a zero terminator (so the existing
	// mandatory-chunk check passes; only the size precheck should catch this).
	for _, c := range []commitgraph.ChunkType{
		commitgraph.OIDFanoutChunk,
		commitgraph.OIDLookupChunk,
		commitgraph.CommitDataChunk,
	} {
		buf.Write(c.Signature())
		s.Require().NoError(binary.WriteUint64(&buf, uint64(8+4*12)))
	}
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, uint64(8+4*12)))

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsChunkOffsetPastEOF() {
	// Header declares 3 chunks; all three entries plus a zero terminator
	// are present and all mandatory chunks (OIDF, OIDL, CDAT) appear, so
	// the mandatory-chunk check cannot fire first. The second chunk's
	// offset (OIDL) points past file_size - hash_size. Canonical Git's
	// read_table_of_contents rejects such offsets; go-git must follow
	// suit and return ErrMalformedCommitGraphFile from readChunkHeaders.
	//
	// fileSize = 8 (header) + 4*12 (toc: 3 chunks + terminator) + 1024 (fanout) + 20 (SHA-1)
	const fileSize = 8 + 4*12 + 1024 + 20 // = 1100

	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1)
	buf.WriteByte(1)
	buf.WriteByte(3) // declare 3 chunks
	buf.WriteByte(0)

	// OIDFanout at a valid offset right after the toc.
	validOffset := uint64(8 + 4*12)
	buf.Write(commitgraph.OIDFanoutChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, validOffset))
	// OIDLookup offset is past EOF (> fileSize - hash_size = 1080).
	buf.Write(commitgraph.OIDLookupChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, uint64(fileSize+1024)))
	// CommitData at a valid offset; without it, the post-loop
	// mandatory-chunk check would reject the file regardless of the
	// new offset validation, so this test would not be attributable
	// to the offset-past-EOF guard.
	buf.Write(commitgraph.CommitDataChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, validOffset+1024))
	// Zero terminator.
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, uint64(fileSize)))

	// Pad to declared file size so verifyFileSize passes.
	buf.Write(make([]byte, fileSize-buf.Len()))

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsDuplicateChunkID() {
	// Declare 4 chunks: OIDF, OIDL, CDAT, then a second OIDF.
	// Canonical Git's read_table_of_contents (chunk-format.c v2.54.0[1])
	// iterates through previously-stored chunk ids and returns -1 on the
	// first duplicate; go-git must follow suit.
	//
	// Without the duplicate-detection guard the second OIDF entry silently
	// overwrites fi.offsets[OIDFanoutChunk]. All three required chunks end
	// up with non-zero offsets, readFanout reads 256 zero uint32s from the
	// overwritten offset, and OpenFileIndex returns (index, nil) — masking
	// the corruption.
	//
	// [1]: https://github.com/git/git/blob/v2.54.0/chunk-format.c#L143-L149
	//
	// Layout (num_chunks=4):
	//   header:     8 bytes
	//   TOC:        5×12 = 60 bytes  (4 chunks + zero terminator)
	//   OIDF data:  1024 bytes        (fanout table, all zeros)
	//   padding:    1024 bytes        (so OIDF-dup offset is within bounds)
	//   SHA-1 hash: 20 bytes
	const (
		fileSize = 8 + 5*12 + 2*1024 + 20 // = 2136
		tocBase  = 8
		tocEnd   = tocBase + 5*12 // = 68
	)

	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1) // version
	buf.WriteByte(1) // SHA-1
	buf.WriteByte(4) // num_chunks
	buf.WriteByte(0) // base graphs

	oidfOffset := uint64(tocEnd)
	oidlOffset := oidfOffset + 1024
	cdatOffset := oidlOffset
	dupOffset := oidlOffset // monotonic; same as cdat
	termOffset := uint64(fileSize - 20)

	buf.Write(commitgraph.OIDFanoutChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, oidfOffset))
	buf.Write(commitgraph.OIDLookupChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, oidlOffset))
	buf.Write(commitgraph.CommitDataChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, cdatOffset))
	buf.Write(commitgraph.OIDFanoutChunk.Signature()) // duplicate
	s.Require().NoError(binary.WriteUint64(&buf, dupOffset))
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, termOffset))

	// Pad to declared file size so verifyFileSize passes.
	buf.Write(make([]byte, fileSize-buf.Len()))

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsZeroChunkCount() {
	// Header declares zero chunks. The terminator entry sits right at
	// the start of the table-of-contents. Even with a valid terminator
	// the required-chunks check fails because OIDFanout, OIDLookup,
	// and CommitData offsets are all zero.
	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1)
	buf.WriteByte(1)
	buf.WriteByte(0) // num_chunks
	buf.WriteByte(0)
	const fileSize = 8 + 12 + 1024 + 20
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, fileSize))

	buf.Write(make([]byte, fileSize-buf.Len()))

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestOpenFileIndexRejectsEarlyZeroChunk() {
	// num_chunks=2, but the second entry is the zero terminator. Canonical
	// reports "terminating chunk id appears earlier than expected"
	// (chunk-format.c v2.54.0); go-git must reject likewise.
	const fileSize = 8 + 3*12 + 1024 + 20

	var buf bytes.Buffer
	buf.WriteString("CGPH")
	buf.WriteByte(1)
	buf.WriteByte(1)
	buf.WriteByte(2)
	buf.WriteByte(0)

	buf.Write(commitgraph.OIDFanoutChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, 8+3*12))
	// Early ZeroChunk inside the declared count. Its offset is strictly
	// greater than the previous one so the monotonicity guard cannot
	// fire; only the in-loop ZeroChunk guard can produce the rejection.
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, 8+3*12+1))
	buf.Write(commitgraph.ZeroChunk.Signature())
	s.Require().NoError(binary.WriteUint64(&buf, fileSize))

	buf.Write(make([]byte, fileSize-buf.Len()))

	_, err := openIndexBytes(buf.Bytes())
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile)
}

func (s *CommitgraphSuite) TestReencodeInMemory() {
	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)

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
