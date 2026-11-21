package transactional

import (
	"io"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestCommit(t *testing.T) {
	t.Parallel()
	base := memory.NewStorage()
	temporal := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	st := NewStorage(base, temporal)
	defer func() {
		if closer, ok := st.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	_, err := st.SetEncodedObject(commit)
	require.NoError(t, err)

	ref := plumbing.NewHashReference("refs/a", commit.Hash())
	require.NoError(t, st.SetReference(ref))

	err = st.Commit()
	require.NoError(t, err)

	ref, err = base.Reference(ref.Name())
	require.NoError(t, err)
	assert.Equal(t, commit.Hash(), ref.Hash())

	obj, err := base.EncodedObject(plumbing.AnyObject, commit.Hash())
	require.NoError(t, err)
	assert.Equal(t, commit.Hash(), obj.Hash())
}

func TestTransactionalPackfileWriter(t *testing.T) {
	t.Parallel()
	base := memory.NewStorage()
	temporal := storage.Storer(filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault()))
	st := NewStorage(base, temporal)
	defer func() {
		if closer, ok := st.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	_, tmpOK := temporal.(storer.PackfileWriter)
	_, ok := st.(storer.PackfileWriter)
	assert.Equal(t, tmpOK, ok)
}

func TestUpdateObjectStorageWritesToTemporal(t *testing.T) {
	t.Parallel()
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	st := NewStorage(base, temporal)

	f := fixtures.Basic().One()
	pf, err := f.Packfile()
	require.NoError(t, err)

	require.NoError(t, packfile.UpdateObjectStorage(st, pf))

	assert.Empty(t, base.Objects)
	assert.Len(t, temporal.Objects, 31)

	h := plumbing.NewHash(f.Head)
	_, err = st.EncodedObject(plumbing.CommitObject, h)
	require.NoError(t, err)
	assert.ErrorIs(t, base.HasEncodedObject(h), plumbing.ErrObjectNotFound)
}
