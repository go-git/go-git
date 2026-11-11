package dotgit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

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

	cases := []struct {
		name string
		fs   billy.Filesystem
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS())},
	}
	// fixPermissions on Windows resolves paths against Root, which for a
	// RepositoryFilesystem is the dot-git, not the common dir holding objects.
	if runtime.GOOS != "windows" {
		cases = append(cases, struct {
			name string
			fs   billy.Filesystem
		}{"RepositoryFilesystem", NewRepositoryFilesystem(
			osfs.New(t.TempDir(), osfs.WithBoundOS()),
			osfs.New(t.TempDir(), osfs.WithBoundOS()),
		)})
	}

	for _, tc := range cases {
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

// packBytes returns the contents of f's packfile.
func packBytes(t *testing.T, f *fixtures.Fixture) []byte {
	t.Helper()

	pf, err := f.Packfile()
	require.NoError(t, err)
	data, err := io.ReadAll(pf)
	require.NoError(t, err)
	return data
}

// TestPackWriterWriteInvalidCleansUp checks that a pack with a bad signature
// does not leave its temporary file behind.
func TestPackWriterWriteInvalidCleansUp(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir())
	w, err := New(fs).NewObjectPack()
	require.NoError(t, err)

	_, err = w.Write([]byte("KCAP\x00\x00\x00\x02\x00\x00\x00\x00"))
	require.NoError(t, err)
	require.Error(t, w.Close())

	require.Empty(t, packDirNames(t, fs))
}

var errInjected = errors.New("injected failure")

// failingFS fails TempFile for one prefix and Rename onto one suffix.
type failingFS struct {
	billy.Filesystem
	tempPrefix   string
	renameSuffix string
}

func (fs *failingFS) TempFile(dir, prefix string) (billy.File, error) {
	if fs.tempPrefix != "" && prefix == fs.tempPrefix {
		return nil, errInjected
	}
	return fs.Filesystem.TempFile(dir, prefix)
}

func (fs *failingFS) Rename(from, to string) error {
	if fs.renameSuffix != "" && strings.HasSuffix(to, fs.renameSuffix) {
		return errInjected
	}
	return fs.Filesystem.Rename(from, to)
}

// TestPackWriterSaveFailureCleansUp checks that a pack that parses but fails
// to save leaves no temporary file behind, and that the index, placed last,
// is never left in place without its pack.
func TestPackWriterSaveFailureCleansUp(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]failingFS{
		"stage idx":      {tempPrefix: "tmp_idx_"},
		"stage rev":      {tempPrefix: "tmp_rev_"},
		"stage promisor": {tempPrefix: "tmp_promisor_"},
		"place promisor": {renameSuffix: ".promisor"},
		"place pack":     {renameSuffix: ".pack"},
		"place rev":      {renameSuffix: ".rev"},
		"place idx":      {renameSuffix: ".idx"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := &failingFS{
				Filesystem:   osfs.New(t.TempDir()),
				tempPrefix:   tc.tempPrefix,
				renameSuffix: tc.renameSuffix,
			}
			dot := NewWithOptions(fs, Options{WriteReverseIndex: true})
			require.NoError(t, dot.Initialize())

			w, err := dot.NewPromisorObjectPack("")
			require.NoError(t, err)
			_, err = w.Write(packBytes(t, fixtures.Basic().One()))
			require.NoError(t, err)
			require.ErrorIs(t, w.Close(), errInjected)

			var pack, idx, rev, promisor bool
			for _, n := range packDirNames(t, fs) {
				require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
				pack = pack || strings.HasSuffix(n, ".pack")
				idx = idx || strings.HasSuffix(n, ".idx")
				rev = rev || strings.HasSuffix(n, ".rev")
				promisor = promisor || strings.HasSuffix(n, ".promisor")
			}
			require.False(t, idx && !pack, "index left in place without its pack")
			require.False(t, idx && !rev, "index left in place without its reverse index")
			require.False(t, pack && !promisor, "pack left in place without its promisor marker")
		})
	}
}

