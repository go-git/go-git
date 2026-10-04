package dotgit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

// counterHash returns the hash encoding n, so that a test can count updates
// in the value of a reference.
func counterHash(n uint64) plumbing.Hash {
	return plumbing.NewHash(fmt.Sprintf("%040x", n))
}

// refTestFilesystems are the filesystems the reference tests run on.
var refTestFilesystems = map[string]func(t *testing.T) billy.Filesystem{
	"memfs":  func(*testing.T) billy.Filesystem { return memfs.New() },
	"osfs":   func(t *testing.T) billy.Filesystem { return osfs.New(t.TempDir()) },
	"norwfs": func(*testing.T) billy.Filesystem { return &norwfs{memfs.New()} },
}

func TestSetRefChecksOld(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	a, b, c := counterHash(1), counterHash(2), counterHash(3)
	ref := func(h plumbing.Hash) *plumbing.Reference { return plumbing.NewHashReference(name, h) }

	tests := []struct {
		name string
		// loose and packed are the values of the reference before the
		// update, if not zero.
		loose, packed plumbing.Hash
		old           *plumbing.Reference
		wantErr       error
		// want is the value of the reference after the update, if not zero.
		want plumbing.Hash
	}{
		{name: "loose, old matches", loose: a, old: ref(a), want: b},
		{name: "loose, old is stale", loose: a, old: ref(c), wantErr: storage.ErrReferenceHasChanged, want: a},
		{name: "loose, no old", loose: a, want: b},
		{name: "packed, old matches", packed: a, old: ref(a), want: b},
		{name: "packed, old is stale", packed: a, old: ref(c), wantErr: storage.ErrReferenceHasChanged, want: a},
		{name: "packed, no old", packed: a, want: b},
		{name: "loose over packed, old matches loose", loose: a, packed: c, old: ref(a), want: b},
		{name: "loose over packed, old matches packed", loose: a, packed: c, old: ref(c), wantErr: storage.ErrReferenceHasChanged, want: a},
		{name: "missing, old", old: ref(a), wantErr: plumbing.ErrReferenceNotFound},
		{name: "missing, no old", want: b},
		{name: "loose, zero old", loose: a, old: ref(plumbing.ZeroHash), wantErr: storage.ErrReferenceHasChanged, want: a},
		{name: "packed, zero old", packed: a, old: ref(plumbing.ZeroHash), wantErr: storage.ErrReferenceHasChanged, want: a},
		{name: "missing, zero old", old: ref(plumbing.ZeroHash), want: b},
	}

	for fsName, newFS := range refTestFilesystems {
		for _, tc := range tests {
			t.Run(fsName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				fs := newFS(t)
				if !tc.loose.IsZero() {
					require.NoError(t, util.WriteFile(fs, name.String(), []byte(tc.loose.String()+"\n"), 0o644))
				}
				if !tc.packed.IsZero() {
					require.NoError(t, util.WriteFile(fs, packedRefsPath, []byte(tc.packed.String()+" "+name.String()+"\n"), 0o644))
				}
				dir := New(fs)

				err := dir.SetRef(ref(b), tc.old)
				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr)
				} else {
					require.NoError(t, err)
				}

				if tc.wantErr != nil {
					// A rejected update leaves the loose reference as it was:
					// in particular, it doesn't create one.
					content, err := util.ReadFile(fs, name.String())
					if tc.loose.IsZero() {
						require.ErrorIs(t, err, os.ErrNotExist)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tc.loose.String()+"\n", string(content))
					}
				}

				_, err = fs.Stat(name.String() + refLockSuffix)
				assert.ErrorIs(t, err, os.ErrNotExist, "the lock file must be removed")

				refs, err := dir.Refs()
				require.NoError(t, err)

				got, err := dir.Ref(name)
				if tc.want.IsZero() {
					require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
					assert.Nil(t, findReference(refs, name.String()))
				} else {
					require.NoError(t, err)
					assert.Equal(t, tc.want, got.Hash())
					assert.Equal(t, ref(tc.want), findReference(refs, name.String()))
				}
			})
		}
	}
}

