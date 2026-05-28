package commitgraph

import (
	"bytes"
	"crypto"
	encbin "encoding/binary"
	"errors"
	"io"
	"math"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// ErrUnsupportedVersion is returned by OpenFileIndex when the commit graph
	// file version is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrUnsupportedHash is returned by OpenFileIndex when the commit graph
	// hash function is not supported. Currently only SHA-1 is defined and
	// supported.
	ErrUnsupportedHash = errors.New("unsupported hash algorithm")
	// ErrMalformedCommitGraphFile is returned by OpenFileIndex when the commit
	// graph file is corrupted.
	ErrMalformedCommitGraphFile = errors.New("malformed commit graph file")
	// ErrTooManyChunks is returned by Encoder.Encode when the assembled
	// chunk-table configuration would not fit the uint8 the on-disk
	// header stores at byte 6.
	ErrTooManyChunks = errors.New("commitgraph: too many chunks")

	commitFileSignature = []byte{'C', 'G', 'P', 'H'}

	parentNone        = uint32(0x70000000)
	parentOctopusUsed = uint32(0x80000000)
	parentOctopusMask = uint32(0x7fffffff)
	parentLast        = uint32(0x80000000)
)

const (
	szUint32 = 4
	szUint64 = 8

	szSignature  = 4
	szHeader     = 4
	szCommitData = 2*szUint32 + szUint64

	lenFanout = 256
)

type sizer interface {
	Size() int64
}

// readerSize returns the byte length reachable from r. It honours bytes.Reader
// (Size()) and any io.Seeker (Seek to SeekEnd). When neither is available
// the size is reported as 0 with a non-nil error so callers can decide
// whether to skip the size-dependent checks.
func readerSize(r io.ReaderAt) (int64, error) {
	if s, ok := r.(sizer); ok {
		return s.Size(), nil
	}
	if s, ok := r.(io.Seeker); ok {
		return s.Seek(0, io.SeekEnd)
	}
	return 0, errors.New("commitgraph: cannot determine reader size")
}

type fileIndex struct {
	reader                ReaderAtCloser
	fanout                [lenFanout]uint32
	offsets               [lenChunks]int64
	sizes                 [lenChunks]int64 // byte length of each known chunk
	parent                Index
	hasGenerationV2       bool
	minimumNumberOfHashes uint32
	objSize               int
	numChunks             uint8
	fileSize              int64
}

// ReaderAtCloser is an interface that combines io.ReaderAt and io.Closer.
type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

// OpenFileIndex opens a serialized commit graph file in the format described at
// https://github.com/git/git/blob/v2.54.0/Documentation/technical/commit-graph-format.adoc
func OpenFileIndex(reader ReaderAtCloser) (Index, error) {
	return OpenFileIndexWithParent(reader, nil)
}

// OpenFileIndexWithParent opens a serialized commit graph file in the format described at
// https://github.com/git/git/blob/v2.54.0/Documentation/technical/commit-graph-format.adoc
func OpenFileIndexWithParent(reader ReaderAtCloser, parent Index) (Index, error) {
	if reader == nil {
		return nil, io.ErrUnexpectedEOF
	}
	fi := &fileIndex{reader: reader, parent: parent, objSize: config.SHA1Size}

	if err := fi.verifyFileHeader(); err != nil {
		return nil, err
	}
	if err := fi.verifyFileSize(); err != nil {
		return nil, err
	}
	if err := fi.readChunkHeaders(); err != nil {
		return nil, err
	}
	if err := fi.verifyChunkSizes(); err != nil {
		return nil, err
	}
	if err := fi.readFanout(); err != nil {
		return nil, err
	}

	fi.hasGenerationV2 = fi.offsets[GenerationDataChunk] > 0
	if fi.parent != nil {
		fi.hasGenerationV2 = fi.hasGenerationV2 && fi.parent.HasGenerationV2()
	}

	if fi.parent != nil {
		fi.minimumNumberOfHashes = fi.parent.MaximumNumberOfHashes()
	}

	return fi, nil
}