// TestPackWriterSaveFailureKeepsExistingIndex checks that a writer that fails
// to save a pack leaves alone an index another writer of the same pack has
// already placed, which that writer relies on finding.
func TestPackWriterSaveFailureKeepsExistingIndex(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())

	// Save the pack once to learn its index name and contents, then stage the
	// shape a concurrent writer leaves: its index in place, its pack not yet.
	ref := osfs.New(t.TempDir())
	require.NoError(t, New(ref).Initialize())
	w, err := New(ref).NewObjectPack()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	var idxName string
	for _, n := range packDirNames(t, ref) {
		if strings.HasSuffix(n, ".idx") {
			idxName = n
		}
	}
	require.NotEmpty(t, idxName)
	idxData, err := util.ReadFile(ref, "objects/pack/"+idxName)
	require.NoError(t, err)

	fs := &failingFS{Filesystem: osfs.New(t.TempDir()), renameSuffix: ".pack"}
	dot := New(fs)
	require.NoError(t, dot.Initialize())
	require.NoError(t, util.WriteFile(fs, "objects/pack/"+idxName, idxData, 0o444))

	w, err = dot.NewObjectPack()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.ErrorIs(t, w.Close(), errInjected)

	got, err := util.ReadFile(fs, "objects/pack/"+idxName)
	require.NoError(t, err, "the other writer's index was removed")
	require.Equal(t, idxData, got)
}

// removeFailingFS fails Remove for names with one prefix, and lets hooks run
// before Lstat, Open and Rename.
type removeFailingFS struct {
	billy.Filesystem
	removePrefix string
	beforeLstat  func(name string)
	beforeOpen   func(name string)
	beforeRename func(from, to string) error
}

func (fs *removeFailingFS) Lstat(name string) (os.FileInfo, error) {
	if fs.beforeLstat != nil {
		fs.beforeLstat(name)
	}
	return fs.Filesystem.Lstat(name)
}

func (fs *removeFailingFS) Open(name string) (billy.File, error) {
	if fs.beforeOpen != nil {
		fs.beforeOpen(name)
	}
	return fs.Filesystem.Open(name)
}

func (fs *removeFailingFS) Remove(name string) error {
	if fs.removePrefix != "" && strings.HasPrefix(filepath.Base(name), fs.removePrefix) {
		return errInjected
	}
	return fs.Filesystem.Remove(name)
}

func (fs *removeFailingFS) Rename(from, to string) error {
	if fs.beforeRename != nil {
		if err := fs.beforeRename(from, to); err != nil {
			return err
		}
	}
	return fs.Filesystem.Rename(from, to)
}

// savedPackFiles saves data with a fresh writer and returns the contents of
// each file it placed in objects/pack, by name.
func savedPackFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()

	fs := osfs.New(t.TempDir())
	require.NoError(t, New(fs).Initialize())
	w, err := New(fs).NewObjectPack()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	files := make(map[string][]byte)
	for _, n := range packDirNames(t, fs) {
		b, err := util.ReadFile(fs, "objects/pack/"+n)
		require.NoError(t, err)
		files[n] = b
	}
	return files
}

// TestPackWriterSaveCarriesOnWhenDiscardFails checks that a temporary file
// whose final name is already taken, and which cannot be removed, does not
// stop the save: as in Git, that only warrants a warning.
func TestPackWriterSaveCarriesOnWhenDiscardFails(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir()), removePrefix: "tmp_pack_"}
	dot := New(fs)
	require.NoError(t, dot.Initialize())
	// The pack is in place, as another writer of it leaves it before its index.
	for n, b := range ref {
		if strings.HasSuffix(n, ".pack") {
			require.NoError(t, util.WriteFile(fs, "objects/pack/"+n, b, 0o444))
		}
	}

	w, err := dot.NewObjectPack()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	for n, b := range ref {
		got, err := util.ReadFile(fs, "objects/pack/"+n)
		require.NoError(t, err, "%s not placed", n)
		require.Equal(t, b, got)
	}
}

// TestPackWriterSaveFinalAppearsBeforeRename checks that a final name another
// writer of the same pack places between the existence check and the rename
// counts as already in place, rather than failing the save. On Windows a
// rename over an existing file fails, which this simulates.
func TestPackWriterSaveFinalAppearsBeforeRename(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for _, suffix := range []string{".pack", ".idx"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
			fs.beforeRename = func(_, to string) error {
				if !strings.HasSuffix(to, suffix) {
					return nil
				}
				require.NoError(t, util.WriteFile(fs.Filesystem, to, ref[filepath.Base(to)], 0o444))
				return errInjected
			}
			dot := New(fs)
			require.NoError(t, dot.Initialize())

			w, err := dot.NewObjectPack()
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())

			names := packDirNames(t, fs)
			for _, n := range names {
				require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
			}
			require.Len(t, names, len(ref))
		})
	}
}