func TestRefLockHeld(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	current, held := counterHash(1), counterHash(2)

	for fsName, newFS := range refTestFilesystems {
		t.Run(fsName, func(t *testing.T) {
			t.Parallel()
			fs := newFS(t)
			require.NoError(t, util.WriteFile(fs, name.String(), []byte(current.String()+"\n"), 0o644))
			// Another writer, git maybe, holds the lock while updating the
			// reference to held.
			lockPath := name.String() + refLockSuffix
			require.NoError(t, util.WriteFile(fs, lockPath, []byte(held.String()+"\n"), 0o644))
			dir := New(fs)

			start := time.Now()
			err := dir.SetRef(plumbing.NewHashReference(name, counterHash(3)), nil)
			require.ErrorIs(t, err, os.ErrExist)
			assert.GreaterOrEqual(t, time.Since(start), refLockTimeout, "SetRef must wait for the lock")

			err = dir.RemoveRef(name)
			require.ErrorIs(t, err, os.ErrExist)

			// The lock belongs to the other writer: it is left alone, and so
			// is the reference.
			content, err := util.ReadFile(fs, lockPath)
			require.NoError(t, err)
			assert.Equal(t, held.String()+"\n", string(content))

			ref, err := dir.Ref(name)
			require.NoError(t, err)
			assert.Equal(t, current, ref.Hash())

			refs, err := dir.Refs()
			require.NoError(t, err)
			assert.Equal(t, []*plumbing.Reference{plumbing.NewHashReference(name, current)}, refs)
		})
	}
}

func TestPackedRefsLockHeld(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	packed := counterHash(1)
	content := packed.String() + " " + name.String() + "\n"
	lockPath := packedRefsPath + refLockSuffix

	for fsName, newFS := range refTestFilesystems {
		t.Run(fsName, func(t *testing.T) {
			t.Parallel()
			fs := newFS(t)
			require.NoError(t, util.WriteFile(fs, packedRefsPath, []byte(content), 0o644))
			// Another writer, git maybe, holds the lock while rewriting
			// packed-refs.
			require.NoError(t, util.WriteFile(fs, lockPath, nil, 0o644))
			dir := New(fs)
			require.NoError(t, dir.SetRef(plumbing.NewHashReference("refs/heads/loose", packed), nil))

			require.ErrorIs(t, dir.RemoveRef(name), os.ErrExist)
			require.ErrorIs(t, dir.PackRefs(), os.ErrExist)

			// Readers don't take the lock, so they don't wait for it.
			ref, err := dir.Ref(name)
			require.NoError(t, err)
			assert.Equal(t, packed, ref.Hash())

			got, err := util.ReadFile(fs, packedRefsPath)
			require.NoError(t, err)
			assert.Equal(t, content, string(got))
			_, err = fs.Stat(lockPath)
			assert.NoError(t, err, "the lock belongs to the other writer")
		})
	}
}

