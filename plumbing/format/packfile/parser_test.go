package packfile_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	billy "github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	gogitbinary "github.com/go-git/go-git/v6/utils/binary"
)

func TestParserHashes(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("packfile-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)
		assertParserOutput(t, f, entries)
	})
}

func TestParserStorageModes(t *testing.T) {
	t.Parallel()

	// TODO: extend to SHA256 once the parser's low-memory path supports it.
	packs := fixtures.ByTag("packfile-entries").ByObjectFormat("sha1")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.Entries(f)

		tests := []struct {
			name              string
			storage           storer.Storer
			option            packfile.ParserOption
			wantLowMemoryMode bool
		}{
			{
				name:              "with filesystem storage",
				storage:           filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()),
				wantLowMemoryMode: true,
			},
			{
				name:    "with storage and high memory mode",
				storage: filesystem.NewStorageWithOptions(osfs.New(t.TempDir()), cache.NewObjectLRUDefault(), filesystem.Options{HighMemoryMode: true}),
			},
			{
				name:    "with memory storage",
				storage: memory.NewStorage(),
			},
			{
				name:   "with memory storage and high memory mode",
				option: packfile.WithHighMemoryMode(),
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if closer, ok := tc.storage.(io.Closer); ok {
					defer func() { _ = closer.Close() }()
				}

				obs := new(testObserver)
				pf, pfErr := f.Packfile()
				require.NoError(t, pfErr)

				opts := []packfile.ParserOption{
					packfile.WithScannerObservers(obs),
					packfile.WithStorage(tc.storage),
				}
				if f.ObjectFormat == "sha256" {
					opts = append(opts, packfile.WithObjectFormat(config.SHA256))
				}
				if tc.option != nil {
					opts = append(opts, tc.option)
				}

				parser := packfile.NewParser(pf, opts...)

				field := reflect.ValueOf(parser).Elem().FieldByName("lowMemoryMode")
				assert.Equal(t, tc.wantLowMemoryMode, field.Bool())

				_, err := parser.Parse(t.Context())
				require.NoError(t, err)

				assert.Equal(t, f.PackfileHash, obs.checksum)
				assert.Len(t, obs.objects, len(entries))

				for _, obj := range obs.objects {
					h := plumbing.NewHash(obj.hash)
					offset, ok := entries[h]
					assert.True(t, ok, "unexpected object %s", obj.hash)
					assert.Equal(t, offset, obj.offset, "offset mismatch for %s", obj.hash)
				}
			})
		}
	})
}

func assertParserOutput(t *testing.T, f *fixtures.Fixture, entries map[plumbing.Hash]int64) {
	t.Helper()

	obs := new(testObserver)
	pf, pfErr := f.Packfile()
	require.NoError(t, pfErr)

	opts := []packfile.ParserOption{packfile.WithScannerObservers(obs)}
	if f.ObjectFormat == "sha256" {
		opts = append(opts, packfile.WithObjectFormat(config.SHA256))
	}

	parser := packfile.NewParser(pf, opts...)

	_, err := parser.Parse(t.Context())
	require.NoError(t, err)

	assert.Equal(t, f.PackfileHash, obs.checksum)
	assert.Len(t, obs.objects, len(entries))

	for _, obj := range obs.objects {
		h := plumbing.NewHash(obj.hash)
		offset, ok := entries[h]
		assert.True(t, ok, "unexpected object %s", obj.hash)
		assert.Equal(t, offset, obj.offset, "offset mismatch for %s", obj.hash)
	}
}

