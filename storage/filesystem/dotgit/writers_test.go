package dotgit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

func BenchmarkNewObjectPack(b *testing.B) {
	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()
	fs := osfs.New(b.TempDir())

	for b.Loop() {
		w, err := newPackWrite(fs, config.SHA1, false)

		require.NoError(b, err)
		pf, pfErr := f.Packfile()
		require.NoError(b, pfErr)
		_, err = io.Copy(w, pf)

		require.NoError(b, err)
		require.NoError(b, w.Close())
	}
}

func TestNewObjectPack(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()

	fs := osfs.New(t.TempDir())
	dot := New(fs)

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	pf, pfErr := f.Packfile()
	require.NoError(t, pfErr)
	_, err = io.Copy(w, pf)
	require.NoError(t, err)

	require.NoError(t, w.Close())

	pfPath := fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash)
	idxPath := fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash)

	stat, err := fs.Stat(pfPath)
	require.NoError(t, err)
	assert.Equal(t, int64(84794), stat.Size())

	stat, err = fs.Stat(idxPath)
	require.NoError(t, err)
	assert.Equal(t, int64(1940), stat.Size())

	pf2, err := fs.Open(pfPath)
	assert.NoError(t, err)

	objFound := false
	pfs := packfile.NewScanner(pf2)
	for pfs.Scan() {
		data := pfs.Data()
		if data.Section != packfile.ObjectSection {
			continue
		}

		objFound = true
		assert.NotNil(t, data.Value())
	}

	assert.NoError(t, pf2.Close())
	assert.True(t, objFound)
}

func TestNewObjectPackTrailingData(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()

	fs := osfs.New(t.TempDir())
	dot := New(fs)

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	pf, err := f.Packfile()
	require.NoError(t, err)
	want, err := io.ReadAll(pf)
	require.NoError(t, err)

	_, err = w.Write(want)
	require.NoError(t, err)
	_, err = w.Write([]byte("trailing data after the pack checksum"))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	got, err := util.ReadFile(fs, fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash))
	require.NoError(t, err)
	assert.Equal(t, len(want), len(got), "saved pack size")
	assert.True(t, bytes.Equal(want, got), "saved pack differs from the one written")
}

var errTruncate = errors.New("truncate failed")

// noTruncateFS hands out temporary files that cannot be truncated.
type noTruncateFS struct{ billy.Filesystem }

func (fs noTruncateFS) TempFile(dir, prefix string) (billy.File, error) {
	f, err := fs.Filesystem.TempFile(dir, prefix)
	if err != nil {
		return nil, err
	}
	return noTruncateFile{f}, nil
}

type noTruncateFile struct{ billy.File }

func (noTruncateFile) Truncate(int64) error { return errTruncate }

func TestNewObjectPackTrailingDataTruncateError(t *testing.T) {
	t.Parallel()

	fs := noTruncateFS{memfs.New()}
	w, err := newPackWrite(fs, config.SHA1, false)
	require.NoError(t, err)

	pf, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	_, err = io.Copy(w, io.MultiReader(pf, strings.NewReader("trailing data")))
	require.NoError(t, err)

	require.ErrorIs(t, w.Close(), errTruncate)

	entries, err := fs.ReadDir("objects/pack")
	require.NoError(t, err)
	assert.Empty(t, entries, "temporary pack left behind")
}

func TestNewObjectPackTruncated(t *testing.T) {
	t.Parallel()

	pf, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	pack, err := io.ReadAll(pf)
	require.NoError(t, err)

	tests := map[string][]byte{
		"after signature":         pack[:4],
		"after version":           pack[:8],
		"no objects, no checksum": []byte("PACK\x00\x00\x00\x02\x00\x00\x00\x00"),
		"mid object":              pack[:len(pack)/2],
		"missing checksum":        pack[:len(pack)-20],
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			w, err := newPackWrite(fs, config.SHA1, false)
			require.NoError(t, err)

			_, err = w.Write(data)
			require.NoError(t, err)

			require.ErrorIs(t, w.Close(), packfile.ErrMalformedPackfile)

			entries, err := fs.ReadDir("objects/pack")
			require.NoError(t, err)
			assert.Empty(t, entries, "temporary pack left behind")
		})
	}
}