// In a linked worktree, packed-refs and its lock file are in the common
// directory, where git has them: the references packed there stay visible to
// the main worktree.
func TestPackedRefsLockInCommonDir(t *testing.T) {
	t.Parallel()

	common, worktree := memfs.New(), memfs.New()
	dir := New(NewRepositoryFilesystem(worktree, common))
	ref := plumbing.NewHashReference("refs/heads/main", counterHash(1))
	require.NoError(t, dir.SetRef(ref, nil))

	lockPath := packedRefsPath + refLockSuffix
	require.NoError(t, util.WriteFile(common, lockPath, nil, 0o644))
	require.ErrorIs(t, dir.PackRefs(), os.ErrExist)
	require.NoError(t, common.Remove(lockPath))

	require.NoError(t, dir.PackRefs())
	_, err := worktree.Stat(packedRefsPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	got, err := New(common).Ref(ref.Name())
	require.NoError(t, err)
	assert.Equal(t, ref, got)
}

// releasingLockFS releases the lock of a reference, as its holder would,
// right after a writer first finds it held.
type releasingLockFS struct {
	billy.Filesystem
	lockPath string
	waited   atomic.Bool
}

func (f *releasingLockFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	file, err := f.Filesystem.OpenFile(name, flag, perm)
	if filepath.ToSlash(name) == f.lockPath && errors.Is(err, os.ErrExist) && !f.waited.Swap(true) {
		if err := f.Remove(name); err != nil {
			return nil, err
		}
	}
	return file, err
}

func TestRefLockWaitsForRelease(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	lockPath := name.String() + refLockSuffix

	fs := &releasingLockFS{Filesystem: memfs.New(), lockPath: lockPath}
	require.NoError(t, util.WriteFile(fs, lockPath, nil, 0o644))
	dir := New(fs)

	want := plumbing.NewHashReference(name, counterHash(1))
	require.NoError(t, dir.SetRef(want, nil))
	assert.True(t, fs.waited.Load(), "SetRef must have found the lock held")

	got, err := dir.Ref(name)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// deletePendingFS refuses to create a lock file once, as Windows does while
// the deletion of the previous one is pending.
type deletePendingFS struct {
	billy.Filesystem
	lockPath string
	refused  atomic.Bool
}

func (f *deletePendingFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	if filepath.ToSlash(name) == f.lockPath && !f.refused.Swap(true) {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrPermission}
	}
	return f.Filesystem.OpenFile(name, flag, perm)
}

func TestRefLockWaitsForPendingDelete(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses access to a file whose deletion is pending")
	}

	const name = plumbing.ReferenceName("refs/heads/main")
	fs := &deletePendingFS{Filesystem: memfs.New(), lockPath: name.String() + refLockSuffix}
	dir := New(fs)

	want := plumbing.NewHashReference(name, counterHash(1))
	require.NoError(t, dir.SetRef(want, nil))
	assert.True(t, fs.refused.Load(), "SetRef must have been refused the lock")

	got, err := dir.Ref(name)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRefsSkipsLockFiles(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	mainRef := plumbing.NewHashReference("refs/heads/main", counterHash(1))
	files := map[string]string{
		"refs/heads/main": mainRef.Hash().String() + "\n",
		// A lock file is empty until written, and holds a value that is not
		// the reference's yet once written.
		"refs/heads/main.lock":  "",
		"refs/heads/other.lock": counterHash(2).String() + "\n",
	}
	for name, content := range files {
		require.NoError(t, util.WriteFile(fs, name, []byte(content), 0o644))
	}
	dir := New(fs)

	refs, err := dir.Refs()
	require.NoError(t, err)
	assert.Equal(t, []*plumbing.Reference{mainRef}, refs)

	count, err := dir.CountLooseRefs()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRemoveRefRemovesEmptyParents(t *testing.T) {
	t.Parallel()

	for fsName, newFS := range refTestFilesystems {
		t.Run(fsName, func(t *testing.T) {
			t.Parallel()
			fs := newFS(t)
			dir := New(fs)
			for _, name := range []plumbing.ReferenceName{"refs/heads/a/b/c", "refs/heads/a/d"} {
				require.NoError(t, dir.SetRef(plumbing.NewHashReference(name, counterHash(1)), nil))
			}
			exists := func(path string) bool {
				_, err := fs.Stat(path)
				return err == nil
			}

			require.NoError(t, dir.RemoveRef("refs/heads/a/b/c"))
			assert.False(t, exists("refs/heads/a/b"))
			assert.True(t, exists("refs/heads/a"), "refs/heads/a holds refs/heads/a/d")

			require.NoError(t, dir.RemoveRef("refs/heads/a/d"))
			assert.False(t, exists("refs/heads/a"))
			assert.True(t, exists("refs/heads"))

			// Removing a reference that doesn't exist leaves no directory
			// created for its lock file, which would keep a reference of
			// the name of that directory from being created.
			require.NoError(t, dir.RemoveRef("refs/heads/e/f"))
			assert.False(t, exists("refs/heads/e"))
			require.NoError(t, dir.SetRef(plumbing.NewHashReference("refs/heads/e", counterHash(1)), nil))
		})
	}
}

func TestSetRefRejectedRemovesEmptyParents(t *testing.T) {
	t.Parallel()

	for fsName, newFS := range refTestFilesystems {
		t.Run(fsName, func(t *testing.T) {
			t.Parallel()
			fs := newFS(t)
			dir := New(fs)
			nested := plumbing.ReferenceName("refs/heads/a/b")

			err := dir.SetRef(plumbing.NewHashReference(nested, counterHash(2)), plumbing.NewHashReference(nested, counterHash(1)))
			require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)

			_, err = fs.Stat("refs/heads/a")
			assert.ErrorIs(t, err, os.ErrNotExist)
			require.NoError(t, dir.SetRef(plumbing.NewHashReference("refs/heads/a", counterHash(1)), nil))
		})
	}
}

// noRenameFS is a filesystem without rename.
type noRenameFS struct {
	billy.Filesystem
}

func (noRenameFS) Rename(string, string) error {
	return billy.ErrNotSupported
}

// noImplicitDirFS is a filesystem that doesn't create the missing directories
// of a file it creates.
type noImplicitDirFS struct {
	billy.Filesystem
}

func (f noImplicitDirFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	if flag&os.O_CREATE != 0 {
		if _, err := f.Stat(path.Dir(filepath.ToSlash(name))); err != nil {
			return nil, err
		}
	}
	return f.Filesystem.OpenFile(name, flag, perm)
}

func TestSetRefFilesystemLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fs   billy.Filesystem
	}{
		{"without rename", noRenameFS{memfs.New()}},
		{"without implicit directories", noImplicitDirFS{memfs.New()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := New(tc.fs)
			first := plumbing.NewHashReference("refs/heads/a/b/main", counterHash(1))
			second := plumbing.NewHashReference(first.Name(), counterHash(2))

			require.NoError(t, dir.SetRef(first, nil))
			require.NoError(t, dir.SetRef(second, first))
			require.ErrorIs(t, dir.SetRef(first, first), storage.ErrReferenceHasChanged)

			got, err := dir.Ref(first.Name())
			require.NoError(t, err)
			assert.Equal(t, second, got)

			_, err = tc.fs.Stat(first.Name().String() + refLockSuffix)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// Concurrent writers incrementing a counter with compare-and-swap lose no
// update, while the counter is packed concurrently, and a concurrent reader
// always lists it.
func TestSetRefConcurrentCompareAndSwap(t *testing.T) {
	t.Parallel()

	const (
		name       = plumbing.ReferenceName("refs/heads/counter")
		writers    = 8
		increments = 50
	)

	fs := osfs.New(t.TempDir())
	// The counter starts packed, so that the first updates compare against
	// packed-refs.
	require.NoError(t, util.WriteFile(fs, packedRefsPath, []byte(counterHash(0).String()+" "+name.String()+"\n"), 0o644))
	dir := New(fs)

	// untilDone runs f in a loop until done is closed, or f fails.
	done := make(chan struct{})
	untilDone := func(f func() error) <-chan error {
		errc := make(chan error, 1)
		go func() {
			for {
				select {
				case <-done:
					errc <- nil
					return
				default:
				}
				if err := f(); err != nil {
					errc <- err
					return
				}
			}
		}()
		return errc
	}
	readerErr := untilDone(func() error {
		refs, err := dir.Refs()
		if err != nil {
			return err
		}
		if findReference(refs, name.String()) == nil {
			return fmt.Errorf("%s not listed", name)
		}
		return nil
	})
	packerErr := untilDone(dir.PackRefs)

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Go(func() {
			for done := 0; done < increments; {
				current, err := dir.Ref(name)
				if err != nil {
					errs <- err
					return
				}
				n, err := strconv.ParseUint(current.Hash().String(), 16, 64)
				if err != nil {
					errs <- err
					return
				}

				err = dir.SetRef(plumbing.NewHashReference(name, counterHash(n+1)), current)
				switch {
				case err == nil:
					done++
				case errors.Is(err, storage.ErrReferenceHasChanged), errors.Is(err, os.ErrExist):
					// Lost the race, or waited too long for the lock: try
					// again from the new value.
				default:
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(done)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, <-readerErr)
	require.NoError(t, <-packerErr)

	got, err := dir.Ref(name)
	require.NoError(t, err)
	assert.Equal(t, counterHash(writers*increments), got.Hash(), "an update was lost")

	err = util.Walk(fs, refsPath, func(path string, _ os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(path, refLockSuffix) {
			return fmt.Errorf("lock file left behind: %s", path)
		}
		return err
	})
	require.NoError(t, err)
}

// updatingFS runs update once a new packed-refs is renamed into place.
type updatingFS struct {
	billy.Filesystem
	update func() error
	once   sync.Once
}

func (f *updatingFS) Rename(from, to string) error {
	if err := f.Filesystem.Rename(from, to); err != nil {
		return err
	}
	var err error
	if filepath.ToSlash(to) == packedRefsPath {
		f.once.Do(func() { err = f.update() })
	}
	return err
}

func TestPackRefsKeepsUpdatedRef(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	packed := plumbing.NewHashReference(name, counterHash(1))
	updated := plumbing.NewHashReference(name, counterHash(2))

	base := memfs.New()
	require.NoError(t, util.WriteFile(base, name.String(), []byte(packed.Hash().String()+"\n"), 0o644))
	// Another writer updates the reference once it is packed, before the
	// loose reference is pruned.
	fs := &updatingFS{Filesystem: base, update: func() error { return New(base).SetRef(updated, packed) }}
	dir := New(fs)

	require.NoError(t, dir.PackRefs())

	got, err := dir.Ref(name)
	require.NoError(t, err)
	assert.Equal(t, updated, got, "the update must not be lost")
	got, err = dir.packedRef(name)
	require.NoError(t, err)
	assert.Equal(t, packed, got)
}

func TestPackRefsKeepsLockedRef(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	value := counterHash(1)
	lockPath := name.String() + refLockSuffix

	fs := memfs.New()
	require.NoError(t, util.WriteFile(fs, name.String(), []byte(value.String()+"\n"), 0o644))
	// Another writer, git maybe, is updating the reference to the value it
	// holds in the lock file.
	require.NoError(t, util.WriteFile(fs, lockPath, []byte(counterHash(2).String()+"\n"), 0o644))
	dir := New(fs)

	require.NoError(t, dir.PackRefs())

	loose, err := dir.readReferenceFile(".", name.String())
	require.NoError(t, err, "the loose reference must be kept")
	assert.Equal(t, value, loose.Hash())
	lock, err := util.ReadFile(fs, lockPath)
	require.NoError(t, err)
	assert.Equal(t, counterHash(2).String()+"\n", string(lock))
}

func TestPackRefsRemovesEmptyParents(t *testing.T) {
	t.Parallel()

	for fsName, newFS := range refTestFilesystems {
		t.Run(fsName, func(t *testing.T) {
			t.Parallel()
			fs := newFS(t)
			dir := New(fs)
			ref := plumbing.NewHashReference("refs/heads/a/b", counterHash(1))
			require.NoError(t, dir.SetRef(ref, nil))

			require.NoError(t, dir.PackRefs())

			_, err := fs.Stat("refs/heads/a")
			assert.ErrorIs(t, err, os.ErrNotExist)
			_, err = fs.Stat("refs/heads")
			assert.NoError(t, err)
			got, err := dir.Ref(ref.Name())
			require.NoError(t, err)
			assert.Equal(t, ref, got)
		})
	}
}

// Of concurrent writers creating the same reference with a zero `old`,
// exactly one succeeds.
func TestSetRefConcurrentCreate(t *testing.T) {
	t.Parallel()

	const (
		name    = plumbing.ReferenceName("refs/heads/created")
		writers = 8
		rounds  = 20
	)
	zero := plumbing.NewHashReference(name, plumbing.ZeroHash)

	for round := range rounds {
		dir := New(osfs.New(t.TempDir()))

		var created atomic.Int32
		var wg sync.WaitGroup
		errs := make(chan error, writers)
		for w := range writers {
			wg.Go(func() {
				err := dir.SetRef(plumbing.NewHashReference(name, counterHash(uint64(w+1))), zero)
				switch {
				case err == nil:
					created.Add(1)
				case errors.Is(err, storage.ErrReferenceHasChanged), errors.Is(err, os.ErrExist):
				default:
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.Equal(t, int32(1), created.Load(), "round %d", round)
	}
}

// git and go-git exclude each other on the lock of a reference, and an update
// go-git rejects leaves nothing git takes for a broken reference.
func TestRefLockInteropWithGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}

	repo := t.TempDir()
	git := func(args ...string) (string, error) {
		cmd := gitenv.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	mustGit := func(args ...string) string {
		t.Helper()
		out, err := git(args...)
		require.NoError(t, err, "git %v: %s", args, out)
		return out
	}

	mustGit("-c", "init.defaultBranch=main", "init", "-q")
	if _, err := os.Stat(filepath.Join(repo, ".git", "reftable")); err == nil {
		t.Skip("git defaults to the reftable backend")
	}
	commit := func(msg string) plumbing.Hash {
		mustGit("-c", "user.name=a", "-c", "user.email=a@example.com", "commit", "-q", "--allow-empty", "-m", msg)
		return plumbing.NewHash(mustGit("rev-parse", "HEAD"))
	}
	a, b := commit("a"), commit("b")
	dir := New(osfs.New(filepath.Join(repo, ".git")))

	//nolint:paralleltest // the subtests share a repository
	t.Run("git waits for the lock of go-git", func(t *testing.T) {
		const name = plumbing.ReferenceName("refs/heads/locked")
		lock, err := dir.lockRef(name)
		require.NoError(t, err)
		out, err := git("update-ref", name.String(), a.String())
		lock.unlock()
		require.Error(t, err)
		assert.Contains(t, out, "cannot lock ref")

		mustGit("update-ref", name.String(), a.String())
	})

	//nolint:paralleltest // the subtests share a repository
	t.Run("rejected updates leave no broken reference", func(t *testing.T) {
		mustGit("update-ref", "refs/heads/packed", a.String())
		mustGit("pack-refs", "--all")

		err := dir.SetRef(plumbing.NewHashReference("refs/heads/packed", b), plumbing.NewHashReference("refs/heads/packed", b))
		require.ErrorIs(t, err, storage.ErrReferenceHasChanged)
		err = dir.SetRef(plumbing.NewHashReference("refs/heads/missing", b), plumbing.NewHashReference("refs/heads/missing", a))
		require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)

		// show-ref fails on a broken reference, for-each-ref warns about it.
		mustGit("show-ref")
		cmd := gitenv.CommandContext(t.Context(), "git", "-C", repo, "for-each-ref")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run())
		assert.Empty(t, stderr.String())
		assert.Equal(t, a.String(), mustGit("rev-parse", "refs/heads/packed"))
	})

	//nolint:paralleltest // the subtests share a repository
	t.Run("git waits for the packed-refs lock of go-git", func(t *testing.T) {
		lock, err := dir.lockFile(packedRefsPath, packedRefsLockTimeout)
		require.NoError(t, err)
		out, err := git("pack-refs", "--all")
		lock.unlock()
		require.Error(t, err)
		assert.Contains(t, out, "packed-refs.lock")

		mustGit("pack-refs", "--all")
	})

	//nolint:paralleltest // the subtests share a repository
	t.Run("git reads the updates of go-git", func(t *testing.T) {
		mustGit("update-ref", "refs/heads/updated", a.String())
		err := dir.SetRef(plumbing.NewHashReference("refs/heads/updated", b), plumbing.NewHashReference("refs/heads/updated", a))
		require.NoError(t, err)
		assert.Equal(t, b.String(), mustGit("rev-parse", "refs/heads/updated"))

		require.NoError(t, dir.RemoveRef("refs/heads/updated"))
		_, err = git("rev-parse", "--verify", "-q", "refs/heads/updated")
		assert.Error(t, err)
	})

	//nolint:paralleltest // the subtests share a repository
	t.Run("a zero old value creates only, as for git", func(t *testing.T) {
		zero := plumbing.ZeroHash.String()

		created := plumbing.NewHashReference("refs/heads/created-by-go-git", a)
		require.NoError(t, dir.SetRef(created, plumbing.NewHashReference(created.Name(), plumbing.ZeroHash)))
		out, err := git("update-ref", created.Name().String(), b.String(), zero)
		require.Error(t, err)
		assert.Contains(t, out, "already exists")

		mustGit("update-ref", "refs/heads/created-by-git", a.String(), zero)
		err = dir.SetRef(plumbing.NewHashReference("refs/heads/created-by-git", b), plumbing.NewHashReference("refs/heads/created-by-git", plumbing.ZeroHash))
		require.ErrorIs(t, err, storage.ErrReferenceHasChanged)
		assert.Equal(t, a.String(), mustGit("rev-parse", "refs/heads/created-by-git"))
	})
}
