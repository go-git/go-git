package worktree_test

import (
	"io"
	iofs "io/fs"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	xworktree "github.com/go-git/go-git/v6/x/plumbing/worktree"
)

func TestAdd(t *testing.T) {
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
		path:     "HEAD",
		fileMode: 0o644,
		content:  []byte("af2d6a6954d532f8ffb47615169c8fdf9d383a1a\n"),
	}, {
		path:     "ORIG_HEAD",
		fileMode: 0o644,
		content:  []byte("af2d6a6954d532f8ffb47615169c8fdf9d383a1a\n"),
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
		commit        plumbing.Hash
		wantErr       bool
		checkFiles    func(t *testing.T, wt, storage billy.Filesystem, name string)
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
			commit:  plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
			wantErr: false,
			checkFiles: func(t *testing.T, wt, storage billy.Filesystem, name string) {
				checkFiles(t, dotGitExpectedFiles, storage, wt, name)
				checkWorktree(t, wt, filepath.Join(storage.Root(), "worktrees", name))
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
			commit:  plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
			wantErr: false,
			checkFiles: func(t *testing.T, wt, storage billy.Filesystem, name string) {
				checkFiles(t, dotGitExpectedFiles, storage, wt, name)
				checkWorktree(t, wt, filepath.Join(storage.Root(), "worktrees", name))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			storer := tt.setupStorer()
			wt := tt.setupWorktree()

			w, err := xworktree.New(storer)
			if err != nil {
				t.Fatalf("failed to create worktree: %v", err)
			}

			err = w.Add(wt, tt.name, xworktree.WithCommit(tt.commit))
			if (err != nil) != tt.wantErr {
				t.Errorf("Add() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFiles != nil {
				tt.checkFiles(t, wt, storer.Filesystem(), tt.name)
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

func checkWorktree(t *testing.T, fs billy.Filesystem, path string) {
	fn := ".git"
	fileMode := 0o644
	content := []byte("gitdir: " + path + "\n")

	fi, err := fs.Lstat(fn)
	require.NoError(t, err, "failed to lstat %q: %w", fn, err)

	assert.Equal(t, iofs.FileMode(fileMode).String(), fi.Mode().String(), "filemode mismatch for %q", fn)
	assert.False(t, fi.IsDir(), "isdir mismatch")

	f, err := fs.Open(fn)
	require.NoError(t, err)

	data, err := io.ReadAll(f)
	require.NoError(t, err)

	assert.Equal(t, string(content), string(data))
}

func checkFiles(t *testing.T, expected []expectedFile, fs, wt billy.Filesystem, name string) {
	for _, e := range expected {
		if e.appendFSRoot {
			e.content = append(e.content, []byte(filepath.Join(wt.Root(), ".git")+"\n")...)
		}

		fn := filepath.Join("worktrees", name, e.path)
		fi, err := fs.Lstat(fn)
		require.NoError(t, err, "failed to lstat %q: %w", fn, err)

		assert.Equal(t, iofs.FileMode(e.fileMode).String(), fi.Mode().String(), "filemode mismatch for %q", fn)
		assert.Equal(t, e.dir, fi.IsDir(), "isdir mismatch")

		if e.dir {
			continue
		}

		f, err := fs.Open(fn)
		require.NoError(t, err)

		data, err := io.ReadAll(f)
		require.NoError(t, err)

		assert.Equal(t, string(e.content), string(data))
	}
}

func TestRemove(t *testing.T) {
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
			name:    "non-existent",
			wantErr: true,
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
