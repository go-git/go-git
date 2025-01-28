package filesystem_test

import (
	"testing"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
)

var (
	fs  = memfs.New()
	sto = filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	// Ensure interfaces are implemented.
	_ storer.EncodedObjectStorer = sto
	_ storer.IndexStorer         = sto
	_ storer.ReferenceStorer     = sto
	_ storer.ShallowStorer       = sto
	_ storer.DeltaObjectStorer   = sto
	_ storer.PackfileWriter      = sto
)

func TestFilesystem(t *testing.T) {
	assert.Same(t, fs, sto.Filesystem())
}

func TestNewStorageShouldNotAddAnyContentsToDir(t *testing.T) {
	fs := osfs.New(t.TempDir())

	sto := filesystem.NewStorageWithOptions(
		fs,
		cache.NewObjectLRUDefault(),
		filesystem.Options{ExclusiveAccess: true})
	assert.NotNil(t, sto)

	fis, err := fs.ReadDir("/")
	assert.NoError(t, err)
	assert.Len(t, fis, 0)
}