// TestPackWriterSaveDifferentFinalAppearsBeforeRename checks that a final
// name another writer places between the check of every final and the rename,
// but with other contents, fails the save as Git's collision check does, and
// is left in place.
func TestPackWriterSaveDifferentFinalAppearsBeforeRename(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for _, suffix := range []string{".pack", ".idx"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			var planted string
			fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
			fs.beforeRename = func(_, to string) error {
				if !strings.HasSuffix(to, suffix) {
					return nil
				}
				planted = filepath.Base(to)
				b := ref[planted]
				require.NoError(t, util.WriteFile(fs.Filesystem, to, b[:len(b)/2], 0o444))
				return errInjected
			}
			dot := New(fs)
			require.NoError(t, dot.Initialize())

			w, err := dot.NewObjectPack()
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.ErrorContains(t, w.Close(), "differ in contents")

			require.NotEmpty(t, planted)
			got, err := util.ReadFile(fs, "objects/pack/"+planted)
			require.NoError(t, err)
			require.Len(t, got, len(ref[planted])/2, "the other writer's file was replaced")

			names := packDirNames(t, fs)
			for _, n := range names {
				require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
			}
			if suffix == ".pack" {
				// The pack is placed first, so nothing else is.
				require.Equal(t, []string{planted}, names)
			}
		})
	}
}

// TestPackWriterSaveFinalVanishesDuringCheck checks that a final that is in
// place when the save looks for it, but goes away before it is compared, as a
// repack removes a redundant pack, is placed anew. A promisor pack placed that
// way gets its marker, as any pack this writer places does.
func TestPackWriterSaveFinalVanishesDuringCheck(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for _, suffix := range []string{".pack", ".idx"} {
		for _, promisor := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/promisor=%t", suffix, promisor), func(t *testing.T) {
				t.Parallel()

				var removed bool
				fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
				fs.beforeOpen = func(name string) {
					if removed || !strings.HasSuffix(name, suffix) ||
						strings.HasPrefix(filepath.Base(name), "tmp_") {
						return
					}
					removed = true
					require.NoError(t, fs.Filesystem.Remove(name))
				}
				dot := New(fs)
				require.NoError(t, dot.Initialize())
				for n, b := range ref {
					if strings.HasSuffix(n, suffix) {
						require.NoError(t, util.WriteFile(fs.Filesystem, "objects/pack/"+n, b, 0o444))
					}
				}

				newPack := dot.NewObjectPack
				if promisor {
					newPack = func() (*PackWriter, error) { return dot.NewPromisorObjectPack("") }
				}
				w, err := newPack()
				require.NoError(t, err)
				_, err = w.Write(data)
				require.NoError(t, err)
				require.NoError(t, w.Close())
				require.True(t, removed, "the final was never compared")

				want := len(ref)
				if promisor {
					want++
				}
				names := packDirNames(t, fs)
				for _, n := range names {
					require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
				}
				require.Len(t, names, want)
				for n, b := range ref {
					got, err := util.ReadFile(fs, "objects/pack/"+n)
					require.NoError(t, err, "%s not placed", n)
					require.Equal(t, b, got)
				}

				promisors, err := dot.PromisorObjectPacks()
				require.NoError(t, err)
				if promisor {
					require.Len(t, promisors, 1, "the pack placed anew has no promisor marker")
				} else {
					require.Empty(t, promisors)
				}
			})
		}
	}
}

// isFinal reports whether name is the final name of a file with suffix, and
// not a temporary file.
func isFinal(name, suffix string) bool {
	return strings.HasSuffix(name, suffix) && !strings.HasPrefix(filepath.Base(name), "tmp_")
}

// TestPackWriterSaveFinalVanishesBeforeCompare checks that a final another
// writer places between the existence check and the rename, and that goes
// away again before it is compared, is placed anew, as Git retries on
// CHECK_COLLISION_DEST_VANISHED, rather than failing the save.
func TestPackWriterSaveFinalVanishesBeforeCompare(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for _, suffix := range []string{".pack", ".idx"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			var planted, removed bool
			fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
			fs.beforeRename = func(_, to string) error {
				if planted || !strings.HasSuffix(to, suffix) {
					return nil
				}
				planted = true
				require.NoError(t, util.WriteFile(fs.Filesystem, to, ref[filepath.Base(to)], 0o444))
				return errInjected
			}
			fs.beforeOpen = func(name string) {
				if !planted || removed || !isFinal(name, suffix) {
					return
				}
				removed = true
				require.NoError(t, fs.Filesystem.Remove(name))
			}
			dot := New(fs)
			require.NoError(t, dot.Initialize())

			w, err := dot.NewObjectPack()
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			require.True(t, removed, "the final was never compared")

			names := packDirNames(t, fs)
			for _, n := range names {
				require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
			}
			require.Len(t, names, len(ref))
			for n, b := range ref {
				got, err := util.ReadFile(fs, "objects/pack/"+n)
				require.NoError(t, err, "%s not placed", n)
				require.Equal(t, b, got)
			}
		})
	}
}

