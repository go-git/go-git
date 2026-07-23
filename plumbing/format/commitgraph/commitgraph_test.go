package commitgraph_test

import (
	"bytes"
	encbin "encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"

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

		err = packfile.UpdateObjectStorage(s.T().Context(), storer, p)
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
			commit, err := object.GetCommit(s.T().Context(), storer, hash)
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

// TestGetCommitDataRejectsExtraEdgesPastChunk verifies that
// GetCommitDataByIndex refuses an octopus parent2 whose extra-edge index
// falls outside the declared EDGE chunk. Canonical Git's
// fill_commit_in_graph validates parent_data_pos against
// chunk_extra_edges_size / sizeof(uint32_t) (commit-graph.c v2.54.0);
// go-git must follow suit and return ErrMalformedCommitGraphFile.
//
// The test loads the real "commit-graph" fixture (which contains an octopus
// merge at 6f6c5d2b with two EDGE entries), reads its raw bytes, and patches
// the octopus commit's parent2 field to encode an index of 100 — far past
// the two-entry EDGE chunk. The patched file is re-opened and the octopus
// commit looked up; the bound guard must fire before any read past the chunk.
func (s *CommitgraphSuite) TestGetCommitDataRejectsExtraEdgesPastChunk() {
	const octopusHash = "6f6c5d2be7852c782be1dd13e36496dd7ad39560"
	// parentOctopusUsed (bit 31) | out-of-bounds edge index 100.
	const badParent2 = uint32(0x80000000 | 100)

	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)

		// Read the raw commit-graph bytes.
		cgPath := dotgit.Join("objects", "info", "commit-graph")
		rdr, err := dotgit.Open(cgPath)
		s.Require().NoError(err)
		sz, err := rdr.Seek(0, io.SeekEnd)
		s.Require().NoError(err)
		_, err = rdr.Seek(0, io.SeekStart)
		s.Require().NoError(err)
		raw := make([]byte, sz)
		_, err = io.ReadFull(rdr, raw)
		s.Require().NoError(err)
		rdr.Close()

		// Locate the octopus commit's index in the OID-lookup chunk so we
		// can compute the byte offset of its parent2 field in the CDAT chunk.
		//
		// File layout (SHA-1, from the fixture's TOC):
		//   header:  8 bytes
		//   TOC:     (numChunks+1) * 12 bytes
		//   OIDF chunk at tocEntry[0].offset
		//   OIDL chunk at tocEntry[1].offset
		//   CDAT chunk at tocEntry[2].offset
		//   EDGE chunk at tocEntry[3].offset
		//   terminator at tocEntry[4].offset  (= end of chunk data)
		numChunks := int(raw[6])
		const tocBase = 8
		const tocEntrySize = 12
		const sha1Size = 20

		readU64 := func(off int) uint64 {
			return encbin.BigEndian.Uint64(raw[off:])
		}

		var oidlOffset, cdatOffset int
		for i := range numChunks {
			base := tocBase + i*tocEntrySize
			id := string(raw[base : base+4])
			off := int(readU64(base + 4))
			switch id {
			case "OIDL":
				oidlOffset = off
			case "CDAT":
				cdatOffset = off
			}
		}
		s.Require().Greater(oidlOffset, 0, "OIDL chunk not found in fixture")
		s.Require().Greater(cdatOffset, 0, "CDAT chunk not found in fixture")

		// Find the octopus commit's position in the sorted OID-lookup table.
		// fanout[255] sits in the four bytes immediately preceding OIDL,
		// regardless of how many other chunks precede the fanout.
		octHash := plumbing.NewHash(octopusHash)
		fanout255 := int(encbin.BigEndian.Uint32(raw[oidlOffset-4:]))
		octIdx := -1
		for i := range fanout255 {
			start := oidlOffset + i*sha1Size
			if bytes.Equal(raw[start:start+sha1Size], octHash.Bytes()) {
				octIdx = i
				break
			}
		}
		s.Require().GreaterOrEqual(octIdx, 0, "octopus commit not found in OIDL")

		// CDAT entry layout: sha1Size tree-hash + 4 parent1 + 4 parent2 + 8 genAndTime.
		const cdatEntrySize = sha1Size + 4 + 4 + 8
		parent2Off := cdatOffset + octIdx*cdatEntrySize + sha1Size + 4

		// Clone and patch.
		patched := make([]byte, len(raw))
		copy(patched, raw)
		encbin.BigEndian.PutUint32(patched[parent2Off:], badParent2)

		// Opening the patched file should succeed: the corrupt field is inside
		// the CDAT chunk and is not read during header/fanout parsing.
		idx, err := openIndexBytes(patched)
		s.Require().NoError(err)

		// GetCommitDataByIndex for the octopus commit must reject the
		// out-of-bounds edge pointer before reading past the EDGE chunk.
		nodeIdx, err := idx.GetIndexByHash(octHash)
		s.Require().NoError(err)
		_, err = idx.GetCommitDataByIndex(nodeIdx)
		s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
			"expected ErrMalformedCommitGraphFile for out-of-bounds edge index")
		idx.Close()
	}
}

