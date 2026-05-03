package memory

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	"github.com/go-git/go-git/v6/plumbing/compat/oidmap"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

func TestSetEncodedObjectDefersMissingCompatDependencies(t *testing.T) {
	t.Parallel()

	s := NewStorage(WithObjectFormat(formatcfg.SHA1))
	s.translator = compat.NewTranslator(formatcfg.SHA1, formatcfg.SHA256, oidmap.NewMemory())

	tree := s.NewEncodedObject()
	tree.SetType(plumbing.TreeObject)

	treeContent := append([]byte("100644 file\x00"), plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes()...)
	tree.SetSize(int64(len(treeContent)))

	w, err := tree.Writer()
	require.NoError(t, err)
	_, err = w.Write(treeContent)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = s.SetEncodedObject(tree)
	require.NoError(t, err)
}

func TestSetEncodedObjectReturnsCompatPersistenceErrors(t *testing.T) {
	t.Parallel()

	s := NewStorage(WithObjectFormat(formatcfg.SHA1))
	s.translator = compat.NewTranslator(formatcfg.SHA1, formatcfg.SHA256, failingMap{err: errors.New("mapping write failed")})

	blob := s.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)

	content := []byte("blob content")
	blob.SetSize(int64(len(content)))

	w, err := blob.Writer()
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = s.SetEncodedObject(blob)
	require.Error(t, err)
	assert.ErrorContains(t, err, "record mapping")
	assert.ErrorContains(t, err, "mapping write failed")
}

type failingMap struct {
	err error
}

func (m failingMap) ToCompat(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.Hash{}, plumbing.ErrObjectNotFound
}

func (m failingMap) ToNative(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.Hash{}, plumbing.ErrObjectNotFound
}

func (m failingMap) Add(plumbing.Hash, plumbing.Hash) error {
	return m.err
}