func TestParserMalformedPack(t *testing.T) {
	t.Parallel()
	f := fixtures.Basic().One()
	pf, pfErr := f.Packfile()
	require.NoError(t, pfErr)
	parser := packfile.NewParser(io.LimitReader(pf, 300))

	_, err := parser.Parse(t.Context())
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestThinPack(t *testing.T) {
	t.Parallel()
	// Initialize an empty repository
	r, err := git.PlainInit(t.Context(), t.TempDir(), true)
	assert.NoError(t, err)

	// Try to parse a thin pack without having the required objects in the repo to
	// see if the correct errors are returned
	thinpack := fixtures.ByTag("thinpack").One()
	thinPf, thinPfErr := thinpack.Packfile()
	require.NoError(t, thinPfErr)
	parser := packfile.NewParser(thinPf, packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!

	_, err = parser.Parse(t.Context())
	assert.ErrorIs(t, err, packfile.ErrReferenceDeltaNotFound)

	// start over with a clean repo
	_ = r.Close()
	r, err = git.PlainInit(t.Context(), t.TempDir(), true)
	assert.NoError(t, err)
	defer func() { _ = r.Close() }()

	// Now unpack a base packfile into our empty repo:
	f := fixtures.ByURL("https://github.com/spinnaker/spinnaker.git").One()
	w, err := r.Storer.(storer.PackfileWriter).PackfileWriter(t.Context())
	assert.NoError(t, err)
	fPf, fPfErr := f.Packfile()
	require.NoError(t, fPfErr)
	_, err = io.Copy(w, fPf)
	assert.NoError(t, err)
	assert.NoError(t, w.Close())

	// Check that the test object that will come with our thin pack is *not* in the repo
	_, err = r.Storer.EncodedObject(t.Context(), plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	// Now unpack the thin pack:
	thinPf2, thinPf2Err := thinpack.Packfile()
	require.NoError(t, thinPf2Err)
	parser = packfile.NewParser(thinPf2, packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!

	h, err := parser.Parse(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, plumbing.NewHash("1288734cbe0b95892e663221d94b95de1f5d7be8"), h)

	// Check that our test object is now accessible
	_, err = r.Storer.EncodedObject(t.Context(), plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.NoError(t, err)
}

func TestResolveExternalRefsInThinPack(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("codecommit").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse(t.Context())
	assert.NoError(t, err)
	assert.NotEqual(t, checksum, plumbing.ZeroHash)
}

func TestResolveExternalRefs(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("delta-before-base").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse(t.Context())
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func TestMemoryResolveExternalRefs(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("delta-before-base").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack, packfile.WithStorage(memory.NewStorage()))

	checksum, err := parser.Parse(t.Context())
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func BenchmarkParseBasic(b *testing.B) {
	for _, format := range []string{"sha1", "sha256"} {
		packs := fixtures.ByTag("packfile-entries").ByObjectFormat(format)
		if len(packs) == 0 {
			continue
		}

		f := packs.One()
		pf, err := f.Packfile()
		if err != nil {
			b.Fatal(err)
		}

		var scanOpts []packfile.ScannerOption
		var parseOpts []packfile.ParserOption
		if f.ObjectFormat == "sha256" {
			scanOpts = append(scanOpts, packfile.WithSHA256())
			parseOpts = append(parseOpts, packfile.WithObjectFormat(config.SHA256))
		}

		scanner := packfile.NewScanner(pf, scanOpts...)
		storage := filesystem.NewStorage(osfs.New(b.TempDir()), cache.NewObjectLRUDefault())
		b.Cleanup(func() {
			_ = storage.Close()
		})

		// TODO: storage modes for SHA256 once the parser's low-memory path supports it.
		if f.ObjectFormat != "sha256" {
			b.Run(format+"/with_storage", func(b *testing.B) {
				benchmarkParseBasic(b, pf, scanner, append(parseOpts, packfile.WithStorage(storage))...)
			})
			b.Run(format+"/with_memory_storage", func(b *testing.B) {
				benchmarkParseBasic(b, pf, scanner, append(parseOpts, packfile.WithStorage(memory.NewStorage()))...)
			})
		}
		b.Run(format+"/without_storage", func(b *testing.B) {
			benchmarkParseBasic(b, pf, scanner, parseOpts...)
		})
	}
}

func benchmarkParseBasic(b *testing.B,
	f billy.File, scanner *packfile.Scanner,
	opts ...packfile.ParserOption,
) {
	for i := 0; i < b.N; i++ {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			b.Fatal(err)
		}
		if err := scanner.Reset(); err != nil {
			b.Fatal(err)
		}
		parser := packfile.NewParser(scanner, opts...)

		checksum, err := parser.Parse(b.Context())
		if err != nil {
			b.Fatal(err)
		}

		if checksum == plumbing.ZeroHash {
			b.Fatal("failed to parse")
		}
	}
}

func BenchmarkParse(b *testing.B) {
	for _, f := range fixtures.ByTag("packfile") {
		pff, pffErr := f.Packfile()
		if pffErr != nil {
			b.Fatal(pffErr)
		}
		scanner := packfile.NewScanner(pff)

		b.Run(f.URL, func(b *testing.B) {
			benchmarkParseBasic(b, pff, scanner)
		})
	}
}

// BenchmarkParseAlternatingDeltaChain exercises the depth-first delta walk
// on packs built from one non-delta base followed by N delta entries that
// alternate between OFS_DELTA and REF_DELTA, each derived from the
// previous entry. The fixture-based BenchmarkParse and BenchmarkParseBasic
// do not contain this shape — they cover pure OFS chains as produced by
// canonical Git's repacker — so without this target a regression in
// resolveDeltas would go unnoticed.
func BenchmarkParseAlternatingDeltaChain(b *testing.B) {
	for _, chainDepth := range []int{1, 4, 16, 64, 256} {
		pack := buildAlternatingDeltaChainPack(b, chainDepth)
		b.Run(fmt.Sprintf("depth=%d", chainDepth), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(pack)))
			for i := 0; i < b.N; i++ {
				parser := packfile.NewParser(bytes.NewReader(pack))
				if _, err := parser.Parse(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// buildAlternatingDeltaChainPack returns a pack with one non-delta base
// followed by chainDepth deltas. Odd-indexed deltas are OFS_DELTAs whose
// base is the immediately preceding entry; even-indexed deltas are
// REF_DELTAs whose base hash is the resolved hash of the preceding entry.
// This shape exercises every transition (non-delta → OFS, OFS → REF,
// REF → OFS, REF → REF) the parser's depth-first walk has to handle.
func buildAlternatingDeltaChainPack(tb testing.TB, chainDepth int) []byte {
	tb.Helper()

	contents := make([][]byte, chainDepth+1)
	contents[0] = []byte("benchmark base payload for the alternating delta chain")
	for i := 1; i <= chainDepth; i++ {
		next := make([]byte, len(contents[i-1]))
		copy(next, contents[i-1])
		next[i%len(next)] ^= 0xff
		contents[i] = next
	}

	hashes := make([]plumbing.Hash, chainDepth+1)
	for i := range contents {
		hashes[i] = blobHash(contents[i])
	}

	var pack bytes.Buffer
	sha := sha1.New()
	w := io.MultiWriter(&pack, sha)

	_, _ = w.Write([]byte("PACK"))
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(chainDepth+1))

	offsets := make([]int64, chainDepth+1)

	// Entry 0: non-delta base blob.
	offsets[0] = int64(pack.Len())
	writePackObjectHeader(tb, w, plumbing.BlobObject, int64(len(contents[0])))
	writeZlibPayload(tb, w, contents[0])

	for i := 1; i <= chainDepth; i++ {
		offsets[i] = int64(pack.Len())
		delta := packfile.DiffDelta(contents[i-1], contents[i])
		if i%2 == 1 {
			writePackObjectHeader(tb, w, plumbing.OFSDeltaObject, int64(len(delta)))
			_ = gogitbinary.WriteVariableWidthInt(w, offsets[i]-offsets[i-1])
		} else {
			writePackObjectHeader(tb, w, plumbing.REFDeltaObject, int64(len(delta)))
			_, _ = hashes[i-1].WriteTo(w)
		}
		writeZlibPayload(tb, w, delta)
	}

	_, _ = pack.Write(sha.Sum(nil))
	return pack.Bytes()
}

type observerObject struct {
	hash   string
	otype  plumbing.ObjectType
	size   int64
	offset int64
	crc    uint32
}

type testObserver struct {
	count    uint32
	checksum string
	objects  []observerObject
	pos      map[int64]int
}

func (t *testObserver) OnHeader(count uint32) error {
	t.count = count
	t.pos = make(map[int64]int, count)
	return nil
}

func (t *testObserver) OnInflatedObjectHeader(otype plumbing.ObjectType, objSize, pos int64) error {
	o := t.get(pos)
	o.otype = otype
	o.size = objSize
	o.offset = pos

	t.put(pos, o)

	return nil
}

func (t *testObserver) OnInflatedObjectContent(h plumbing.Hash, pos int64, crc uint32, _ []byte) error {
	o := t.get(pos)
	o.hash = h.String()
	o.crc = crc

	t.put(pos, o)

	return nil
}

func (t *testObserver) OnFooter(h plumbing.Hash) error {
	t.checksum = h.String()
	return nil
}

func (t *testObserver) get(pos int64) observerObject {
	i, ok := t.pos[pos]
	if ok {
		return t.objects[i]
	}

	return observerObject{}
}

func (t *testObserver) put(pos int64, o observerObject) {
	i, ok := t.pos[pos]
	if ok {
		t.objects[i] = o
		return
	}

	t.pos[pos] = len(t.objects)
	t.objects = append(t.objects, o)
}

func TestChecksumMismatch(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "temp.pack")
	require.NoError(t, err)
	defer f.Close()

	basicPf, bpfErr := fixtures.Basic().One().Packfile()
	require.NoError(t, bpfErr)
	_, err = io.Copy(f, basicPf)
	require.NoError(t, err)

	_, err = f.Seek(-1, io.SeekEnd)
	require.NoError(t, err)

	_, err = f.Write([]byte{0})
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	scanner := packfile.NewScanner(f)
	parser := packfile.NewParser(scanner)

	_, err = parser.Parse(t.Context())
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestMalformedPack(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "temp.pack")
	require.NoError(t, err)
	defer f.Close()

	basicPf2, bpf2Err := fixtures.Basic().One().Packfile()
	require.NoError(t, bpf2Err)
	_, err = io.Copy(f, io.LimitReader(basicPf2, 200))
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	scanner := packfile.NewScanner(f)
	parser := packfile.NewParser(scanner)

	_, err = parser.Parse(t.Context())
	require.ErrorContains(t, err, "malformed pack")
}

func TestParserRejectsOverflowingObjectHeader(t *testing.T) {
	t.Parallel()

	// Build a minimal pack whose first (and only) object header advertises
	// a variable-length size with enough continuation bytes that the
	// running shift would exceed what a uint64 can hold. The decoder must
	// reject this as malformed input rather than propagate a value that
	// later flows into a buffer allocation.
	var body bytes.Buffer
	body.WriteString("PACK")
	_ = binary.Write(&body, binary.BigEndian, uint32(2))
	_ = binary.Write(&body, binary.BigEndian, uint32(1))
	body.WriteByte(0x90) // type=commit, continuation=1, low nibble=0
	body.Write(bytes.Repeat([]byte{0x80}, 9))

	sum := sha1.Sum(body.Bytes())
	body.Write(sum[:])

	parser := packfile.NewParser(bytes.NewReader(body.Bytes()))

	_, err := parser.Parse(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "malformed pack")
}

// writePackObjectHeader writes a packfile object header for an entry of
// the given type and uncompressed payload size in the variable-length
// encoding used by the pack format (4 bits of size in the first byte, 7
// bits per continuation byte).
func writePackObjectHeader(tb testing.TB, w io.Writer, typ plumbing.ObjectType, size int64) {
	tb.Helper()
	first := byte(typ)<<4 | byte(size&0x0F)
	rest := uint(size >> 4)
	if rest != 0 {
		first |= 0x80
	}
	_, _ = w.Write([]byte{first})
	if rest != 0 {
		_ = packutil.EncodeLEB128ToWriter(w, rest)
	}
}

// writeZlibPayload zlib-compresses payload and writes the result to w.
func writeZlibPayload(tb testing.TB, w io.Writer, payload []byte) {
	tb.Helper()
	zw := zlib.NewWriter(w)
	_, _ = zw.Write(payload)
	_ = zw.Close()
}

func blobHash(content []byte) plumbing.Hash {
	hasher := plumbing.NewHasher(config.SHA1, plumbing.BlobObject, int64(len(content)))
	_, _ = hasher.Write(content)
	return hasher.Sum()
}

// TestParserResolvesRefDeltaOfOfsDelta covers a pack containing a chain
// where a REF_DELTA's base is itself an OFS_DELTA. This shape is legal per
// the packfile spec and canonical Git's threaded_second_pass
// (builtin/index-pack.c:1103 in v2.54.0 94f057755b) walks delta children
// of both kinds depth-first from every non-delta base.
//
// A two-pass parser that resolves all REF_DELTAs first and OFS_DELTAs
// second would look up the REF_DELTA's base hash before the OFS_DELTA has
// been resolved (its hash is unknown at scan time), silently mark it as a
// thin-pack external reference, and then fail to materialise the leaf
// object.
//
// The probe runs under every storage mode the parser supports, because
// the storage-backed paths (parentReader at parser.go:333 and the
// LowMemoryMode branch in ensureContent) reload a resolved delta's
// contents through parent.Hash, and the new depth-first walk depends on
// that hash being set on the just-resolved OFS-delta before any REF-delta
// children look it up.
func TestParserResolvesRefDeltaOfOfsDelta(t *testing.T) {
	t.Parallel()

	pack, midHash, leafHash := buildRefOnOfsDeltaChainPack(t)

	tests := []struct {
		name    string
		storage storer.Storer
		option  packfile.ParserOption
	}{
		{
			name: "no storage",
		},
		{
			name:    "with memory storage",
			storage: memory.NewStorage(),
		},
		{
			name:   "with memory storage and high memory mode",
			option: packfile.WithHighMemoryMode(),
		},
		{
			name:    "with filesystem storage",
			storage: filesystem.NewStorage(osfs.New(t.TempDir()), cache.NewObjectLRUDefault()),
		},
		{
			name:    "with filesystem storage and high memory mode",
			storage: filesystem.NewStorageWithOptions(osfs.New(t.TempDir()), cache.NewObjectLRUDefault(), filesystem.Options{HighMemoryMode: true}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if closer, ok := tc.storage.(io.Closer); ok {
				defer func() { _ = closer.Close() }()
			}

			obs := new(testObserver)
			opts := []packfile.ParserOption{packfile.WithScannerObservers(obs)}
			if tc.storage != nil {
				opts = append(opts, packfile.WithStorage(tc.storage))
			}
			if tc.option != nil {
				opts = append(opts, tc.option)
			}

			parser := packfile.NewParser(bytes.NewReader(pack), opts...)

			_, err := parser.Parse(t.Context())
			require.NoError(t, err, "parser must resolve REF-delta whose base is an in-pack OFS-delta")

			seen := make(map[plumbing.Hash]bool, len(obs.objects))
			for _, o := range obs.objects {
				seen[plumbing.NewHash(o.hash)] = true
			}
			assert.True(t, seen[midHash], "OFS-delta resolved hash %s missing from observer", midHash)
			assert.True(t, seen[leafHash], "REF-delta resolved hash %s missing from observer", leafHash)
		})
	}
}

// buildRefOnOfsDeltaChainPack returns a 3-entry pack with the shape
// [base blob, OFS-delta(base=blob), REF-delta(base=OFS-delta-hash)], along
// with the resolved hashes of the two delta entries.
func buildRefOnOfsDeltaChainPack(t *testing.T) (pack []byte, midHash, leafHash plumbing.Hash) {
	t.Helper()

	base := []byte("a stable base payload used by the OFS-delta entry")
	mid := []byte("a stable base payload modified by the OFS-delta entry")
	leaf := []byte("a stable base payload modified twice for the REF-delta")

	midHash = blobHash(mid)
	leafHash = blobHash(leaf)

	var buf bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&buf, h)

	// PACK header: magic, version 2, 3 entries.
	_, _ = w.Write([]byte("PACK"))
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(3))

	// Entry 1: non-delta base blob at offset 12.
	obj1Offset := int64(buf.Len())
	writePackObjectHeader(t, w, plumbing.BlobObject, int64(len(base)))
	writeZlibPayload(t, w, base)

	// Entry 2: OFS-delta whose base is entry 1.
	obj2Offset := int64(buf.Len())
	delta12 := packfile.DiffDelta(base, mid)
	writePackObjectHeader(t, w, plumbing.OFSDeltaObject, int64(len(delta12)))
	_ = gogitbinary.WriteVariableWidthInt(w, obj2Offset-obj1Offset)
	writeZlibPayload(t, w, delta12)

	// Entry 3: REF-delta whose base is entry 2's resolved hash.
	delta23 := packfile.DiffDelta(mid, leaf)
	writePackObjectHeader(t, w, plumbing.REFDeltaObject, int64(len(delta23)))
	_, _ = midHash.WriteTo(w)
	writeZlibPayload(t, w, delta23)

	// SHA-1 trailer over the pack body.
	_, _ = buf.Write(h.Sum(nil))

	return buf.Bytes(), midHash, leafHash
}

// TestParserParseRejectsSecondCall pins the single-shot Parser invariant
// documented on the Parser type: once Parse has been called (whether it
// returned successfully or with an error mid-walk), a subsequent call
// against the same instance must fail loudly rather than silently
// running over the prior call's leftover state.
func TestParserParseRejectsSecondCall(t *testing.T) {
	t.Parallel()

	// Build a minimal valid pack with zero objects (header + count=0 +
	// SHA-1 trailer over the header). Parse accepts this and returns
	// the pack checksum; the test then asserts the second call against
	// the same Parser is rejected.
	var buf bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&buf, h)
	_, _ = w.Write([]byte{'P', 'A', 'C', 'K'})
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(0))
	_, _ = buf.Write(h.Sum(nil))

	p := packfile.NewParser(bytes.NewReader(buf.Bytes()))
	_, err := p.Parse(t.Context())
	require.NoError(t, err, "first Parse on the empty-object pack should succeed")

	_, err = p.Parse(t.Context())
	assert.ErrorIs(t, err, packfile.ErrParserConsumed, "second Parse must return ErrParserConsumed")
}