func TestNewObjectPackUnused(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	dot := New(fs)

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	assert.NoError(t, w.Close())

	info, err := fs.ReadDir("objects/pack")
	require.NoError(t, err)
	assert.Len(t, info, 0)

	// check clean up of temporary files
	info, err = fs.ReadDir("")
	require.NoError(t, err)
	for _, fi := range info {
		assert.True(t, fi.IsDir())
	}
}

func TestSyncedReader(t *testing.T) {
	t.Parallel()

	tmpw, err := util.TempFile(osfs.Default, "", "example")
	require.NoError(t, err)

	tmpr, err := osfs.Default.Open(tmpw.Name())
	require.NoError(t, err)

	defer func() {
		tmpw.Close()
		tmpr.Close()
		os.Remove(tmpw.Name())
	}()

	synced := newSyncedReader(tmpw, tmpr)

	go func() {
		for i := range 281 {
			_, err := synced.Write([]byte(strconv.Itoa(i) + "\n"))
			require.NoError(t, err)
		}

		synced.Close()
	}()

	o, err := synced.Seek(1002, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(1002), o)

	head := make([]byte, 3)
	n, err := io.ReadFull(synced, head)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "278", string(head))

	o, err = synced.Seek(1010, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(1010), o)

	n, err = io.ReadFull(synced, head)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "280", string(head))
}

// firstReadSignal closes read once its first Read has returned.
type firstReadSignal struct {
	r    io.Reader
	once sync.Once
	read chan struct{}
}

func (s *firstReadSignal) Read(p []byte) (int, error) {
	defer s.once.Do(func() { close(s.read) })
	return s.r.Read(p)
}

// The reader PackWriter parses from returns only what has been written so
// far, so the parser can see the start of a pack before all of it arrives.
func TestSyncedReaderPartialSignature(t *testing.T) {
	t.Parallel()

	pf, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	pack, err := io.ReadAll(pf)
	require.NoError(t, err)

	fs := osfs.New(t.TempDir())
	fw, err := fs.TempFile("", "pack")
	require.NoError(t, err)
	defer fw.Close()
	fr, err := fs.Open(fw.Name())
	require.NoError(t, err)
	defer fr.Close()

	synced := newSyncedReader(fw, fr)
	_, err = synced.Write(pack[:2])
	require.NoError(t, err)

	r := &firstReadSignal{r: synced, read: make(chan struct{})}
	parsed := make(chan error, 1)
	go func() {
		_, err := packfile.NewParser(r).Parse()
		parsed <- err
	}()

	// The parser has now read the two bytes written so far, and no more.
	<-r.read
	_, err = synced.Write(pack[2:])
	require.NoError(t, err)
	require.NoError(t, synced.Close())

	require.NoError(t, <-parsed)
}

func TestPackWriterUnusedNotify(t *testing.T) {
	t.Parallel()
	fs := osfs.New(t.TempDir())

	w, err := newPackWrite(fs, config.SHA1, false)
	require.NoError(t, err)

	w.Notify = func(_ plumbing.Hash, _ *idxfile.Writer) {
		t.Fatal("unexpected call to PackWriter.Notify")
	}

	assert.NoError(t, w.Close())
}

func TestPackWriterPermissions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fs   billy.Filesystem
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := fixtures.Basic().One()

			dot := New(tc.fs)
			require.NoError(t, dot.Initialize())

			w, err := dot.NewObjectPack()
			require.NoError(t, err)

			pf, pfErr := f.Packfile()
			require.NoError(t, pfErr)
			_, err = io.Copy(w, pf)
			require.NoError(t, err)

			require.NoError(t, w.Close())

			pfPath := filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.pack", f.PackfileHash))
			idxPath := filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.idx", f.PackfileHash))

			ro, err := isReadOnly(tc.fs, pfPath)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", pfPath)

			ro, err = isReadOnly(tc.fs, idxPath)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", idxPath)
		})
	}
}

func TestPackWriterExistingReadOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		fs       billy.Filesystem
		writeRev bool
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS()), false},
		{"BoundOS_Rev", osfs.New(t.TempDir(), osfs.WithBoundOS()), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := fixtures.Basic().One()

			dot := New(tc.fs)
			dot.options.WriteReverseIndex = tc.writeRev
			require.NoError(t, dot.Initialize())

			pfPath := filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.pack", f.PackfileHash))
			idxPath := filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.idx", f.PackfileHash))
			revPath := filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.rev", f.PackfileHash))

			writePack := func() {
				t.Helper()
				w, err := dot.NewObjectPack()
				require.NoError(t, err)

				pf, pfErr := f.Packfile()
				require.NoError(t, pfErr)
				_, err = io.Copy(w, pf)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}

			writePack()

			ro, err := isReadOnly(tc.fs, pfPath)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", pfPath)

			ro, err = isReadOnly(tc.fs, idxPath)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", idxPath)

			if tc.writeRev {
				ro, err = isReadOnly(tc.fs, revPath)
				require.NoError(t, err)
				assert.True(t, ro, "file %q is not read-only", revPath)
			}

			// Writing the same pack again must not fail on read-only files.
			writePack()

			// Remove .idx only, keep .pack — the next write must
			// recreate .idx without touching the existing .pack.
			require.NoError(t, tc.fs.Remove(idxPath))
			writePack()

			_, err = tc.fs.Lstat(idxPath)
			require.NoError(t, err, ".idx should have been recreated")

			ro, err = isReadOnly(tc.fs, idxPath)
			require.NoError(t, err)
			assert.True(t, ro, "recreated %q is not read-only", idxPath)

			if tc.writeRev {
				// Remove .rev only — the next write must recreate it.
				require.NoError(t, tc.fs.Remove(revPath))
				writePack()

				_, err = tc.fs.Lstat(revPath)
				require.NoError(t, err, ".rev should have been recreated")

				ro, err = isReadOnly(tc.fs, revPath)
				require.NoError(t, err)
				assert.True(t, ro, "recreated %q is not read-only", revPath)
			}
		})
	}
}

func TestPackWriterRejectsNonRegularFile(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".idx", ".pack", ".rev"} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			f := fixtures.Basic().One()
			fs := osfs.New(t.TempDir(), osfs.WithBoundOS())

			dot := New(fs)
			dot.options.WriteReverseIndex = (ext == ".rev")
			require.NoError(t, dot.Initialize())

			// Place a directory where the pack file should go.
			path := filepath.Join("objects", "pack",
				fmt.Sprintf("pack-%s%s", f.PackfileHash, ext))
			require.NoError(t, fs.MkdirAll(path, 0o755))

			w, err := dot.NewObjectPack()
			require.NoError(t, err)

			pf, pfErr := f.Packfile()
			require.NoError(t, pfErr)
			_, err = io.Copy(w, pf)
			require.NoError(t, err)
			notified := false
			w.Notify = func(_ plumbing.Hash, _ *idxfile.Writer) {
				notified = true
			}

			err = w.Close()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unexpected file type")
			assert.False(t, notified)
		})
	}
}

func TestObjectWriterPermissions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fs   billy.Filesystem
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dot := New(tc.fs)
			require.NoError(t, dot.Initialize())

			w, err := dot.NewObject()
			require.NoError(t, err)

			err = w.WriteHeader(plumbing.BlobObject, 14)
			require.NoError(t, err)

			_, err = w.Write([]byte("this is a test"))
			require.NoError(t, err)

			require.NoError(t, w.Close())

			path := filepath.Join("objects", "a8", "a940627d132695a9769df883f85992f0ff4a43")
			ro, err := isReadOnly(tc.fs, path)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", path)
		})
	}
}

