package dotgit

import (
	"io"
	iofs "io/fs"
	"path"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
)

// Git loads every .idx in objects/pack that has a matching .pack, whatever
// the name before the extension (prepare_pack in odb/source-packed.c).
// `git maintenance run --task=loose-objects` relies on that, writing its
// packs as loose-<hash>.

func writeFixturePack(t *testing.T, fs billy.Filesystem, base string, exts ...string) plumbing.Hash {
	t.Helper()

	f := fixtures.Basic().One()
	sources := map[string]func() (billy.File, error){
		packExt: f.Packfile,
		idxExt:  f.Idx,
	}
	for _, ext := range exts {
		dst, err := fs.Create(path.Join(objectsPath, packPath, base+ext))
		require.NoError(t, err)
		if open, ok := sources[ext]; ok {
			src, err := open()
			require.NoError(t, err)
			_, err = io.Copy(dst, src)
			require.NoError(t, err)
			require.NoError(t, src.Close())
		}
		require.NoError(t, dst.Close())
	}

	return plumbing.NewHash(f.PackfileHash)
}

func packDirNames(t *testing.T, fs billy.Filesystem) []string {
	t.Helper()

	entries, err := fs.ReadDir(path.Join(objectsPath, packPath))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func forEachAccessMode(t *testing.T, fn func(t *testing.T, opts Options)) {
	t.Helper()

	for name, exclusive := range map[string]bool{"shared": false, "exclusive": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, Options{ExclusiveAccess: exclusive})
		})
	}
}

func TestLooseObjectsPackIsReadable(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		h := writeFixturePack(t, fs, "loose-"+fixtures.Basic().One().PackfileHash, packExt, idxExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, packs)

		pack, err := dot.ObjectPack(h)
		require.NoError(t, err)
		assert.Equal(t, "loose-"+h.String()+packExt, filepath.Base(pack.Name()))
		require.NoError(t, pack.Close())

		readOnly, err := dot.OpenPackForReading(h)
		require.NoError(t, err)
		assert.Equal(t, "loose-"+h.String()+packExt, filepath.Base(readOnly.Name()))
		_, err = readOnly.Stat()
		require.NoError(t, err)
		require.NoError(t, readOnly.Close())

		ph, err := dot.PackHandle(h)
		require.NoError(t, err)
		packHash, err := ph.PackHash()
		require.NoError(t, err)
		assert.Equal(t, h, packHash)

		idx, err := dot.packHandle(h)
		require.NoError(t, err)
		index, err := idx.Index()
		require.NoError(t, err)
		count, err := index.Count()
		require.NoError(t, err)
		assert.Positive(t, count)
	})
}

func TestPackNamesWithoutMatchingIndexAreIgnored(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		h := fixtures.Basic().One().PackfileHash
		writeFixturePack(t, fs, "loose-"+h, idxExt)
		writeFixturePack(t, fs, "other-"+h, packExt)
		writeFixturePack(t, fs, "pack-"+h, idxExt)
		writeFixturePack(t, fs, "loose-0123", packExt, idxExt)
		dot := NewWithOptions(fs, opts)

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Empty(t, packs)
	})
}

// `git repack` writes complete packs as .tmp-<pid>-pack-<hash> and renames
// them into place (builtin/repack.c), so they can vanish at any moment.
func TestInProgressRepackPacksAreIgnored(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		h := writeFixturePack(t, fs, ".tmp-123-pack-"+fixtures.Basic().One().PackfileHash, packExt, idxExt)
		dot := NewWithOptions(fs, opts)

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Empty(t, packs)

		_, err = dot.ObjectPack(h)
		assert.ErrorIs(t, err, ErrPackfileNotFound)
	})
}

func TestSameHashUnderTwoNamesIsListedOnceAndReadAsPack(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		hex := fixtures.Basic().One().PackfileHash
		h := writeFixturePack(t, fs, "pack-"+hex, packExt, idxExt, revExt)
		writeFixturePack(t, fs, "loose-"+hex, packExt, idxExt, promisorExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, packs)

		promisors, err := dot.PromisorObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, promisors)

		pack, err := dot.ObjectPack(h)
		require.NoError(t, err)
		assert.Equal(t, "pack-"+hex+packExt, filepath.Base(pack.Name()))
		require.NoError(t, pack.Close())
	})
}

// RepackObjects only packs what the refs reach, while the loose-objects task
// also packs objects that only the index or a reflog reach. Deleting a
// loose-<hash> pack after a repack would therefore lose those objects, so
// only pack-<hash> is removed.
func TestDeleteRemovesOnlyPackNamedCopy(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		hex := fixtures.Basic().One().PackfileHash
		h := writeFixturePack(t, fs, "pack-"+hex, packExt, idxExt, revExt)
		writeFixturePack(t, fs, "loose-"+hex, packExt, idxExt, promisorExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))
		assert.ElementsMatch(t, []string{
			"loose-" + hex + idxExt,
			"loose-" + hex + packExt,
			"loose-" + hex + promisorExt,
		}, packDirNames(t, fs))

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, packs)
	})
}

