package idxfile_test

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func BenchmarkFindOffset(b *testing.B) {
	idx, err := fixtureIndex()
	if err != nil {
		b.Fatal(err.Error())
	}

	for b.Loop() {
		for _, h := range fixtureHashes {
			_, err := idx.FindOffset(h)
			if err != nil {
				b.Fatalf("error getting offset: %s", err)
			}
		}
	}
}

func BenchmarkFindCRC32(b *testing.B) {
	idx, err := fixtureIndex()
	if err != nil {
		b.Fatal(err.Error())
	}

	for b.Loop() {
		for _, h := range fixtureHashes {
			_, err := idx.FindCRC32(h)
			if err != nil {
				b.Fatalf("error getting crc32: %s", err)
			}
		}
	}
}

func BenchmarkContains(b *testing.B) {
	idx, err := fixtureIndex()
	if err != nil {
		b.Fatal(err.Error())
	}

	for b.Loop() {
		for _, h := range fixtureHashes {
			ok, err := idx.Contains(h)
			if err != nil {
				b.Fatalf("error checking if hash is in index: %s", err)
			}

			if !ok {
				b.Error("expected hash to be in index")
			}
		}
	}
}

func BenchmarkEntries(b *testing.B) {
	idx, err := fixtureIndex()
	if err != nil {
		b.Fatal(err.Error())
	}

	for b.Loop() {
		iter, err := idx.Entries()
		if err != nil {
			b.Fatalf("unexpected error getting entries: %s", err)
		}

		var entries int
		for {
			_, err := iter.Next()
			if err != nil {
				if err == io.EOF {
					break
				}

				b.Errorf("unexpected error getting entry: %s", err)
			}

			entries++
		}

		if entries != len(fixtureHashes) {
			b.Errorf("expecting entries to be %d, got %d", len(fixtureHashes), entries)
		}
	}
}

type IndexSuite struct {
	suite.Suite
}

func TestIndexSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IndexSuite))
}

func (s *IndexSuite) TestMayContain() {
	idx, err := fixtureIndex()
	s.NoError(err)

	// Positive: every known hash must report true.
	for _, h := range fixtureHashes {
		s.True(idx.MayContain(h), "expected MayContain=true for %s", h)
	}

	// Negative: find a FanoutMapping entry set to noMapping (-1) and
	// craft a hash starting with that byte; result must be false.
	// FanoutMapping is an exported field so no internal access needed.
	emptyByte := -1
	for b, mapped := range idx.FanoutMapping {
		if mapped == -1 {
			emptyByte = b
			break
		}
	}
	s.Require().NotEqual(-1, emptyByte,
		"fixture must have at least one empty fanout bucket")

	var miss plumbing.Hash
	hashBytes := make([]byte, 20)
	hashBytes[0] = byte(emptyByte)
	miss = plumbing.NewHash(fmt.Sprintf("%x", hashBytes))
	s.False(idx.MayContain(miss),
		"expected MayContain=false for hash starting with 0x%02x", emptyByte)
}

func (s *IndexSuite) TestFindHash() {
	idx, err := fixtureIndex()
	s.NoError(err)

	for i, pos := range fixtureOffsets {
		hash, err := idx.FindHash(pos)
		s.NoError(err)
		s.Equal(fixtureHashes[i], hash)
	}
}

func (s *IndexSuite) TestEntriesByOffset() {
	idx, err := fixtureIndex()
	s.NoError(err)

	entries, err := idx.EntriesByOffset()
	s.NoError(err)
	defer entries.Close()

	for _, pos := range fixtureOffsets {
		e, err := entries.Next()
		s.NoError(err)

		s.Equal(uint64(pos), e.Offset)
	}
}