// Close closes the underlying reader and the parent index if it exists.
func (fi *fileIndex) Close() (err error) {
	if fi.parent != nil {
		defer func() {
			parentErr := fi.parent.Close()
			// only report the error from the parent if there is no error from the reader
			if err == nil {
				err = parentErr
			}
		}()
	}
	err = fi.reader.Close()
	return err
}

func (fi *fileIndex) verifyFileHeader() error {
	// Verify file signature
	signature := make([]byte, szSignature)
	if _, err := fi.reader.ReadAt(signature, 0); err != nil {
		return err
	}
	if !bytes.Equal(signature, commitFileSignature) {
		return ErrMalformedCommitGraphFile
	}

	// Read and verify the file header
	header := make([]byte, szHeader)
	if _, err := fi.reader.ReadAt(header, szHeader); err != nil {
		return err
	}
	if header[0] != 1 {
		return ErrUnsupportedVersion
	}
	if (fi.objSize != crypto.SHA1.Size() || header[1] != 1) &&
		(fi.objSize != crypto.SHA256.Size() || header[1] != 2) {
		// Unknown hash type / unsupported hash type
		return ErrUnsupportedHash
	}
	fi.numChunks = header[2]

	return nil
}

// verifyFileSize records the reader's byte length on fi.fileSize and
// mirrors canonical Git's parse_commit_graph_v1 check [1] that the
// file is large enough to hold the header, the full chunk table of
// contents (including the zero terminator), the fanout table, and
// the trailing hash trailer.
//
// If the reader satisfies neither sizer nor io.Seeker the size is
// left at zero and the precheck is skipped; the per-chunk reads in
// readChunkHeaders still detect truncation reactively.
//
// [1]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L419
func (fi *fileIndex) verifyFileSize() error {
	size, err := readerSize(fi.reader)
	if err != nil {
		// Without a size we fall back on per-chunk reads to detect
		// truncation. The reader interface only requires io.ReaderAt
		// and io.Closer, so this branch is taken by exotic callers
		// only; the in-tree filesystem and in-memory paths both
		// satisfy sizer or io.Seeker.
		return nil
	}
	fi.fileSize = size

	minSize := int64(szSignature+szHeader) +
		int64(uint16(fi.numChunks)+1)*int64(szChunkSig+szUint64) +
		int64(lenFanout*szUint32) +
		int64(fi.objSize)
	if size < minSize {
		return ErrMalformedCommitGraphFile
	}
	return nil
}

// chunkAssignment records the file offset of a known chunk type in
// table-of-contents order, so that readChunkHeaders can derive each
// chunk's byte length from adjacent offsets once the terminator is found.
type chunkAssignment struct {
	ct     ChunkType
	offset int64
}

