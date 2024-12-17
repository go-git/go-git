package dotgit

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

func BenchmarkNewObjectPack(b *testing.B) {
	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()
	fs := osfs.New(b.TempDir())

	for i := 0; i < b.N; i++ {
		w, err := newPackWrite(fs)

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
		for i := 0; i < 281; i++ {
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
	fs := osfs.New(t.TempDir())

  w, err := newPackWrite(fs)
	require.NoError(t, err)

	w.Notify = func(h plumbing.Hash, idx *idxfile.Writer) {
		t.Fatal("unexpected call to PackWriter.Notify")
	}

	assert.NoError(t, w.Close())
}