var fixtureHashes = []plumbing.Hash{
	plumbing.NewHash("303953e5aa461c203a324821bc1717f9b4fff895"),
	plumbing.NewHash("5296768e3d9f661387ccbff18c4dea6c997fd78c"),
	plumbing.NewHash("03fc8d58d44267274edef4585eaeeb445879d33f"),
	plumbing.NewHash("8f3ceb4ea4cb9e4a0f751795eb41c9a4f07be772"),
	plumbing.NewHash("e0d1d625010087f79c9e01ad9d8f95e1628dda02"),
	plumbing.NewHash("90eba326cdc4d1d61c5ad25224ccbf08731dd041"),
	plumbing.NewHash("bab53055add7bc35882758a922c54a874d6b1272"),
	plumbing.NewHash("1b8995f51987d8a449ca5ea4356595102dc2fbd4"),
	plumbing.NewHash("35858be9c6f5914cbe6768489c41eb6809a2bceb"),
}

var fixtureOffsets = []int64{
	12,
	142,
	1601322837,
	2646996529,
	3452385606,
	3707047470,
	5323223332,
	5894072943,
	5924278919,
}

func fixtureIndex() (*idxfile.MemoryIndex, error) {
	raw, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(fixtureLarge4GB)))
	if err != nil {
		return nil, fmt.Errorf("unexpected error decoding fixture: %s", err)
	}

	idx := new(idxfile.MemoryIndex)

	d := idxfile.NewDecoder(idxfile.FromBytes(raw), hash.New(crypto.SHA1))
	if err := d.Decode(idx); err != nil {
		return nil, fmt.Errorf("unexpected error decoding index: %s", err)
	}

	return idx, nil
}

type MemoryIndexSuite struct {
	suite.Suite
}

func TestMemoryIndexSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(MemoryIndexSuite))
}

func (s *MemoryIndexSuite) TestCloseIsNoOp() {
	idx := idxfile.NewMemoryIndex(crypto.SHA1.Size())
	s.NoError(idx.Close())
	// Calling Close repeatedly is fine.
	s.NoError(idx.Close())
}

func (s *MemoryIndexSuite) TestSatisfiesIndexInterface() {
	// Close must be reachable via the interface.
	var idx idxfile.Index = idxfile.NewMemoryIndex(crypto.SHA1.Size())
	s.NoError(idx.Close())
}

func TestMemoryIndex_EntriesWithPrefix_Empty(t *testing.T) {
	t.Parallel()
	idx, err := fixtureIndex()
	require.NoError(t, err)

	iter, err := idx.EntriesWithPrefix(nil)
	require.NoError(t, err)
	defer iter.Close()

	// Empty prefix returns all entries (same as Entries()).
	n := 0
	for {
		_, err := iter.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		n++
	}
	assert.Equal(t, len(fixtureHashes), n)
}

func TestMemoryIndex_EntriesWithPrefix_OneByteMatch(t *testing.T) {
	t.Parallel()
	idx, err := fixtureIndex()
	require.NoError(t, err)

	// Pick the first byte of a known hash; the iterator should yield
	// every entry in that bucket.
	target := fixtureHashes[0]
	prefix := target.Bytes()[:1]

	iter, err := idx.EntriesWithPrefix(prefix)
	require.NoError(t, err)
	defer iter.Close()

	var got []plumbing.Hash
	for {
		e, err := iter.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		got = append(got, e.Hash)
	}

	// Cross-check: every yielded hash must start with prefix.
	for _, h := range got {
		assert.True(t, bytes.HasPrefix(h.Bytes(), prefix),
			"hash %v should start with prefix %x", h, prefix)
	}
	// And the seed hash must be among the yielded entries.
	assert.Contains(t, got, target)
}

