package compat_test

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/compat"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushExportStorerExportsCompatObjects(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := compat.NewMemoryMapping()
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)

	blobObj := makeCompatEncodedObject(t, plumbing.BlobObject, []byte("exported content\n"), format.SHA256)
	_, err := base.ObjectStorage.SetEncodedObject(blobObj)
	require.NoError(t, err)
	compatBlobHash, err := tr.TranslateObject(blobObj)
	require.NoError(t, err)

	cfg := config.NewConfig()
	cfg.Extensions.ObjectFormat = format.SHA256
	exporter := compat.NewPushExportStorer(base, cfg, tr)

	exported, err := exporter.EncodedObject(plumbing.BlobObject, blobObj.Hash())
	require.NoError(t, err)
	assert.Equal(t, compatBlobHash, exported.Hash())

	exportedByCompat, err := exporter.EncodedObject(plumbing.BlobObject, compatBlobHash)
	require.NoError(t, err)
	assert.Equal(t, compatBlobHash, exportedByCompat.Hash())

	assert.NoError(t, exporter.HasEncodedObject(compatBlobHash))
	size, err := exporter.EncodedObjectSize(compatBlobHash)
	require.NoError(t, err)
	assert.Equal(t, int64(len("exported content\n")), size)
}

func TestPushExportStorerPropagatesMappingErrors(t *testing.T) {
	t.Parallel()

	base := memory.NewStorage(memory.WithObjectFormat(format.SHA256))
	mapping := &failingNativeLookupMapping{err: errors.New("native lookup failed")}
	tr := compat.NewTranslator(compat.Formats{
		Native: format.SHA256,
		Compat: format.SHA1,
	}, mapping)

	exporter := compat.NewPushExportStorer(base, config.NewConfig(), tr)
	_, err := exporter.EncodedObject(plumbing.AnyObject, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "native lookup failed")
}

type failingNativeLookupMapping struct {
	err error
}

func (m *failingNativeLookupMapping) NativeToCompat(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.ZeroHash, m.err
}

func (m *failingNativeLookupMapping) CompatToNative(plumbing.Hash) (plumbing.Hash, error) {
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

func (m *failingNativeLookupMapping) Add(plumbing.Hash, plumbing.Hash) error {
	return nil
}

func (m *failingNativeLookupMapping) Count() (int, error) {
	return 0, nil
}

func makeCompatEncodedObject(
	t *testing.T,
	objType plumbing.ObjectType,
	content []byte,
	objectFormat format.ObjectFormat,
) plumbing.EncodedObject {
	t.Helper()

	obj := plumbing.NewMemoryObject(plumbing.FromObjectFormat(objectFormat))
	obj.SetType(objType)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = w.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return obj
}
