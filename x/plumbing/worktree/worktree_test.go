package worktree_test

import (
	"io"
	iofs "io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	xworktree "github.com/go-git/go-git/v6/x/plumbing/worktree"
)

var worktreeNameRE = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

func TestAdd(t *testing.T) {
	t.Parallel()

	dotGitExpectedFiles := []expectedFile{{
		path:     "commondir",
		fileMode: 0o644,
		content:  []byte("../..\n"),
	}, {
		path:         "gitdir",
		fileMode:     0o644,
		content:      []byte(""),
		appendFSRoot: true,
	}, {
		path:     "refs",
		dir:      true,
		fileMode: int(0o755 | iofs.ModeDir),
	}}

	tests := []struct {
		description   string
		setupStorer   func() *filesystem.Storage
		setupWorktree func() billy.Filesystem
		name          string
		opts          []xworktree.Option
		wantErr       bool
		checkFiles    func(t *testing.T, storage, wt billy.Filesystem, name string)
	}{
		{
			description: "memfs: add worktree",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name:    "test-work-tree",
			wantErr: false,
			checkFiles: func(t *testing.T, storage, wt billy.Filesystem, name string) {
				expected := append(dotGitExpectedFiles, expectedFile{ //nolint:gocritic
					path: "ORIG_HEAD", fileMode: 0o644,
					content: []byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"),
				}, expectedFile{
					path: "HEAD", fileMode: 0o644,
					content: []byte("ref: refs/heads/test-work-tree\n"),
				})
				checkFiles(t, expected, storage, wt, name)
				checkWorktree(t, storage, wt, filepath.Join(storage.Root(), "worktrees", name))
			},
		},
		{
			description: "memfs: add worktree with commit",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name: "test-work-tree2",
			opts: []xworktree.Option{
				xworktree.WithCommit(plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")),
			},
			wantErr: false,
			checkFiles: func(t *testing.T, storage, wt billy.Filesystem, name string) {
				expected := append(dotGitExpectedFiles, expectedFile{ //nolint:gocritic
					path: "ORIG_HEAD", fileMode: 0o644,
					content: []byte("af2d6a6954d532f8ffb47615169c8fdf9d383a1a\n"),
				}, expectedFile{
					path: "HEAD", fileMode: 0o644,
					content: []byte("ref: refs/heads/test-work-tree2\n"),
				})
				checkFiles(t, expected, storage, wt, name)
				checkWorktree(t, storage, wt, filepath.Join(storage.Root(), "worktrees", name))
			},
		},
		{
			description: "boundOS: add worktree",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir, osfs.WithBoundOS()))
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name:    "test-work-tree",
			wantErr: false,
			checkFiles: func(t *testing.T, storage, wt billy.Filesystem, name string) {
				checkFiles(t, dotGitExpectedFiles, storage, wt, name)
				checkWorktree(t, storage, wt, filepath.Join(storage.Root(), "worktrees", name))
			},
		},
		{
			description: "memfs: add worktree with detached HEAD",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name:    "detached-worktree",
			opts:    []xworktree.Option{xworktree.WithDetachedHead()},
			wantErr: false,
			checkFiles: func(t *testing.T, storage, wt billy.Filesystem, name string) {
				expected := append(dotGitExpectedFiles, expectedFile{ //nolint:gocritic
					path: "ORIG_HEAD", fileMode: 0o644,
					content: []byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"),
				}, expectedFile{
					path: "HEAD", fileMode: 0o644,
					content: []byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"),
				})
				checkFiles(t, expected, storage, wt, name)

				w, err := xworktree.New(filesystem.NewStorage(storage, cache.NewObjectLRUDefault()))
				require.NoError(t, err)

				repo, err := w.Open(wt)
				require.NoError(t, err)

				// Verify HEAD points to the commit (detached).
				head, err := repo.Head()
				require.NoError(t, err)
				assert.Equal(t, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())
				assert.Equal(t, plumbing.ReferenceName("HEAD"), head.Name())
			},
		},
		{
			description: "memfs: add worktree with branch (default)",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name:    "branch-worktree",
			wantErr: false,
			checkFiles: func(t *testing.T, storage, wt billy.Filesystem, name string) {
				w, err := xworktree.New(filesystem.NewStorage(storage, cache.NewObjectLRUDefault()))
				require.NoError(t, err)

				repo, err := w.Open(wt)
				require.NoError(t, err)

				// Verify HEAD points to the branch.
				head, err := repo.Head()
				require.NoError(t, err)
				assert.Equal(t, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", head.Hash().String())
				assert.Equal(t, plumbing.NewBranchReferenceName("branch-worktree"), head.Name())

				branchRef, err := repo.Reference(plumbing.NewBranchReferenceName("branch-worktree"), true)
				require.NoError(t, err)
				assert.Equal(t, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", branchRef.Hash().String())
			},
		},
		{
			description: "memfs: add worktree that already exists",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				err = w.Add(wtFS, "existing-worktree", xworktree.WithCommit(plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")))
				require.NoError(t, err)

				return storer
			},
			setupWorktree: func() billy.Filesystem {
				return memfs.New()
			},
			name:    "existing-worktree",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			storer := tt.setupStorer()
			wt := tt.setupWorktree()

			w, err := xworktree.New(storer)
			if err != nil {
				t.Fatalf("failed to create worktree: %v", err)
			}

			err = w.Add(wt, tt.name, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Add() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFiles != nil {
				tt.checkFiles(t, storer.Filesystem(), wt, tt.name)
			}
		})
	}
}

type expectedFile struct {
	path         string
	dir          bool
	appendFSRoot bool
	fileMode     int
	content      []byte
}

func checkWorktree(t *testing.T, storage, wt billy.Filesystem, path string) {
	fn := ".git"
	fileMode := 0o644
	content := []byte("gitdir: " + path + "\n")

	fi, err := wt.Lstat(fn)
	require.NoError(t, err, "failed to lstat %q: %w", fn, err)

	if runtime.GOOS != "windows" {
		assert.Equal(t, iofs.FileMode(fileMode).String(), fi.Mode().String(), "filemode mismatch for %q", fn)
	}
	assert.False(t, fi.IsDir(), "isdir mismatch")

	f, err := wt.Open(fn)
	require.NoError(t, err)

	data, err := io.ReadAll(f)
	require.NoError(t, err)

	assert.Equal(t, string(content), string(data))

	rel, err := filepath.Rel(storage.Root(), path)
	require.NoError(t, err)

	gitDir, err := storage.Chroot(rel)
	require.NoError(t, err)

	commonDir := storage
	stor := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(gitDir, commonDir),
		cache.NewObjectLRUDefault())
	r, err := git.Open(stor, wt)
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	st, err := w.Status()
	require.NoError(t, err)

	assert.True(t, st.IsClean(), "worktree is not clean")
}

func checkFiles(t *testing.T, expected []expectedFile, storage, wt billy.Filesystem, name string) {
	for _, e := range expected {
		if e.appendFSRoot {
			e.content = append(e.content, []byte(filepath.Join(wt.Root(), ".git")+"\n")...)
		}

		fn := filepath.Join("worktrees", name, e.path)
		fi, err := storage.Lstat(fn)
		require.NoError(t, err, "failed to lstat %q: %w", fn, err)

		if runtime.GOOS != "windows" {
			assert.Equal(t, iofs.FileMode(e.fileMode).String(), fi.Mode().String(), "filemode mismatch for %q", fn)
		}
		assert.Equal(t, e.dir, fi.IsDir(), "isdir mismatch")

		if e.dir {
			continue
		}

		f, err := storage.Open(fn)
		require.NoError(t, err)

		data, err := io.ReadAll(f)
		require.NoError(t, err)

		assert.Equal(t, string(e.content), string(data))
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description  string
		setupStorer  func() *filesystem.Storage
		name         string
		wantErr      bool
		errContains  string
		checkRemoved func(t *testing.T, storage billy.Filesystem, name string)
	}{
		{
			description: "memfs: remove existing worktree",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				err = w.Add(wtFS, "test-worktree", xworktree.WithCommit(plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")))
				require.NoError(t, err)

				return storer
			},
			name:    "test-worktree",
			wantErr: false,
			checkRemoved: func(t *testing.T, storage billy.Filesystem, name string) {
				worktreePath := filepath.Join("worktrees", name)
				_, err := storage.Lstat(worktreePath)
				assert.Error(t, err, "worktree directory should be removed")
			},
		},
		{
			description: "boundOS: remove existing worktree",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir, osfs.WithBoundOS()))
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				err = w.Add(wtFS, "test-worktree", xworktree.WithCommit(plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")))
				require.NoError(t, err)

				return storer
			},
			name:    "test-worktree",
			wantErr: false,
			checkRemoved: func(t *testing.T, storage billy.Filesystem, name string) {
				worktreePath := filepath.Join("worktrees", name)
				_, err := storage.Lstat(worktreePath)
				assert.Error(t, err, "worktree directory should be removed")
			},
		},
		{
			description: "remove non-existent worktree",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			name:        "non-existent",
			wantErr:     true,
			errContains: "worktree not found",
		},
		{
			description: "invalid worktree name with spaces",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			name:        "invalid name",
			wantErr:     true,
			errContains: "invalid worktree name",
		},
		{
			description: "invalid worktree name with special characters",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			name:        "test@worktree",
			wantErr:     true,
			errContains: "invalid worktree name",
		},
		{
			description: "invalid worktree name with slash",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			name:        "test/worktree",
			wantErr:     true,
			errContains: "invalid worktree name",
		},
		{
			description: "empty worktree name",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			name:        "",
			wantErr:     true,
			errContains: "invalid worktree name",
		},
		{
			description: "worktree name with only dash",
			setupStorer: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				err = w.Add(wtFS, "test-dash-name", xworktree.WithCommit(plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")))
				require.NoError(t, err)

				return storer
			},
			name:    "test-dash-name",
			wantErr: false,
			checkRemoved: func(t *testing.T, storage billy.Filesystem, name string) {
				worktreePath := filepath.Join("worktrees", name)
				_, err := storage.Lstat(worktreePath)
				assert.Error(t, err, "worktree directory should be removed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			storer := tt.setupStorer()
			w, err := xworktree.New(storer)
			require.NoError(t, err, "Unable to create worktree")

			err = w.Remove(tt.name)
			if tt.wantErr {
				require.Error(t, err, "Remove() should return an error")
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains, "error message should contain expected text")
				}
				return
			}

			require.NoError(t, err, "Remove() should not return an error")

			if tt.checkRemoved != nil {
				tt.checkRemoved(t, storer.Filesystem(), tt.name)
			}
		})
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		setup       func() *filesystem.Storage
		wantNames   []string
		wantErr     bool
	}{
		{
			description: "memfs: list empty worktrees",
			setup: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			},
			wantNames: []string{},
			wantErr:   false,
		},
		{
			description: "memfs: list single worktree",
			setup: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				err = w.Add(memfs.New(), "worktree-1",
					xworktree.WithCommit(
						plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")))
				require.NoError(t, err)

				return storer
			},
			wantNames: []string{"worktree-1"},
			wantErr:   false,
		},
		{
			description: "memfs: list multiple worktrees",
			setup: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")

				for _, name := range []string{"worktree-1", "worktree-2", "worktree-3"} {
					err = w.Add(memfs.New(), name, xworktree.WithCommit(commit))
					require.NoError(t, err)
				}

				return storer
			},
			wantNames: []string{"worktree-1", "worktree-2", "worktree-3"},
			wantErr:   false,
		},
		{
			description: "boundOS: list worktrees",
			setup: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir, osfs.WithBoundOS()))
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")

				err = w.Add(memfs.New(), "feature-a", xworktree.WithCommit(commit))
				require.NoError(t, err)

				err = w.Add(memfs.New(), "feature-b", xworktree.WithCommit(commit))
				require.NoError(t, err)

				return storer
			},
			wantNames: []string{"feature-a", "feature-b"},
			wantErr:   false,
		},
		{
			description: "memfs: list after removing a worktree",
			setup: func() *filesystem.Storage {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")

				for _, name := range []string{"wt-1", "wt-2", "wt-3"} {
					err = w.Add(memfs.New(), name, xworktree.WithCommit(commit))
					require.NoError(t, err)
				}

				err = w.Remove("wt-2")
				require.NoError(t, err)

				return storer
			},
			wantNames: []string{"wt-1", "wt-3"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			storer := tt.setup()
			w, err := xworktree.New(storer)
			require.NoError(t, err)

			names, err := w.List()
			if tt.wantErr {
				require.Error(t, err, "List() should return an error")
				return
			}

			require.NoError(t, err, "List() should not return an error")
			assert.ElementsMatch(t, tt.wantNames, names, "returned worktree names should match expected")
		})
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		setup       func() (*filesystem.Storage, billy.Filesystem)
		wantErr     bool
		errContains string
		checkRepo   func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem)
	}{
		{
			description: "memfs: open linked worktree",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				name := "test-worktree"
				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
				err = w.Add(wtFS, name, xworktree.WithCommit(commit))
				require.NoError(t, err)

				return storer, wtFS
			},
			wantErr: false,
			checkRepo: func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem) {
				require.NotNil(t, repo, "repository should not be nil")

				wt, err := repo.Worktree()
				require.NoError(t, err)
				require.NotNil(t, wt)

				head, err := repo.Head()
				require.NoError(t, err)
				assert.Equal(t, "af2d6a6954d532f8ffb47615169c8fdf9d383a1a", head.Hash().String())
			},
		},
		{
			description: "boundOS: open linked worktree",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir, osfs.WithBoundOS()))
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				name := "test-worktree"
				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
				err = w.Add(wtFS, name, xworktree.WithCommit(commit))
				require.NoError(t, err)

				return storer, wtFS
			},
			wantErr: false,
			checkRepo: func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem) {
				require.NotNil(t, repo, "repository should not be nil")

				wt, err := repo.Worktree()
				require.NoError(t, err)
				require.NotNil(t, wt)

				head, err := repo.Head()
				require.NoError(t, err)
				assert.Equal(t, "af2d6a6954d532f8ffb47615169c8fdf9d383a1a", head.Hash().String())
			},
		},
		{
			description: "open with nil filesystem",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				return storer, nil
			},
			wantErr:     true,
			errContains: "worktree fs is nil",
		},
		{
			description: "open regular repository (non-linked worktree)",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

				return storer, memfs.New()
			},
			wantErr: false,
			checkRepo: func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem) {
				require.NotNil(t, repo, "repository should not be nil")

				_, err := repo.Head()
				require.NoError(t, err)
			},
		},
		{
			description: "open linked worktree and verify filesystem operations",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS := memfs.New()
				name := "feature-branch"
				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
				err = w.Add(wtFS, name, xworktree.WithCommit(commit))
				require.NoError(t, err)

				return storer, wtFS
			},
			wantErr: false,
			checkRepo: func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem) {
				require.NotNil(t, repo, "repository should not be nil")

				fi, err := wtFS.Stat(".git")
				require.NoError(t, err)
				assert.False(t, fi.IsDir(), ".git should be a file, not a directory in linked worktree")

				head, err := repo.Head()
				require.NoError(t, err)
				assert.Equal(t, "af2d6a6954d532f8ffb47615169c8fdf9d383a1a", head.Hash().String())

				commit, err := repo.CommitObject(head.Hash())
				require.NoError(t, err)
				require.NotNil(t, commit)
			},
		},
		{
			description: "open multiple linked worktrees",
			setup: func() (*filesystem.Storage, billy.Filesystem) {
				fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
				storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
				w, err := xworktree.New(storer)
				require.NoError(t, err)

				wtFS1 := memfs.New()
				commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
				err = w.Add(wtFS1, "worktree-1", xworktree.WithCommit(commit))
				require.NoError(t, err)

				r, err := w.Open(wtFS1)
				require.NoError(t, err)

				wt, err := r.Worktree()
				require.NoError(t, err)

				err = util.WriteFile(wtFS1, "newfile.txt", []byte("foobar"), 0o644)
				require.NoError(t, err)

				_, err = wt.Add("newfile.txt")
				require.NoError(t, err)

				_, err = wt.Commit("test commit", &git.CommitOptions{
					Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
				})
				require.NoError(t, err)

				wtFS2 := memfs.New()
				err = w.Add(wtFS2, "worktree-2", xworktree.WithCommit(commit))
				require.NoError(t, err)

				return storer, wtFS1
			},
			wantErr: false,
			checkRepo: func(t *testing.T, repo *git.Repository, wtFS billy.Filesystem) {
				require.NotNil(t, repo, "repository should not be nil")

				head, err := repo.Head()
				require.NoError(t, err)
				assert.NotEqual(t, "af2d6a6954d532f8ffb47615169c8fdf9d383a1a", head.Hash().String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Parallel()

			storer, wtFS := tt.setup()
			w, err := xworktree.New(storer)
			require.NoError(t, err)

			repo, err := w.Open(wtFS)
			if tt.wantErr {
				require.Error(t, err, "Open() should return an error")
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains, "error message should contain expected text")
				}
				return
			}

			require.NoError(t, err, "Open() should not return an error")

			if tt.checkRepo != nil {
				tt.checkRepo(t, repo, wtFS)
			}
		})
	}
}

