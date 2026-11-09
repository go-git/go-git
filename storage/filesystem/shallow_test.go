package filesystem_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestShallowParsing(t *testing.T) {
	t.Parallel()

	sha1 := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	sha1b := "e8d3ffab552895c19b9fcf7aa264d277cde33881"
	sha256 := "a3c1b7a8f2d1e4c5b6a7980f1e2d3c4b5a6978877665544332211ffeeddccbba"

	tests := []struct {
		name    string
		format  formatcfg.ObjectFormat
		content string
		want    []string
		wantErr string
	}{
		{
			name:    "sha1 lines",
			format:  formatcfg.SHA1,
			content: sha1 + "\n" + sha1b + "\n",
			want:    []string{sha1, sha1b},
		},
		{
			name:    "unset format reads sha1",
			format:  formatcfg.UnsetObjectFormat,
			content: sha1 + "\n",
			want:    []string{sha1},
		},
		{
			name:    "sha256 line",
			format:  formatcfg.SHA256,
			content: sha256 + "\n",
			want:    []string{sha256},
		},
		{
			name:    "no trailing newline",
			format:  formatcfg.SHA1,
			content: sha1,
			want:    []string{sha1},
		},
		{
			name:    "crlf line ending",
			format:  formatcfg.SHA1,
			content: sha1 + "\r\n",
			want:    []string{sha1},
		},
		{
			name:    "bytes after the hash are ignored",
			format:  formatcfg.SHA1,
			content: sha1 + " trailing\n",
			want:    []string{sha1},
		},
		{
			name:    "empty file",
			format:  formatcfg.SHA1,
			content: "",
		},
		{
			name:    "blank line",
			format:  formatcfg.SHA1,
			content: sha1 + "\n\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "short hex",
			format:  formatcfg.SHA1,
			content: sha1[:38] + "\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "non-hex",
			format:  formatcfg.SHA1,
			content: strings.Repeat("z", 40) + "\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "sha1 line in sha256 repository",
			format:  formatcfg.SHA256,
			content: sha1 + "\n",
			wantErr: "bad shallow line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			require.NoError(t, util.WriteFile(fs, "shallow", []byte(tc.content), 0o644))
			st := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(),
				filesystem.Options{ObjectFormat: tc.format})

			got, err := st.Shallow()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			var want []plumbing.Hash
			for _, h := range tc.want {
				want = append(want, plumbing.NewHash(h))
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestShallowRoundTrip(t *testing.T) {
	t.Parallel()

	st := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	want := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}
	require.NoError(t, st.SetShallow(want))

	got, err := st.Shallow()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestSetShallowRemovesFileWhenEmpty checks that an empty boundary removes
// the shallow file rather than leaving it empty: Git takes any shallow file,
// even an empty one, to mean the repository is shallow.
func TestSetShallowRemovesFileWhenEmpty(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	require.NoError(t, st.SetShallow([]plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}))

	require.NoError(t, st.SetShallow(nil))
	_, err := fs.Lstat("shallow")
	require.ErrorIs(t, err, os.ErrNotExist)

	got, err := st.Shallow()
	require.NoError(t, err)
	assert.Empty(t, got)

	// Without a shallow file there is nothing to remove.
	require.NoError(t, st.SetShallow(nil))
	_, err = fs.Lstat("shallow")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestShallowFollowsSetObjectFormat(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	require.NoError(t, st.SetObjectFormat(formatcfg.SHA256))

	sha256 := "a3c1b7a8f2d1e4c5b6a7980f1e2d3c4b5a6978877665544332211ffeeddccbba"
	require.NoError(t, util.WriteFile(fs, "shallow", []byte(sha256+"\n"), 0o644))

	got, err := st.Shallow()
	require.NoError(t, err)
	assert.Equal(t, []plumbing.Hash{plumbing.NewHash(sha256)}, got)
}

func TestSetShallowReportsCloseError(t *testing.T) {
	t.Parallel()

	errClose := errors.New("close failed")
	fs := memfs.New()
	require.NoError(t, util.WriteFile(fs, "shallow", []byte(oldShallow), 0o644))
	st := filesystem.NewStorage(&failingFS{Filesystem: fs, closeErr: errClose}, cache.NewObjectLRUDefault())

	err := st.SetShallow([]plumbing.Hash{plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")})
	require.ErrorIs(t, err, errClose)
	requireShallowFile(t, fs, oldShallow)
}

// TestSetShallowKeepsOldFileOnWriteError checks that a failed write leaves
// the shallow file as it was, rather than truncated: a truncated boundary
// makes commits whose parents are missing look complete.
func TestSetShallowKeepsOldFileOnWriteError(t *testing.T) {
	t.Parallel()

	errWrite := errors.New("write failed")
	fs := memfs.New()
	require.NoError(t, util.WriteFile(fs, "shallow", []byte(oldShallow), 0o644))
	st := filesystem.NewStorage(&failingFS{Filesystem: fs, writeErr: errWrite}, cache.NewObjectLRUDefault())

	err := st.SetShallow([]plumbing.Hash{plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")})
	require.ErrorIs(t, err, errWrite)
	requireShallowFile(t, fs, oldShallow)
}

// TestSetShallowFailsWhileLocked checks that SetShallow fails, and leaves
// both files alone, while another writer holds shallow.lock, as Git does.
func TestSetShallowFailsWhileLocked(t *testing.T) {
	t.Parallel()

	for name, commits := range map[string][]plumbing.Hash{
		"write":  {plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")},
		"remove": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			require.NoError(t, util.WriteFile(fs, "shallow", []byte(oldShallow), 0o644))
			require.NoError(t, util.WriteFile(fs, "shallow.lock", []byte("other writer"), 0o644))
			st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

			require.ErrorIs(t, st.SetShallow(commits), os.ErrExist)

			got, err := util.ReadFile(fs, "shallow")
			require.NoError(t, err)
			assert.Equal(t, oldShallow, string(got))
			lock, err := util.ReadFile(fs, "shallow.lock")
			require.NoError(t, err)
			assert.Equal(t, "other writer", string(lock))
		})
	}
}

// TestSetShallowWithoutRename checks that SetShallow still replaces the file
// on a filesystem that cannot rename.
func TestSetShallowWithoutRename(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	require.NoError(t, util.WriteFile(fs, "shallow", []byte(oldShallow), 0o644))
	st := filesystem.NewStorage(&failingFS{Filesystem: fs, renameErr: billy.ErrNotSupported}, cache.NewObjectLRUDefault())

	require.NoError(t, st.SetShallow([]plumbing.Hash{plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")}))
	requireShallowFile(t, fs, "e8d3ffab552895c19b9fcf7aa264d277cde33881\n")
}

const oldShallow = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"

// requireShallowFile checks the shallow file holds want, and that no
// shallow.lock was left behind.
func requireShallowFile(t *testing.T, fs billy.Filesystem, want string) {
	t.Helper()

	got, err := util.ReadFile(fs, "shallow")
	require.NoError(t, err)
	assert.Equal(t, want, string(got))

	_, err = fs.Lstat("shallow.lock")
	require.ErrorIs(t, err, os.ErrNotExist, "shallow.lock left behind")
}

// failingFS opens files whose Write or Close fails with the given errors, and
// fails Rename with renameErr.
type failingFS struct {
	billy.Filesystem
	writeErr, closeErr, renameErr error
}

func (fs *failingFS) Create(name string) (billy.File, error) {
	return fs.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (fs *failingFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	f, err := fs.Filesystem.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &failingFile{File: f, writeErr: fs.writeErr, closeErr: fs.closeErr}, nil
}

func (fs *failingFS) Rename(from, to string) error {
	if fs.renameErr != nil {
		return fs.renameErr
	}
	return fs.Filesystem.Rename(from, to)
}

type failingFile struct {
	billy.File
	writeErr, closeErr error
}

func (f *failingFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func (f *failingFile) Close() error {
	err := f.File.Close()
	if f.closeErr != nil {
		return f.closeErr
	}
	return err
}
