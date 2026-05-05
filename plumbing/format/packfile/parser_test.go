package packfile_test

import (
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
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
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

				_, err := parser.Parse()
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

	_, err := parser.Parse()
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

	_, err := parser.Parse()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestThinPack(t *testing.T) {
	t.Parallel()
	// Initialize an empty repository
	r, err := git.PlainInit(t.TempDir(), true)
	assert.NoError(t, err)

	// Try to parse a thin pack without having the required objects in the repo to
	// see if the correct errors are returned
	thinpack := fixtures.ByTag("thinpack").One()
	thinPf, thinPfErr := thinpack.Packfile()
	require.NoError(t, thinPfErr)
	parser := packfile.NewParser(thinPf, packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!

	_, err = parser.Parse()
	assert.ErrorIs(t, err, packfile.ErrReferenceDeltaNotFound)

	// start over with a clean repo
	_ = r.Close()
	r, err = git.PlainInit(t.TempDir(), true)
	assert.NoError(t, err)
	defer func() { _ = r.Close() }()

	// Now unpack a base packfile into our empty repo:
	f := fixtures.ByURL("https://github.com/spinnaker/spinnaker.git").One()
	w, err := r.Storer.(storer.PackfileWriter).PackfileWriter()
	assert.NoError(t, err)
	fPf, fPfErr := f.Packfile()
	require.NoError(t, fPfErr)
	_, err = io.Copy(w, fPf)
	assert.NoError(t, err)
	assert.NoError(t, w.Close())

	// Check that the test object that will come with our thin pack is *not* in the repo
	_, err = r.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	// Now unpack the thin pack:
	thinPf2, thinPf2Err := thinpack.Packfile()
	require.NoError(t, thinPf2Err)
	parser = packfile.NewParser(thinPf2, packfile.WithStorage(r.Storer)) // ParserWithStorage writes to the storer all parsed objects!

	h, err := parser.Parse()
	assert.NoError(t, err)
	assert.Equal(t, plumbing.NewHash("1288734cbe0b95892e663221d94b95de1f5d7be8"), h)

	// Check that our test object is now accessible
	_, err = r.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(thinpack.Head))
	assert.NoError(t, err)
}

func TestResolveExternalRefsInThinPack(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("codecommit").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse()
	assert.NoError(t, err)
	assert.NotEqual(t, checksum, plumbing.ZeroHash)
}

func TestResolveExternalRefs(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("delta-before-base").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack)

	checksum, err := parser.Parse()
	assert.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, checksum)
}

func TestMemoryResolveExternalRefs(t *testing.T) {
	t.Parallel()
	extRefsThinPack, err := fixtures.ByTag("delta-before-base").One().Packfile()
	require.NoError(t, err)

	parser := packfile.NewParser(extRefsThinPack, packfile.WithStorage(memory.NewStorage()))

	checksum, err := parser.Parse()
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

		checksum, err := parser.Parse()
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

	_, err = parser.Parse()
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

	_, err = parser.Parse()
	require.ErrorContains(t, err, "malformed pack")
}