// readChunkHeaders parses the chunk table of contents. The number of
// non-terminating entries is taken from the file header (byte 6), mirroring
// canonical Git's parse_commit_graph_v1 [1] which passes that count to
// read_table_of_contents [2]; the latter iterates exactly num_chunks times
// and rejects both an early zero chunk-id and a non-zero terminator entry.
//
// After the terminator offset is known, the byte length of every known chunk
// is computed as the difference between its starting offset and that of the
// next entry in table-of-contents order (or the terminator for the last
// one). These lengths are stored in fi.sizes and used by GetCommitDataByIndex
// to bound the octopus extra-edge walk, mirroring canonical Git's
// chunk_extra_edges_size / sizeof(uint32_t) guard in fill_commit_in_graph.
//
// [1]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L414
// [2]: https://github.com/git/git/blob/v2.54.0/chunk-format.c#L117
func (fi *fileIndex) readChunkHeaders() error {
	tocBase := int64(szSignature + szHeader)
	const tocEntrySize = szChunkSig + szUint64
	chunkID := make([]byte, szChunkSig) // reused across loop iterations
	var prevOffset int64

	// Canonical Git's read_table_of_contents [2] validates each chunk's
	// offset against mfile_size - hash_size (upper bound) and the
	// previous offset (monotonicity). Mirror that check here.
	upperBound := fi.fileSize - int64(fi.objSize)
	if fi.fileSize == 0 {
		// verifyFileSize was unable to determine the file size; skip
		// the upper-bound check, the per-chunk ReadAt will catch
		// out-of-range offsets reactively.
		upperBound = math.MaxInt64
	}

	// Track every chunk-id seen so far (known and unknown). Canonical
	// Git's read_table_of_contents scans cf->chunks[0..chunks_nr-1] for
	// the incoming id and returns -1 on the first match [2]. Use a map
	// instead of a linear scan; the semantics are identical.
	seen := make(map[[szChunkSig]byte]struct{}, int(fi.numChunks))

	// assigned records, in file order, every chunk whose offset was stored in
	// fi.offsets. After the terminator offset is known, a second pass derives
	// fi.sizes from consecutive offset differences.
	assigned := make([]chunkAssignment, 0, int(fi.numChunks))

	for i := range int(fi.numChunks) {
		entry := io.NewSectionReader(fi.reader, tocBase+int64(i)*tocEntrySize, tocEntrySize)
		if _, err := io.ReadAtLeast(entry, chunkID, szChunkSig); err != nil {
			return err
		}
		chunkOffset, err := binary.ReadUint64(entry)
		if err != nil {
			return err
		}

		// Validate the offset before classifying the chunk-id, mirroring
		// canonical Git's read_table_of_contents which checks the offset
		// against the previous one and the upper bound before any
		// per-chunk dispatch.
		if int64(chunkOffset) > upperBound || int64(chunkOffset) < prevOffset {
			return ErrMalformedCommitGraphFile
		}
		prevOffset = int64(chunkOffset)

		// Reject duplicate chunk-ids (known and unknown alike), matching
		// canonical Git's "duplicate chunk ID" check [2].
		var id [szChunkSig]byte
		copy(id[:], chunkID)
		if _, ok := seen[id]; ok {
			return ErrMalformedCommitGraphFile
		}
		seen[id] = struct{}{}

		chunkType, ok := ChunkTypeFromBytes(chunkID)
		if !ok {
			continue
		}
		// A zero chunk-id inside the declared count is the same condition
		// canonical Git reports as "terminating chunk id appears earlier
		// than expected".
		if chunkType == ZeroChunk {
			return ErrMalformedCommitGraphFile
		}
		if int(chunkType) >= len(fi.offsets) {
			continue
		}
		fi.offsets[chunkType] = int64(chunkOffset)
		assigned = append(assigned, chunkAssignment{ct: chunkType, offset: int64(chunkOffset)})
	}

	// The table is followed by a single terminator entry whose chunk-id
	// is zero. Reading anything else means the declared count does not
	// match the table contents.
	terminator := io.NewSectionReader(fi.reader, tocBase+int64(fi.numChunks)*tocEntrySize, tocEntrySize)
	if _, err := io.ReadAtLeast(terminator, chunkID, szChunkSig); err != nil {
		return err
	}
	if !bytes.Equal(chunkID, ZeroChunk.Signature()) {
		return ErrMalformedCommitGraphFile
	}
	// The terminator entry's offset marks the end of all chunk data. Use it
	// together with the per-chunk start offsets to derive chunk byte lengths.
	terminatorOffset, err := binary.ReadUint64(terminator)
	if err != nil {
		return err
	}
	for i, a := range assigned {
		var end int64
		if i+1 < len(assigned) {
			end = assigned[i+1].offset
		} else {
			end = int64(terminatorOffset)
		}
		fi.sizes[a.ct] = end - a.offset
	}

	if fi.offsets[OIDFanoutChunk] <= 0 || fi.offsets[OIDLookupChunk] <= 0 || fi.offsets[CommitDataChunk] <= 0 {
		return ErrMalformedCommitGraphFile
	}

	return nil
}

