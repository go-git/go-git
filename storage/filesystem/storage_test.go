package filesystem_test

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
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
	_ storer.ObjectFormatGetter  = sto
)

func TestFilesystem(t *testing.T) {
	t.Parallel()
	assert.Same(t, fs, sto.Filesystem())
}

func TestNewStorageShouldNotAddAnyContentsToDir(t *testing.T) {
	t.Parallel()
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

func TestSetObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		initialFormat formatcfg.ObjectFormat
		targetFormat  formatcfg.ObjectFormat
		wantErr       bool
		errContains   string
	}{
		{
			name:          "set SHA1 on empty storage",
			initialFormat: formatcfg.UnsetObjectFormat,
			targetFormat:  formatcfg.SHA1,
			wantErr:       false,
		},
		{
			name:          "set SHA256 on empty storage",
			initialFormat: formatcfg.UnsetObjectFormat,
			targetFormat:  formatcfg.SHA256,
			wantErr:       false,
		},
		{
			name:          "set SHA1 when already SHA1",
			initialFormat: formatcfg.SHA1,
			targetFormat:  formatcfg.SHA1,
			wantErr:       false,
		},
		{
			name:          "set SHA256 when already SHA256",
			initialFormat: formatcfg.SHA256,
			targetFormat:  formatcfg.SHA256,
			wantErr:       false,
		},
		{
			name:          "change from SHA1 to SHA256",
			initialFormat: formatcfg.SHA1,
			targetFormat:  formatcfg.SHA256,
			wantErr:       false,
		},
		{
			name:          "change from SHA256 to SHA1",
			initialFormat: formatcfg.SHA256,
			targetFormat:  formatcfg.SHA1,
			wantErr:       false,
		},
		{
			name:          "invalid object format",
			initialFormat: formatcfg.UnsetObjectFormat,
			targetFormat:  formatcfg.ObjectFormat("invalid"),
			wantErr:       true,
			errContains:   "invalid object format",
		},
		{
			name:          "empty string object format",
			initialFormat: formatcfg.UnsetObjectFormat,
			targetFormat:  formatcfg.ObjectFormat(""),
			wantErr:       true,
			errContains:   "invalid object format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := osfs.New(t.TempDir())
			sto := filesystem.NewStorageWithOptions(
				fs,
				cache.NewObjectLRUDefault(),
				filesystem.Options{ObjectFormat: tt.initialFormat},
			)
			assert.NoError(t, sto.Init())

			if tt.initialFormat != formatcfg.UnsetObjectFormat {
				err := sto.SetObjectFormat(tt.initialFormat)
				assert.NoError(t, err)
			}

			err := sto.SetObjectFormat(tt.targetFormat)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetObjectFormatWithExistingPackfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tag          string
		targetFormat formatcfg.ObjectFormat
	}{
		{
			name:         "change to SHA256 with existing packfiles",
			tag:          ".git",
			targetFormat: formatcfg.SHA256,
		},
		{
			name:         "set same format SHA1 with existing packfiles",
			tag:          ".git",
			targetFormat: formatcfg.SHA1,
		},
		{
			name:         "change to SHA1 with existing SHA256 packfiles",
			tag:          ".git-sha256",
			targetFormat: formatcfg.SHA1,
		},
		{
			name:         "set same format SHA256 with existing packfiles",
			tag:          ".git-sha256",
			targetFormat: formatcfg.SHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := fixtures.ByTag(tt.tag).One().DotGit()
			sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

			packs, err := sto.ObjectPacks()
			require.NoError(t, err)
			require.Len(t, packs, 1)

			err = sto.SetObjectFormat(tt.targetFormat)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "cannot change object format")
		})
	}
}