func TestDeleteLeavesLooseObjectsPackReadable(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		hex := fixtures.Basic().One().PackfileHash
		h := writeFixturePack(t, fs, "loose-"+hex, packExt, idxExt, revExt, promisorExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))
		assert.Len(t, packDirNames(t, fs), 4)

		packs, err := dot.ObjectPacks()
		require.NoError(t, err)
		assert.Equal(t, []plumbing.Hash{h}, packs)

		rev, err := dot.ObjectPackRev(h)
		require.NoError(t, err)
		assert.Equal(t, "loose-"+hex+revExt, filepath.Base(rev.Name()))
		require.NoError(t, rev.Close())
	})
}

func TestDeleteRemovesOrphanedPackNextToLooseObjectsPack(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		hex := fixtures.Basic().One().PackfileHash
		h := writeFixturePack(t, fs, "loose-"+hex, packExt, idxExt)
		writeFixturePack(t, fs, "pack-"+hex, packExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		require.NoError(t, dot.DeleteOldObjectPackAndIndex(h, time.Time{}))
		assert.ElementsMatch(t, []string{
			"loose-" + hex + idxExt,
			"loose-" + hex + packExt,
		}, packDirNames(t, fs))
	})
}

func TestMissingPackFileIsNotFoundInEitherMode(t *testing.T) {
	t.Parallel()

	forEachAccessMode(t, func(t *testing.T, opts Options) {
		fs := memfs.New()
		h := writeFixturePack(t, fs, "loose-"+fixtures.Basic().One().PackfileHash, packExt, idxExt)
		dot := NewWithOptions(fs, opts)
		defer func() { _ = dot.Close() }()

		_, err := dot.packFileSize(h, revExt)
		assert.ErrorIs(t, err, ErrPackfileNotFound)
	})
}

type readDirCounter struct {
	billy.Filesystem
	readDirs atomic.Int32
}

func (c *readDirCounter) ReadDir(path string) ([]iofs.DirEntry, error) {
	c.readDirs.Add(1)
	return c.Filesystem.ReadDir(path)
}

func TestLooseObjectsPackNameIsResolvedOnce(t *testing.T) {
	t.Parallel()

	fs := &readDirCounter{Filesystem: memfs.New()}
	h := writeFixturePack(t, fs, "loose-"+fixtures.Basic().One().PackfileHash, packExt, idxExt, revExt)
	dot := NewWithOptions(fs, Options{})
	defer func() { _ = dot.Close() }()

	for range 3 {
		for _, open := range []func(plumbing.Hash) (billy.File, error){dot.ObjectPack, dot.ObjectPackIdx, dot.ObjectPackRev} {
			f, err := open(h)
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}
	}

	assert.Equal(t, int32(1), fs.readDirs.Load())
}

// Packs written before Git 2.41, or with pack.writeReverseIndex=false, have
// no .rev, so every open of the reverse index misses.
func TestMissingReverseIndexIsLookedUpOnce(t *testing.T) {
	t.Parallel()

	fs := &readDirCounter{Filesystem: memfs.New()}
	h := writeFixturePack(t, fs, "pack-"+fixtures.Basic().One().PackfileHash, packExt, idxExt)
	dot := NewWithOptions(fs, Options{ReadReverseIndex: true})
	defer func() { _ = dot.Close() }()

	for range 3 {
		rev, err := dot.OpenPackRev(h)
		require.NoError(t, err)
		require.NoError(t, rev.Close())
	}

	assert.LessOrEqual(t, fs.readDirs.Load(), int32(1))
}

func TestRenamedPackIsFoundAgain(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	hex := fixtures.Basic().One().PackfileHash
	h := writeFixturePack(t, fs, "loose-"+hex, packExt, idxExt)
	dot := NewWithOptions(fs, Options{})
	defer func() { _ = dot.Close() }()

	pack, err := dot.ObjectPack(h)
	require.NoError(t, err)
	require.NoError(t, pack.Close())

	for _, ext := range []string{packExt, idxExt} {
		from := path.Join(objectsPath, packPath, "loose-"+hex+ext)
		to := path.Join(objectsPath, packPath, "other-"+hex+ext)
		require.NoError(t, fs.Rename(from, to))
	}

	pack, err = dot.ObjectPack(h)
	require.NoError(t, err)
	assert.Equal(t, "other-"+hex+packExt, filepath.Base(pack.Name()))
	require.NoError(t, pack.Close())
}