// verifyChunkSizes asserts the byte length of every required chunk
// against the fanout-derived commit count. Canonical Git applies the
// same cardinality checks at parse time so that truncated or
// hand-edited files fail once during OpenFileIndex rather than mid-
// walk (commit-graph.c v2.54.0, graph_read_oid_fanout [1],
// graph_read_oid_lookup [2], graph_read_commit_data [3], and
// graph_read_generation_data [4]).
//
// numCommits is fanout[255]; reading the single uint32 at the end of
// the fanout chunk avoids depending on readFanout's later pass.
//
// [1]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L288
// [2]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L311
// [3]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L320
// [4]: https://github.com/git/git/blob/v2.54.0/commit-graph.c#L330
func (fi *fileIndex) verifyChunkSizes() error {
	if fi.sizes[OIDFanoutChunk] != lenFanout*szUint32 {
		return ErrMalformedCommitGraphFile
	}

	var buf [szUint32]byte
	off := fi.offsets[OIDFanoutChunk] + (lenFanout-1)*szUint32
	if _, err := fi.reader.ReadAt(buf[:], off); err != nil {
		return err
	}
	numCommits := int64(encbin.BigEndian.Uint32(buf[:]))
	if numCommits > 0x7fffffff {
		return ErrMalformedCommitGraphFile
	}

	hashSize := int64(fi.objSize)
	if fi.sizes[OIDLookupChunk] != numCommits*hashSize {
		return ErrMalformedCommitGraphFile
	}
	if fi.sizes[CommitDataChunk] != numCommits*(hashSize+szCommitData) {
		return ErrMalformedCommitGraphFile
	}
	if fi.offsets[GenerationDataChunk] > 0 &&
		fi.sizes[GenerationDataChunk] != numCommits*szUint32 {
		return ErrMalformedCommitGraphFile
	}
	return nil
}

func (fi *fileIndex) readFanout() error {
	// The Fanout table is a 256 entry table of the number (as uint32) of OIDs with first byte at most i.
	// Thus F[255] stores the total number of commits (N)
	fanoutReader := io.NewSectionReader(fi.reader, fi.offsets[OIDFanoutChunk], lenFanout*szUint32)
	for i := range 256 {
		fanoutValue, err := binary.ReadUint32(fanoutReader)
		if err != nil {
			return err
		}
		if fanoutValue > 0x7fffffff {
			return ErrMalformedCommitGraphFile
		}
		fi.fanout[i] = fanoutValue
	}
	return nil
}

// GetIndexByHash looks up the provided hash in the commit-graph fanout and returns the index of the commit data for the given hash.
func (fi *fileIndex) GetIndexByHash(h plumbing.Hash) (uint32, error) {
	var oid plumbing.Hash

	// Find the hash in the oid lookup table
	var low uint32
	if h.Bytes()[0] == 0 {
		low = 0
	} else {
		low = fi.fanout[h.Bytes()[0]-1]
	}
	high := fi.fanout[h.Bytes()[0]]
	for low < high {
		mid := (low + high) >> 1
		offset := fi.offsets[OIDLookupChunk] + int64(mid)*int64(fi.objSize)
		if _, err := oid.ReadFrom(io.NewSectionReader(fi.reader, offset, int64(oid.Size()))); err != nil {
			return 0, err
		}
		cmp := h.Compare(oid.Bytes())
		switch {
		case cmp < 0:
			high = mid
		case cmp == 0:
			return mid + fi.minimumNumberOfHashes, nil
		default:
			low = mid + 1
		}
	}

	if fi.parent != nil {
		idx, err := fi.parent.GetIndexByHash(h)
		if err != nil {
			return 0, err
		}
		return idx, nil
	}

	return 0, plumbing.ErrObjectNotFound
}