func TestMemoryIndex_EntriesWithPrefix_MultiByte(t *testing.T) {
	t.Parallel()

	// Synthetic index with five entries all in fanout bucket 0x42,
	// sorted by full hash. The matching entries for prefix
	// [0x42, 0x05] are a contiguous run in the middle of the bucket,
	// so the iterator must skip past 0x42_00 before yielding.
	makeHash := func(b1, b2, b3 byte) plumbing.Hash {
		raw := make([]byte, 20)
		raw[0] = b1
		raw[1] = b2
		raw[2] = b3
		return plumbing.NewHash(fmt.Sprintf("%x", raw))
	}

	hashes := []plumbing.Hash{
		makeHash(0x42, 0x00, 0xaa),
		makeHash(0x42, 0x05, 0xab),
		makeHash(0x42, 0x05, 0xbb),
		makeHash(0x42, 0x05, 0xcc),
		makeHash(0x42, 0x07, 0xdd),
	}

	w := &idxfile.Writer{}
	require.NoError(t, w.OnHeader(uint32(len(hashes))))
	for i, h := range hashes {
		w.Add(h, uint64(100+i), uint32(i))
	}
	require.NoError(t, w.OnFooter(
		plumbing.NewHash("0000000000000000000000000000000000000001")))

	idx, err := w.Index()
	require.NoError(t, err)

	collect := func(t *testing.T, prefix []byte) []plumbing.Hash {
		t.Helper()
		iter, err := idx.EntriesWithPrefix(prefix)
		require.NoError(t, err)
		defer iter.Close()
		var got []plumbing.Hash
		for {
			e, err := iter.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			got = append(got, e.Hash)
		}
		return got
	}

	t.Run("middle run", func(t *testing.T) {
		t.Parallel()
		got := collect(t, []byte{0x42, 0x05})
		require.Len(t, got, 3)
		for _, h := range got {
			assert.True(t, bytes.HasPrefix(h.Bytes(), []byte{0x42, 0x05}))
		}
	})

	t.Run("end of bucket", func(t *testing.T) {
		t.Parallel()
		got := collect(t, []byte{0x42, 0x07})
		require.Len(t, got, 1)
		assert.Equal(t, hashes[4], got[0])
	})

	t.Run("start of bucket", func(t *testing.T) {
		t.Parallel()
		got := collect(t, []byte{0x42, 0x00})
		require.Len(t, got, 1)
		assert.Equal(t, hashes[0], got[0])
	})

	t.Run("no match in populated bucket", func(t *testing.T) {
		t.Parallel()
		got := collect(t, []byte{0x42, 0x06})
		assert.Empty(t, got)
	})

	t.Run("three-byte refinement", func(t *testing.T) {
		t.Parallel()
		got := collect(t, []byte{0x42, 0x05, 0xbb})
		require.Len(t, got, 1)
		assert.Equal(t, hashes[2], got[0])
	})
}

func TestMemoryIndex_EntriesWithPrefix_NoMatch(t *testing.T) {
	t.Parallel()
	idx, err := fixtureIndex()
	require.NoError(t, err)

	// Find an empty fanout bucket (FanoutMapping == noMapping == -1).
	var emptyByte byte
	found := false
	for b := range 256 {
		if idx.FanoutMapping[b] == -1 {
			emptyByte = byte(b)
			found = true
			break
		}
	}
	if !found {
		t.Skip("fixture has objects in every fanout bucket")
	}

	iter, err := idx.EntriesWithPrefix([]byte{emptyByte})
	require.NoError(t, err)
	defer iter.Close()
	_, err = iter.Next()
	assert.Equal(t, io.EOF, err)
}

func TestOffsetHashConcurrentPopulation(t *testing.T) {
	t.Parallel()
	idx, err := fixtureIndex()
	if err != nil {
		t.Fatalf("failed to build fixture index: %v", err)
	}

	var wg sync.WaitGroup

	for _, h := range fixtureHashes {
		wg.Go(func() {
			for range 5000 {
				_, _ = idx.FindOffset(h)
			}
		})
	}

	for _, off := range fixtureOffsets {
		wg.Go(func() {
			for range 3000 {
				_, _ = idx.FindHash(off)
			}
		})
	}

	wg.Wait()
}
