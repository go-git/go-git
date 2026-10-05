package filesystem

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
)

// Git rescans the pack directory when an object read misses, and looks in the
// packs again, so a pack written by another process after the first read is
// found:
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/odb.c#L564-L578
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/odb/source-packed.c#L89-L95
func TestEncodedObjectFindsPackAddedAfterIndexLoad(t *testing.T) {
	t.Parallel()

	packFixture := fixtures.ByTag("packfile").ByTag("standalone").One()
	packFile, err := packFixture.Packfile()
	require.NoError(t, err)
	t.Cleanup(func() { _ = packFile.Close() })
	idxFile, err := packFixture.Idx()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idxFile.Close() })
	commitInStandalonePack := plumbing.NewHash("a771b1e94141480861332fd0e4684d33071306c6")

	fs, err := fixtures.Basic().One().DotGit()
	require.NoError(t, err)
	storer := NewStorage(fs, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = storer.Close() })

	_, err = storer.EncodedObject(plumbing.CommitObject, commitInStandalonePack)
	require.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	packName := fmt.Sprintf("pack-%s", packFixture.PackfileHash)
	require.NoError(t, copyFile(fs, filepath.Join("objects", "pack", packName+".pack"), packFile))
	require.NoError(t, copyFile(fs, filepath.Join("objects", "pack", packName+".idx"), idxFile))

	size, err := storer.EncodedObjectSize(commitInStandalonePack)
	require.NoError(t, err)
	assert.Positive(t, size)

	obj, err := storer.EncodedObject(plumbing.CommitObject, commitInStandalonePack)
	require.NoError(t, err)
	assert.Equal(t, commitInStandalonePack, obj.Hash())
	assert.NoError(t, storer.HasEncodedObject(commitInStandalonePack))
}

// An existence check is quick in git: it does not rescan the pack directory
// unless the caller asks for ODB_HAS_OBJECT_RECHECK_PACKED.
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/odb.c#L800-L813
func TestHasEncodedObjectDoesNotRescanPacks(t *testing.T) {
	t.Parallel()

	packFixture := fixtures.ByTag("packfile").ByTag("standalone").One()
	packFile, err := packFixture.Packfile()
	require.NoError(t, err)
	t.Cleanup(func() { _ = packFile.Close() })
	idxFile, err := packFixture.Idx()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idxFile.Close() })
	commitInStandalonePack := plumbing.NewHash("a771b1e94141480861332fd0e4684d33071306c6")

	fs, err := fixtures.Basic().One().DotGit()
	require.NoError(t, err)
	storer := NewStorage(fs, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = storer.Close() })

	require.ErrorIs(t, storer.HasEncodedObject(commitInStandalonePack), plumbing.ErrObjectNotFound)

	packName := fmt.Sprintf("pack-%s", packFixture.PackfileHash)
	require.NoError(t, copyFile(fs, filepath.Join("objects", "pack", packName+".pack"), packFile))
	require.NoError(t, copyFile(fs, filepath.Join("objects", "pack", packName+".idx"), idxFile))

	assert.ErrorIs(t, storer.HasEncodedObject(commitInStandalonePack), plumbing.ErrObjectNotFound)
}

// A repack by another process moves loose objects into a new pack and deletes
// the pack the index was loaded from. Fixes #2242.
func TestEncodedObjectAfterExternalRepack(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		args = append([]string{
			"-C", dir, "-c", "user.name=a", "-c", "user.email=a@example.com",
			"-c", "maintenance.auto=false", "-c", "gc.auto=0",
		}, args...)
		out, err := gitenv.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "packed")
	git("repack", "-adq")
	packed := plumbing.NewHash(git("rev-parse", "HEAD"))
	git("commit", "-q", "--allow-empty", "-m", "loose")
	loose := plumbing.NewHash(git("rev-parse", "HEAD"))

	storer := NewStorage(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = storer.Close() })
	require.NoError(t, storer.HasEncodedObject(packed))
	require.NoError(t, storer.HasEncodedObject(loose))

	git("repack", "-adq")

	for _, h := range []plumbing.Hash{packed, loose} {
		_, err := storer.EncodedObject(plumbing.CommitObject, h)
		assert.NoError(t, err, "EncodedObject(%s)", h)
		_, err = storer.EncodedObjectSize(h)
		assert.NoError(t, err, "EncodedObjectSize(%s)", h)
	}
}

// A pack whose handle is cached but whose descriptors were released is
// reopened on the next read, and a repack may have deleted it by then. The
// reopen reports dotgit.ErrPackfileNotFound rather than os.ErrNotExist.
func TestEncodedObjectAfterExternalRepackOfReleasedPack(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		args = append([]string{
			"-C", dir, "-c", "user.name=a", "-c", "user.email=a@example.com",
			"-c", "maintenance.auto=false", "-c", "gc.auto=0",
		}, args...)
		out, err := gitenv.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "packed")
	git("repack", "-adq")
	packed := plumbing.NewHash(git("rev-parse", "HEAD"))

	// No object cache, so that every read goes to the pack, and an index held
	// in memory, so that it still routes there once the descriptors are gone.
	storer := NewStorageWithOptions(osfs.New(filepath.Join(dir, ".git")), cache.NewObjectLRU(0),
		Options{UseInMemoryIdx: true})
	t.Cleanup(func() { _ = storer.Close() })
	_, err := storer.EncodedObject(plumbing.CommitObject, packed)
	require.NoError(t, err)
	require.NoError(t, storer.CloseIdleDescriptors())

	git("commit", "-q", "--allow-empty", "-m", "loose")
	git("repack", "-adq")

	_, err = storer.EncodedObjectSize(packed)
	assert.NoError(t, err, "EncodedObjectSize")
	_, err = storer.EncodedObject(plumbing.CommitObject, packed)
	assert.NoError(t, err, "EncodedObject")
}