// GetCommitDataByIndex returns the commit data for the given index in the commit-graph.
func (fi *fileIndex) GetCommitDataByIndex(idx uint32) (*CommitData, error) {
	if idx < fi.minimumNumberOfHashes {
		if fi.parent != nil {
			data, err := fi.parent.GetCommitDataByIndex(idx)
			if err != nil {
				return nil, err
			}
			return data, nil
		}

		return nil, plumbing.ErrObjectNotFound
	}
	idx -= fi.minimumNumberOfHashes
	if idx >= fi.fanout[0xff] {
		return nil, plumbing.ErrObjectNotFound
	}

	offset := fi.offsets[CommitDataChunk] + int64(idx)*int64(fi.objSize+szCommitData)
	commitDataReader := io.NewSectionReader(fi.reader, offset, int64(fi.objSize+szCommitData))

	// TODO: Add support for SHA256
	var treeHash plumbing.Hash
	_, err := treeHash.ReadFrom(commitDataReader)
	if err != nil {
		return nil, err
	}
	parent1, err := binary.ReadUint32(commitDataReader)
	if err != nil {
		return nil, err
	}
	parent2, err := binary.ReadUint32(commitDataReader)
	if err != nil {
		return nil, err
	}
	genAndTime, err := binary.ReadUint64(commitDataReader)
	if err != nil {
		return nil, err
	}

	var parentIndexes []uint32
	switch {
	case parent2&parentOctopusUsed == parentOctopusUsed:
		// Octopus merge — look up extra parents from the EDGE chunk. Canonical
		// Git's fill_commit_in_graph bounds parent_data_pos against
		// chunk_extra_edges_size / sizeof(uint32_t) on every iteration
		// (commit-graph.c v2.54.0); mirror that to refuse out-of-range
		// pointers and sentinel-less runaway reads.
		edgeCount := fi.sizes[ExtraEdgeListChunk] / szUint32
		pos := int64(parent2 & parentOctopusMask)
		if pos >= edgeCount {
			return nil, ErrMalformedCommitGraphFile
		}
		parentIndexes = []uint32{parent1 & parentOctopusMask}
		offset := fi.offsets[ExtraEdgeListChunk] + szUint32*pos
		buf := make([]byte, szUint32)
		for {
			if pos >= edgeCount {
				return nil, ErrMalformedCommitGraphFile
			}
			_, err := fi.reader.ReadAt(buf, offset)
			if err != nil {
				return nil, err
			}

			parent := encbin.BigEndian.Uint32(buf)
			offset += szUint32
			pos++
			parentIndexes = append(parentIndexes, parent&parentOctopusMask)
			if parent&parentLast == parentLast {
				break
			}
		}
	case parent2 != parentNone:
		parentIndexes = []uint32{parent1 & parentOctopusMask, parent2 & parentOctopusMask}
	case parent1 != parentNone:
		parentIndexes = []uint32{parent1 & parentOctopusMask}
	}

	parentHashes, err := fi.getHashesFromIndexes(parentIndexes)
	if err != nil {
		return nil, err
	}

	generationV2 := uint64(0)

	if fi.hasGenerationV2 {
		// set the GenerationV2 result to the commit time
		generationV2 = uint64(genAndTime & 0x3FFFFFFFF)

		// Next read the generation (offset) data from the generation data chunk
		offset := fi.offsets[GenerationDataChunk] + int64(idx)*szUint32
		buf := make([]byte, szUint32)
		if _, err := fi.reader.ReadAt(buf, offset); err != nil {
			return nil, err
		}
		genV2Data := encbin.BigEndian.Uint32(buf)

		// check if the data is an overflow that needs to be looked up in the overflow chunk
		if genV2Data&0x80000000 > 0 {
			// Overflow — look up the corrected commit date from the GDO2
			// chunk. Canonical Git's fill_commit_graph_info refuses an
			// offset_pos that falls past
			// chunk_generation_data_overflow_size / sizeof(uint64_t)
			// (commit-graph.c v2.54.0); mirror that to keep an
			// out-of-range pointer from reading adjacent chunk bytes
			// or past EOF.
			pos := int64(genV2Data & 0x7fffffff)
			overflowCount := fi.sizes[GenerationDataOverflowChunk] / szUint64
			if pos >= overflowCount {
				return nil, ErrMalformedCommitGraphFile
			}
			offset := fi.offsets[GenerationDataOverflowChunk] + pos*szUint64
			buf := make([]byte, 8)
			if _, err := fi.reader.ReadAt(buf, offset); err != nil {
				return nil, err
			}

			generationV2 += encbin.BigEndian.Uint64(buf)
		} else {
			generationV2 += uint64(genV2Data)
		}
	}

	return &CommitData{
		TreeHash:      treeHash,
		ParentIndexes: parentIndexes,
		ParentHashes:  parentHashes,
		Generation:    genAndTime >> 34,
		GenerationV2:  generationV2,
		When:          time.Unix(int64(genAndTime&0x3FFFFFFFF), 0),
	}, nil
}