// TestGetCommitDataAcceptsValidOctopusParents complements the negative
// extra-edge test by exercising the happy path through the same bound:
// loading the unmodified fixture and reading the octopus commit must
// still return all three parent hashes. Without this, an off-by-one
// regression in the EDGE chunk cap would silently shrink legitimate
// octopus walks.
func (s *CommitgraphSuite) TestGetCommitDataAcceptsValidOctopusParents() {
	const octopusHash = "6f6c5d2be7852c782be1dd13e36496dd7ad39560"

	for _, f := range fixtures.ByTag("commit-graph") {
		dotgit, err := f.DotGit()
		s.Require().NoError(err)

		cgPath := dotgit.Join("objects", "info", "commit-graph")
		rdr, err := dotgit.Open(cgPath)
		s.Require().NoError(err)
		idx, err := commitgraph.OpenFileIndex(rdr)
		s.Require().NoError(err)

		octHash := plumbing.NewHash(octopusHash)
		nodeIdx, err := idx.GetIndexByHash(octHash)
		s.Require().NoError(err)

		data, err := idx.GetCommitDataByIndex(nodeIdx)
		s.Require().NoError(err)
		s.Require().Greater(len(data.ParentHashes), 2,
			"fixture's octopus commit should expose more than two parents")
		s.Equal(len(data.ParentHashes), len(data.ParentIndexes))

		idx.Close()
	}
}

// TestGetCommitDataRejectsGenerationOverflowPastChunk verifies that
// GetCommitDataByIndex refuses a GDA2 overflow pointer whose index
// falls outside the declared GDO2 chunk. Canonical Git's
// fill_commit_graph_info validates the offset_pos against
// chunk_generation_data_overflow_size / sizeof(uint64_t)
// (commit-graph.c v2.54.0); go-git must follow suit and return
// ErrMalformedCommitGraphFile rather than reading past the chunk.
func (s *CommitgraphSuite) TestGetCommitDataRejectsGenerationOverflowPastChunk() {
	// Build a one-commit graph whose generation value forces overflow
	// encoding (GenerationV2Data must exceed math.MaxUint32 to trip
	// Encoder.prepare and >= 0x80000000 to trip the GDA2 overflow
	// branch). Encode it, then surgically rewrite the GDA2 entry to
	// point past the single-entry GDO2 chunk.
	mem := commitgraph.NewMemoryIndex()
	mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		&commitgraph.CommitData{
			TreeHash:     plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Generation:   1,
			GenerationV2: 0x100000001, // > MaxUint32 and well above 0x80000000
		})

	var buf bytes.Buffer
	s.Require().NoError(commitgraph.NewEncoder(&buf).Encode(mem))
	raw := buf.Bytes()

	// Locate GDA2 chunk via the TOC. numChunks is the uint8 at byte 6;
	// every TOC entry is a 4-byte signature + uint64 offset.
	numChunks := int(raw[6])
	const tocBase = 8
	const tocEntrySize = 12
	gda2Offset := 0
	for i := range numChunks {
		base := tocBase + i*tocEntrySize
		if string(raw[base:base+4]) == "GDA2" {
			gda2Offset = int(encbin.BigEndian.Uint64(raw[base+4:]))
			break
		}
	}
	s.Require().Greater(gda2Offset, 0, "GDA2 chunk not present in encoded graph")

	// Rewrite the first GDA2 entry to mark overflow at the largest
	// representable index, well past the one-entry GDO2 chunk.
	encbin.BigEndian.PutUint32(raw[gda2Offset:], 0x80000000|0x7FFFFFFF)

	idx, err := openIndexBytes(raw)
	s.Require().NoError(err)
	defer idx.Close()

	_, err = idx.GetCommitDataByIndex(0)
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
		"expected ErrMalformedCommitGraphFile for out-of-bounds overflow index")
}

// TestGetCommitDataReadsGenerationOverflow complements the rejection
// test by exercising the happy path through the same bound: a graph
// whose commit's GenerationV2Data exceeds MaxUint32 must round-trip
// the overflow value through the GDO2 chunk. Without this, an
// off-by-one tightening of the overflow cap would silently shrink
// legitimate overflow reads.
func (s *CommitgraphSuite) TestGetCommitDataReadsGenerationOverflow() {
	// Use Unix epoch as the commit time so the encoder's
	// (generation<<34 | unixTime) packing leaves the lower 34 bits at
	// zero; the reader's `generationV2 = uint64(genAndTime & 0x3FFFFFFFF)`
	// then equals When.Unix(), and `+= overflow_value` returns the
	// caller-set GenerationV2 unchanged.
	want := uint64(0x100000001)
	mem := commitgraph.NewMemoryIndex()
	mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		&commitgraph.CommitData{
			TreeHash:     plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Generation:   1,
			GenerationV2: want,
			When:         time.Unix(0, 0),
		})

	var buf bytes.Buffer
	s.Require().NoError(commitgraph.NewEncoder(&buf).Encode(mem))

	idx, err := commitgraph.OpenFileIndex(
		discardCloseReader{bytes.NewReader(buf.Bytes())})
	s.Require().NoError(err)
	defer idx.Close()

	data, err := idx.GetCommitDataByIndex(0)
	s.Require().NoError(err)
	s.Equal(want, data.GenerationV2,
		"GenerationV2 should round-trip through the GDO2 overflow chunk")
}

