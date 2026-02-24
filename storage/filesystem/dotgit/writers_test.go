package dotgit

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
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
		w, err := newPackWrite(fs, config.SHA1)

		require.NoError(b, err)
		_, err = io.Copy(w, f.Packfile())

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

	_, err = io.Copy(w, f.Packfile())
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

	pf, err := fs.Open(pfPath)
	assert.NoError(t, err)

	objFound := false
	pfs := packfile.NewScanner(pf)
	for pfs.Scan() {
		data := pfs.Data()
		if data.Section != packfile.ObjectSection {
			continue
		}

		objFound = true
		assert.NotNil(t, data.Value())
	}

	assert.NoError(t, pf.Close())
	assert.True(t, objFound)
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

func TestPackWriterUnusedNotify(t *testing.T) {
	t.Parallel()
	fs := osfs.New(t.TempDir())

	w, err := newPackWrite(fs, config.SHA1)
	require.NoError(t, err)

	w.Notify = func(_ plumbing.Hash, _ *idxfile.Writer) {
		t.Fatal("unexpected call to PackWriter.Notify")
	}

	assert.NoError(t, w.Close())
}

func TestPackWriterPermissions(t *testing.T) {
	t.Parallel()

	f := fixtures.Basic().One()

	fs := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	_, err = io.Copy(w, f.Packfile())
	require.NoError(t, err)

	require.NoError(t, w.Close())

	pfPath := fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash)
	idxPath := fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash)
	revPath := fmt.Sprintf("objects/pack/pack-%s.rev", f.PackfileHash)

	stat, err := fs.Stat(pfPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), stat.Mode().Perm())

	stat, err = fs.Stat(idxPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), stat.Mode().Perm())

	stat, err = fs.Stat(revPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), stat.Mode().Perm())
}

func TestObjectWriterPermissions(t *testing.T) {
	t.Parallel()

	fs := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dot := New(fs)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewObject()
	require.NoError(t, err)

	err = w.WriteHeader(plumbing.BlobObject, 14)
	require.NoError(t, err)

	_, err = w.Write([]byte("this is a test"))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	stat, err := fs.Stat("objects/a8/a940627d132695a9769df883f85992f0ff4a43")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), stat.Mode().Perm())
}
