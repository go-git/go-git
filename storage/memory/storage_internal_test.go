package memory

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetEncodedObjectDefersMissingCompatDependencies(t *testing.T) {
	t.Parallel()

	s := NewStorage(WithObjectFormat(formatcfg.SHA1))
	s.translator = compat.NewTranslator(compat.Formats{
		Native: formatcfg.SHA1,
		Compat: formatcfg.SHA256,
	}, compat.NewMemoryMapping())

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
	s.translator = compat.NewTranslator(compat.Formats{
		Native: formatcfg.SHA1,
		Compat: formatcfg.SHA256,
	}, failingHashMapping{err: errors.New("mapping write failed")})

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

type failingHashMapping struct {
	err error
}

func (m failingHashMapping) NativeToCompat(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.Hash{}, plumbing.ErrObjectNotFound
}

func (m failingHashMapping) CompatToNative(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.Hash{}, plumbing.ErrObjectNotFound
}

func (m failingHashMapping) Add(plumbing.Hash, plumbing.Hash) error {
	return m.err
}

func (m failingHashMapping) Count() (int, error) {
	return 0, nil
}