// patchTOCOffset rewrites the file offset of the TOC entry whose
// 4-byte signature matches sig. Returns false if no entry matches.
func patchTOCOffset(raw, sig []byte, newOffset uint64) bool {
	if len(raw) < 8 || len(sig) != 4 {
		return false
	}
	numChunks := int(raw[6])
	const tocBase = 8
	const tocEntrySize = 12
	for i := 0; i <= numChunks; i++ {
		base := tocBase + i*tocEntrySize
		if base+12 > len(raw) {
			return false
		}
		if bytes.Equal(raw[base:base+4], sig) {
			encbin.BigEndian.PutUint64(raw[base+4:], newOffset)
			return true
		}
	}
	return false
}

// findTOCOffset reads back the file offset of the TOC entry with the
// given 4-byte signature.
func findTOCOffset(raw, sig []byte) (uint64, bool) {
	if len(raw) < 8 || len(sig) != 4 {
		return 0, false
	}
	numChunks := int(raw[6])
	const tocBase = 8
	const tocEntrySize = 12
	for i := 0; i <= numChunks; i++ {
		base := tocBase + i*tocEntrySize
		if base+12 > len(raw) {
			return 0, false
		}
		if bytes.Equal(raw[base:base+4], sig) {
			return encbin.BigEndian.Uint64(raw[base+4:]), true
		}
	}
	return 0, false
}

// buildSimpleEncoded encodes a one-commit graph with no octopus and
// no generation-v2 data, exercising only OIDF/OIDL/CDAT chunks.
func buildSimpleEncoded() ([]byte, error) {
	mem := commitgraph.NewMemoryIndex()
	mem.Add(plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		&commitgraph.CommitData{
			TreeHash:   plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Generation: 1,
		})
	var buf bytes.Buffer
	if err := commitgraph.NewEncoder(&buf).Encode(mem); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TestOpenFileIndexRejectsFanoutWrongSize verifies the OIDFanout
// chunk's byte length is asserted to be exactly 256 * uint32.
// Canonical Git: graph_read_oid_fanout, commit-graph.c v2.54.0.
func (s *CommitgraphSuite) TestOpenFileIndexRejectsFanoutWrongSize() {
	raw, err := buildSimpleEncoded()
	s.Require().NoError(err)

	// Shift OIDL's TOC offset up by 4 bytes; OIDF's derived size becomes
	// 1028, not 1024.
	oidlOff, ok := findTOCOffset(raw, []byte("OIDL"))
	s.Require().True(ok, "OIDL TOC entry not present")
	s.Require().True(patchTOCOffset(raw, []byte("OIDL"), oidlOff+4))

	_, err = openIndexBytes(raw)
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
		"expected ErrMalformedCommitGraphFile for OIDF size != 1024")
}

// TestOpenFileIndexRejectsLookupCardinality verifies the OIDLookup
// chunk's byte length is asserted to equal numCommits * hashSize.
// Canonical Git: graph_read_oid_lookup, commit-graph.c v2.54.0.
func (s *CommitgraphSuite) TestOpenFileIndexRejectsLookupCardinality() {
	raw, err := buildSimpleEncoded()
	s.Require().NoError(err)

	// Shift CDAT's TOC offset up by one hash size; OIDL's derived size
	// becomes numCommits*hashSize + hashSize, no longer a multiple match.
	cdatOff, ok := findTOCOffset(raw, []byte("CDAT"))
	s.Require().True(ok, "CDAT TOC entry not present")
	s.Require().True(patchTOCOffset(raw, []byte("CDAT"), cdatOff+20))

	_, err = openIndexBytes(raw)
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
		"expected ErrMalformedCommitGraphFile for OIDL cardinality mismatch")
}

// TestOpenFileIndexRejectsCommitDataCardinality verifies the
// CommitData chunk's byte length is asserted to equal
// numCommits * (hashSize + szCommitData).
// Canonical Git: graph_read_commit_data, commit-graph.c v2.54.0.
func (s *CommitgraphSuite) TestOpenFileIndexRejectsCommitDataCardinality() {
	raw, err := buildSimpleEncoded()
	s.Require().NoError(err)

	// Shift the terminator's offset up by one CDAT-entry stride; CDAT's
	// derived size becomes numCommits*entrySize + entrySize.
	termOff, ok := findTOCOffset(raw, []byte{0, 0, 0, 0})
	s.Require().True(ok, "terminator TOC entry not present")
	s.Require().True(patchTOCOffset(raw, []byte{0, 0, 0, 0}, termOff+36))

	_, err = openIndexBytes(raw)
	s.ErrorIs(err, commitgraph.ErrMalformedCommitGraphFile,
		"expected ErrMalformedCommitGraphFile for CDAT cardinality mismatch")
}
