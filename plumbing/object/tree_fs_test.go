package object

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestTreeFilesystemFileOperations(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	fileHash := storeTestObject(t, store, plumbing.BlobObject, []byte("hello tree"))
	root := &Tree{
		Entries: []TreeEntry{{Name: "hello.txt", Mode: filemode.Regular, Hash: fileHash}},
		s:       store,
	}
	filesystem := &treeFilesystem{tree: root}

	file, err := filesystem.Open("hello.txt")
	require.NoError(t, err)
	defer file.Close()

	contents, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("hello tree"), contents)

	position, err := file.Seek(6, io.SeekStart)
	require.NoError(t, err)
	require.Equal(t, int64(6), position)
	part := make([]byte, 4)
	_, err = file.Read(part)
	require.NoError(t, err)
	require.Equal(t, []byte("tree"), part)

	readAt := make([]byte, 5)
	_, err = file.ReadAt(readAt, 0)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), readAt)
}

func TestTreeFilesystemConcurrentReadAt(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	fileHash := storeTestObject(t, store, plumbing.BlobObject, []byte("concurrent reads"))
	filesystem := &treeFilesystem{
		tree: &Tree{
			Entries: []TreeEntry{{Name: "file", Mode: filemode.Regular, Hash: fileHash}},
			s:       store,
		},
	}

	file, err := filesystem.Open("file")
	require.NoError(t, err)
	defer file.Close()

	const readers = 8
	results := make(chan []byte, readers)
	var group sync.WaitGroup
	group.Add(readers)
	for range readers {
		go func() {
			defer group.Done()
			buffer := make([]byte, 10)
			_, readErr := file.ReadAt(buffer, 0)
			require.NoError(t, readErr)
			results <- buffer
		}()
	}
	group.Wait()
	close(results)

	for result := range results {
		require.Equal(t, []byte("concurrent"), result)
	}
}

func TestTreeFilesystemDirectoryOperations(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	fileHash := storeTestObject(t, store, plumbing.BlobObject, []byte("nested"))
	subtreeHash := storeTestTree(t, store, []TreeEntry{{Name: "nested.txt", Mode: filemode.Regular, Hash: fileHash}})
	root := &Tree{
		Entries: []TreeEntry{
			{Name: "dir", Mode: filemode.Dir, Hash: subtreeHash},
			{Name: "root.txt", Mode: filemode.Regular, Hash: fileHash},
		},
		s: store,
	}
	filesystem := &treeFilesystem{tree: root}

	entries, err := filesystem.ReadDir("dir")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "nested.txt", entries[0].Name())
	require.False(t, entries[0].IsDir())

	file, err := filesystem.OpenFile("dir/nested.txt", os.O_RDONLY, 0)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	_, err = filesystem.OpenFile("root.txt", os.O_WRONLY, 0)
	require.ErrorIs(t, err, billy.ErrReadOnly)
}

func TestTreeFilesystemRejectsWriteOperations(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	fileHash := storeTestObject(t, store, plumbing.BlobObject, []byte("read only"))
	root := &Tree{
		Entries: []TreeEntry{{Name: "file", Mode: filemode.Regular, Hash: fileHash}},
		s:       store,
	}
	filesystem := &treeFilesystem{tree: root}

	filesystemOperations := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { _, err := filesystem.Create("new"); return err }},
		{"Remove", func() error { return filesystem.Remove("file") }},
		{"Rename", func() error { return filesystem.Rename("file", "new") }},
		{"Symlink", func() error { return filesystem.Symlink("file", "link") }},
		{"TempFile", func() error { _, err := filesystem.TempFile("", "tmp"); return err }},
		{"MkdirAll", func() error { return filesystem.MkdirAll("new", 0o755) }},
	}
	for _, operation := range filesystemOperations {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.call(), billy.ErrReadOnly)
		})
	}

	writeFlags := []int{os.O_WRONLY, os.O_RDWR, os.O_APPEND, os.O_CREATE, os.O_TRUNC, os.O_EXCL}
	for _, flag := range writeFlags {
		t.Run("OpenFile write flag", func(t *testing.T) {
			t.Parallel()
			_, err := filesystem.OpenFile("file", flag, 0o644)
			require.ErrorIs(t, err, billy.ErrReadOnly)
		})
	}

	file, err := filesystem.Open("file")
	require.NoError(t, err)
	defer file.Close()
	require.ErrorIs(t, file.Truncate(0), billy.ErrReadOnly)
	_, err = file.Write([]byte("write"))
	require.ErrorIs(t, err, billy.ErrReadOnly)
	_, err = file.WriteAt([]byte("write"), 0)
	require.ErrorIs(t, err, billy.ErrReadOnly)
}

func TestTreeFilesystemRejectsDirectoryWrites(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	subtreeHash := storeTestTree(t, store, nil)
	filesystem := &treeFilesystem{
		tree: &Tree{
			Entries: []TreeEntry{{Name: "dir", Mode: filemode.Dir, Hash: subtreeHash}},
			s:       store,
		},
	}

	directory, err := filesystem.Open("dir")
	require.NoError(t, err)
	defer directory.Close()
	require.ErrorIs(t, directory.Truncate(0), billy.ErrReadOnly)
	_, err = directory.Write([]byte("write"))
	require.ErrorIs(t, err, billy.ErrReadOnly)
	_, err = directory.WriteAt([]byte("write"), 0)
	require.ErrorIs(t, err, billy.ErrReadOnly)
}

func TestTreeFilesystemSymlinkAndLstat(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	targetHash := storeTestObject(t, store, plumbing.BlobObject, []byte("target.txt"))
	root := &Tree{
		Entries: []TreeEntry{{Name: "link", Mode: filemode.Symlink, Hash: targetHash}},
		s:       store,
	}
	filesystem := &treeFilesystem{tree: root}

	target, err := filesystem.Readlink("link")
	require.NoError(t, err)
	require.Equal(t, "target.txt", target)

	info, err := filesystem.Lstat("link")
	require.NoError(t, err)
	require.Equal(t, "link", info.Name())
	symlinkMode, err := filemode.Symlink.ToOSFileMode()
	require.NoError(t, err)
	require.Equal(t, symlinkMode&os.ModeType, info.Mode()&os.ModeType)

	_, err = filesystem.Readlink("missing")
	require.Error(t, err)
	_, err = filesystem.Readlink(".")
	require.Error(t, err)
}

func TestTreeFilesystemInvalidPaths(t *testing.T) {
	t.Parallel()
	filesystem := &treeFilesystem{tree: &Tree{}}

	for _, path := range []string{"../outside", "a//b", ""} {
		_, err := filesystem.Open(path)
		if path == "" {
			require.NoError(t, err)
			continue
		}
		require.Error(t, err)
	}
}

func TestTreeFilesystemReadAtDoesNotChangePosition(t *testing.T) {
	t.Parallel()
	store := memory.NewStorage()
	hash := storeTestObject(t, store, plumbing.BlobObject, []byte("abcdef"))
	filesystem := &treeFilesystem{tree: &Tree{
		Entries: []TreeEntry{{Name: "file", Mode: filemode.Regular, Hash: hash}},
		s:       store,
	}}
	file, err := filesystem.Open("file")
	require.NoError(t, err)
	defer file.Close()

	_, err = file.Seek(2, io.SeekStart)
	require.NoError(t, err)
	buffer := make([]byte, 2)
	_, err = file.ReadAt(buffer, 0)
	require.NoError(t, err)
	require.Equal(t, []byte("ab"), buffer)

	remaining, err := io.ReadAll(file)
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte("cdef"), remaining))
}