// TestSyncedReaderNoLostWakeup checks that a reader that has caught up with
// the writer is woken by the next write, however the two goroutines
// interleave.
func TestSyncedReaderNoLostWakeup(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	for i := range 2000 {
		fw, err := fs.Create(fmt.Sprintf("f%d", i))
		require.NoError(t, err)
		fr, err := fs.Open(fw.Name())
		require.NoError(t, err)

		s := newSyncedReader(fw, fr)
		got := make(chan error, 1)
		go func() {
			b := make([]byte, 1)
			_, err := io.ReadFull(s, b)
			got <- err
		}()

		_, err = s.Write([]byte{'x'})
		require.NoError(t, err)

		select {
		case err := <-got:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			_ = s.Close() // release the reader goroutine
			t.Fatalf("iteration %d: reader never woke after write", i)
		}
		require.NoError(t, s.Close())
		require.NoError(t, fr.Close())
		require.NoError(t, fw.Close())
	}
}

// packBytes returns the contents of f's packfile.
func packBytes(t *testing.T, f *fixtures.Fixture) []byte {
	t.Helper()

	pf, err := f.Packfile()
	require.NoError(t, err)
	data, err := io.ReadAll(pf)
	require.NoError(t, err)
	return data
}

// assertNoPackFiles checks that objects/pack holds nothing, temporary files
// included.
func assertNoPackFiles(t *testing.T, fs billy.Filesystem) {
	t.Helper()

	entries, err := fs.ReadDir("objects/pack")
	require.NoError(t, err)
	for _, e := range entries {
		t.Errorf("objects/pack/%s left behind", e.Name())
	}
}

// TestPackWriterWriteInvalidCleansUp checks that a pack with a bad signature
// does not leave its temporary file behind. TestNewObjectPackTruncated covers
// truncated packs.
func TestPackWriterWriteInvalidCleansUp(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	w, err := New(fs).NewObjectPack()
	require.NoError(t, err)

	_, err = w.Write([]byte("KCAP\x00\x00\x00\x02\x00\x00\x00\x00"))
	require.NoError(t, err)
	require.Error(t, w.Close())

	assertNoPackFiles(t, fs)
}

var errInjected = errors.New("injected failure")

// failingFS fails Create for paths ending in createSuffix, and Rename onto
// a .pack when failRename is set.
type failingFS struct {
	billy.Filesystem
	createSuffix string
	failRename   bool
}

func (fs *failingFS) Create(name string) (billy.File, error) {
	if fs.createSuffix != "" && strings.HasSuffix(name, fs.createSuffix) {
		return nil, errInjected
	}
	return fs.Filesystem.Create(name)
}

func (fs *failingFS) Rename(from, to string) error {
	if fs.failRename && strings.HasSuffix(to, ".pack") {
		return errInjected
	}
	return fs.Filesystem.Rename(from, to)
}

// TestPackWriterSaveFailureCleansUp checks that a pack that parses but fails
// to save leaves nothing behind: not the temporary pack, nor the index,
// reverse index or marker written for it.
func TestPackWriterSaveFailureCleansUp(t *testing.T) {
	t.Parallel()

	tests := map[string]failingFS{
		"create idx":      {createSuffix: ".idx"},
		"create rev":      {createSuffix: ".rev"},
		"create promisor": {createSuffix: promisorExt},
		"rename":          {failRename: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := &failingFS{
				Filesystem:   osfs.New(t.TempDir()),
				createSuffix: tc.createSuffix,
				failRename:   tc.failRename,
			}
			dot := NewWithOptions(fs, Options{WriteReverseIndex: true})
			require.NoError(t, dot.Initialize())

			w, err := dot.NewPromisorObjectPack("")
			require.NoError(t, err)

			_, err = w.Write(packBytes(t, fixtures.Basic().One()))
			require.NoError(t, err)
			require.ErrorIs(t, w.Close(), errInjected)

			assertNoPackFiles(t, fs)
		})
	}
}