// TestPackWriterSaveFinalKeepsVanishing checks that a final that goes away
// every time it is compared fails the save after a bounded number of
// attempts, as in Git, and leaves no temporary file behind.
func TestPackWriterSaveFinalKeepsVanishing(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	var renames int
	fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
	fs.beforeRename = func(_, to string) error {
		if !strings.HasSuffix(to, ".idx") {
			return nil
		}
		renames++
		require.NoError(t, util.WriteFile(fs.Filesystem, to, ref[filepath.Base(to)], 0o444))
		return errInjected
	}
	fs.beforeOpen = func(name string) {
		if isFinal(name, ".idx") {
			require.NoError(t, fs.Filesystem.Remove(name))
		}
	}
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewObjectPack()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.ErrorContains(t, w.Close(), "repeatedly vanishing")
	// Git gives up on the seventh vanish: "retries++ > 5" lets six
	// retries through after the first attempt.
	require.Equal(t, 7, renames)

	for _, n := range packDirNames(t, fs) {
		require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
	}
}

// TestPackWriterSaveComparesFinalReplacedAfterCheck checks that a final the
// check of every final found to match, but that goes away and is replaced
// with other contents before it is placed, is compared again and fails the
// save, rather than being kept on the strength of the earlier match.
func TestPackWriterSaveComparesFinalReplacedAfterCheck(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for _, suffix := range []string{".pack", ".idx"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			var lstats int
			var planted string
			fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
			// The first look at the final is the check of every final, which
			// finds it matching; it is gone by the second, from place.
			fs.beforeLstat = func(name string) {
				if !isFinal(name, suffix) {
					return
				}
				lstats++
				if lstats == 2 {
					require.NoError(t, fs.Filesystem.Remove(name))
				}
			}
			// Another file takes its place before the rename.
			fs.beforeRename = func(_, to string) error {
				if planted != "" || !strings.HasSuffix(to, suffix) {
					return nil
				}
				planted = filepath.Base(to)
				b := ref[planted]
				require.NoError(t, util.WriteFile(fs.Filesystem, to, b[:len(b)/2], 0o444))
				return errInjected
			}
			dot := New(fs)
			require.NoError(t, dot.Initialize())
			for n, b := range ref {
				if strings.HasSuffix(n, suffix) {
					require.NoError(t, util.WriteFile(fs.Filesystem, "objects/pack/"+n, b, 0o444))
				}
			}

			w, err := dot.NewObjectPack()
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.ErrorContains(t, w.Close(), "differ in contents")

			require.NotEmpty(t, planted)
			got, err := util.ReadFile(fs, "objects/pack/"+planted)
			require.NoError(t, err)
			require.Len(t, got, len(ref[planted])/2, "the other writer's file was replaced")
		})
	}
}

// TestPackWriterSavePromisorMarkerFollowsPlacement checks that a promisor
// writer marks the pack when it places the pack itself, and only then: a
// pack that the check of every final found in place but that went away
// before it was placed is placed anew and marked, and one another writer
// placed after that check, which place then finds in place, is left
// unmarked, as an existing pack is.
func TestPackWriterSavePromisorMarkerFollowsPlacement(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())
	ref := savedPackFiles(t, data)

	for name, tc := range map[string]struct {
		inPlace bool // the pack is in place for the check of every final
		marked  bool
	}{
		"vanishes after check": {inPlace: true, marked: true},
		"appears after check":  {inPlace: false, marked: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var lstats int
			fs := &removeFailingFS{Filesystem: osfs.New(t.TempDir())}
			fs.beforeLstat = func(name string) {
				if !isFinal(name, ".pack") {
					return
				}
				lstats++
				if lstats != 2 {
					return
				}
				// Between the check of every final and place.
				if tc.inPlace {
					require.NoError(t, fs.Filesystem.Remove(name))
				} else {
					require.NoError(t, util.WriteFile(fs.Filesystem, name, ref[filepath.Base(name)], 0o444))
				}
			}
			dot := New(fs)
			require.NoError(t, dot.Initialize())
			if tc.inPlace {
				for n, b := range ref {
					if strings.HasSuffix(n, ".pack") {
						require.NoError(t, util.WriteFile(fs.Filesystem, "objects/pack/"+n, b, 0o444))
					}
				}
			}

			w, err := dot.NewPromisorObjectPack("")
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			require.GreaterOrEqual(t, lstats, 2, "the pack was not looked up by place")

			for _, n := range packDirNames(t, fs) {
				require.False(t, strings.HasPrefix(n, "tmp_"), "temporary file %s left behind", n)
			}
			promisors, err := dot.PromisorObjectPacks()
			require.NoError(t, err)
			if tc.marked {
				require.Len(t, promisors, 1, "the pack placed anew has no promisor marker")
			} else {
				require.Empty(t, promisors, "the pack another writer placed was marked")
			}
		})
	}
}