// GetHashByIndex looks up the hash for the given index in the commit-graph.
func (fi *fileIndex) GetHashByIndex(idx uint32) (found plumbing.Hash, err error) {
	if idx < fi.minimumNumberOfHashes {
		if fi.parent != nil {
			return fi.parent.GetHashByIndex(idx)
		}
		return found, ErrMalformedCommitGraphFile
	}
	idx -= fi.minimumNumberOfHashes
	if idx >= fi.fanout[0xff] {
		return found, ErrMalformedCommitGraphFile
	}

	offset := fi.offsets[OIDLookupChunk] + int64(idx)*int64(fi.objSize)
	if _, err := found.ReadFrom(io.NewSectionReader(fi.reader, offset, int64(found.Size()))); err != nil {
		return found, err
	}

	return found, nil
}

func (fi *fileIndex) getHashesFromIndexes(indexes []uint32) ([]plumbing.Hash, error) {
	hashes := make([]plumbing.Hash, len(indexes))

	for i, idx := range indexes {
		if idx < fi.minimumNumberOfHashes {
			if fi.parent != nil {
				hash, err := fi.parent.GetHashByIndex(idx)
				if err != nil {
					return nil, err
				}
				hashes[i] = hash
				continue
			}

			return nil, ErrMalformedCommitGraphFile
		}

		idx -= fi.minimumNumberOfHashes
		if idx >= fi.fanout[0xff] {
			return nil, ErrMalformedCommitGraphFile
		}

		offset := fi.offsets[OIDLookupChunk] + int64(idx)*int64(fi.objSize)
		if _, err := hashes[i].ReadFrom(io.NewSectionReader(fi.reader, offset, int64(hashes[i].Size()))); err != nil {
			return nil, err
		}
	}

	return hashes, nil
}

// Hashes returns all the hashes that are available in the index.
func (fi *fileIndex) Hashes() []plumbing.Hash {
	hashes := make([]plumbing.Hash, fi.fanout[0xff]+fi.minimumNumberOfHashes)
	for i := uint32(0); i < fi.minimumNumberOfHashes; i++ {
		hash, err := fi.parent.GetHashByIndex(i)
		if err != nil {
			return nil
		}
		hashes[i] = hash
	}

	for i := uint32(0); i < fi.fanout[0xff]; i++ {
		h := &hashes[i+fi.minimumNumberOfHashes]
		offset := fi.offsets[OIDLookupChunk] + int64(i)*int64(h.Size())
		n, err := h.ReadFrom(io.NewSectionReader(fi.reader, offset, int64(h.Size())))
		if err != nil || n < int64(h.Size()) {
			return nil
		}
	}
	return hashes
}

func (fi *fileIndex) HasGenerationV2() bool {
	return fi.hasGenerationV2
}

func (fi *fileIndex) MaximumNumberOfHashes() uint32 {
	return fi.minimumNumberOfHashes + fi.fanout[0xff]
}