func FuzzAdd(f *testing.F) {
	f.Add("test")
	f.Add("test-worktree")
	f.Add("test123")
	f.Add("TEST-123")
	f.Add("")
	f.Add("test worktree")
	f.Add("test@worktree")
	f.Add("test/worktree")
	f.Add("test.worktree")
	f.Add("test_worktree")
	f.Add("-")
	f.Add("a")
	f.Add("123")
	f.Add("test-")
	f.Add("-test")
	f.Add("../../../test")

	f.Fuzz(func(t *testing.T, name string) {
		fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
		storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
		w, err := xworktree.New(storer)
		require.NoError(t, err, "failed to create worktree manager")
		require.NotNil(t, w)

		wtFS := memfs.New()
		commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")

		err = w.Add(wtFS, name, xworktree.WithCommit(commit), xworktree.WithDetachedHead())
		if worktreeNameRE.MatchString(name) {
			assert.NoError(t, err, "worktree name: %q", name)
		} else {
			assert.Error(t, err, "worktree name: %q", name)
		}
	})
}

func FuzzOpen(f *testing.F) {
	f.Add("gitdir: /path/to/worktree")
	f.Add("gitdir: .")
	f.Add("gitdir:")
	f.Add("gitdir")
	f.Add("")
	f.Add("invalid content")
	f.Add("gitdir: /very/long/path/to/worktree/directory/structure")
	f.Add("gitdir: ../relative/path")
	f.Add("gitdir: \n")
	f.Add("gitdir: path\nwith\nnewlines")
	f.Add("../../path")
	f.Add("../../path\n")

	f.Fuzz(func(t *testing.T, gitFileContent string) {
		fs := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
		storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
		w, err := xworktree.New(storer)
		require.NoError(t, err, "failed to create worktree manager")
		require.NotNil(t, w)

		wtFS := memfs.New()
		err = util.WriteFile(wtFS, ".git", []byte(gitFileContent), 0o644)
		require.NoError(t, err, "failed to file .git file")

		repo, err := w.Open(wtFS)

		if err == nil && repo == nil {
			assert.Fail(t, "invalid state: repository and error is nil")
		}
	})
}

func ExampleWorktree_Open() {
	// Setup repository storage pointing to existing dotgit.
	fs := osfs.New("/path/to/repo/.git")
	storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	w, err := xworktree.New(storer)
	if err != nil {
		panic(err)
	}

	// Create a filesystem for the new worktree.
	worktreeFS := osfs.New("/path/to/worktrees/feature-branch")

	// Specify the commit to check out.
	commit := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")

	// Create linked worktree.
	err = w.Add(worktreeFS, "feature-branch", xworktree.WithCommit(commit))
	if err != nil {
		panic(err)
	}

	// Open linked worktree repository.
	r, err := w.Open(worktreeFS)
	if err != nil {
		panic(err)
	}

	_, _ = r.Head()

	// The linked worktree repository is now ready to be used.
}

func ExampleWorktree_Remove() {
	// Setup repository storage pointing to existing dotgit.
	fs := osfs.New("/path/to/repo/.git")
	storer := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	w, err := xworktree.New(storer)
	if err != nil {
		panic(err)
	}

	// Remove a linked worktree by name.
	err = w.Remove("feature-branch")
	if err != nil {
		panic(err)
	}

	// The worktree metadata has been removed.
}