// closeFailingFile fails Close after closing the file it wraps.
type closeFailingFile struct {
	billy.File
}

func (f closeFailingFile) Close() error {
	_ = f.File.Close()
	return errInjected
}

// TestPackWriterCloseFailure checks that a failure to close either handle on
// the temporary pack is returned, removes the temporary pack and places
// nothing.
func TestPackWriterCloseFailure(t *testing.T) {
	t.Parallel()

	data := packBytes(t, fixtures.Basic().One())

	for name, wrap := range map[string]func(w *PackWriter){
		"reader": func(w *PackWriter) { w.fr = closeFailingFile{w.fr} },
		"writer": func(w *PackWriter) { w.fw = closeFailingFile{w.fw} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			require.NoError(t, New(fs).Initialize())
			w, err := newPackWrite(fs, config.SHA1, false)
			require.NoError(t, err)
			wrap(w)

			_, err = w.Write(data)
			require.NoError(t, err)
			require.ErrorIs(t, w.Close(), errInjected)
			require.Empty(t, packDirNames(t, fs))
		})
	}
}

// TestPackWriterRefusesDifferentExistingIndex checks that an index already
// under the pack's final name, but not the one this writer would write, such
// as a truncated one a failed save left, fails the save as Git's collision
// check does, and is left for the user to repair.
func TestPackWriterRefusesDifferentExistingIndex(t *testing.T) {
	t.Parallel()

	for _, promisor := range []bool{false, true} {
		t.Run(fmt.Sprintf("promisor=%t", promisor), func(t *testing.T) {
			t.Parallel()

			data := packBytes(t, fixtures.Basic().One())
			ref := savedPackFiles(t, data)

			fs := osfs.New(t.TempDir())
			dot := New(fs)
			require.NoError(t, dot.Initialize())
			var idxName string
			for n, b := range ref {
				if strings.HasSuffix(n, ".idx") {
					idxName = n
					require.NoError(t, util.WriteFile(fs, "objects/pack/"+n, b[:len(b)/2], 0o444))
				}
			}
			require.NotEmpty(t, idxName)

			newPack := dot.NewObjectPack
			if promisor {
				newPack = func() (*PackWriter, error) { return dot.NewPromisorObjectPack("") }
			}
			w, err := newPack()
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
			err = w.Close()
			require.ErrorContains(t, err, "differ in contents")
			// The error names the file to remove to let the save through.
			require.ErrorContains(t, err, fmt.Sprintf("remove %q", fs.Join("objects", "pack", idxName)))

			got, err := util.ReadFile(fs, "objects/pack/"+idxName)
			require.NoError(t, err)
			require.Len(t, got, len(ref[idxName])/2, "the existing index was replaced")
			// Nothing is placed: the pack would be listed through the
			// index that does not match it.
			require.Equal(t, []string{idxName}, packDirNames(t, fs))
			packs, err := dot.ObjectPacks()
			require.NoError(t, err)
			require.Empty(t, packs)
		})
	}
}

// sameContents compares whole files, past the first chunk it reads and by
// length.
func TestSameContents(t *testing.T) {
	t.Parallel()

	const chunk = 32 * 1024
	big := bytes.Repeat([]byte("a"), 2*chunk+10)
	differLate := bytes.Clone(big)
	differLate[chunk+5] = 'b'
	for _, tc := range []struct {
		name string
		a, b []byte
		same bool
	}{
		{name: "empty", same: true},
		{name: "equal", a: big, b: big, same: true},
		{name: "differ after the first chunk", a: big, b: differLate},
		{name: "longer by a chunk", a: big[:chunk], b: big[:2*chunk]},
		{name: "longer by a byte", a: big[:chunk], b: big[:chunk+1]},
		{name: "empty and not", b: []byte("a")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			require.NoError(t, util.WriteFile(fs, "a", tc.a, 0o644))
			require.NoError(t, util.WriteFile(fs, "b", tc.b, 0o644))
			same, err := sameContents(fs, "a", "b")
			require.NoError(t, err)
			require.Equal(t, tc.same, same)
			same, err = sameContents(fs, "b", "a")
			require.NoError(t, err)
			require.Equal(t, tc.same, same)
		})
	}
}
