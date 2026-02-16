package memory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

var (
	sto = memory.NewStorage()

	// Ensure interfaces are implemented.
	_ storer.EncodedObjectStorer  = sto
	_ storer.IndexStorer          = sto
	_ storer.ReferenceStorer      = sto
	_ storer.ShallowStorer        = sto
	_ xstorage.ObjectFormatSetter = sto
	_ xstorage.ExtensionChecker   = sto
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sto := memory.NewStorage(memory.WithObjectFormat(tt.initialFormat))

			err := sto.SetObjectFormat(tt.targetFormat)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetObjectFormatWithExistingObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		initialFormat formatcfg.ObjectFormat
		targetFormat  formatcfg.ObjectFormat
	}{
		{
			name:          "change from SHA1 to SHA256",
			initialFormat: formatcfg.SHA1,
			targetFormat:  formatcfg.SHA256,
		},
		{
			name:          "change from SHA1 (unset) to SHA1",
			initialFormat: formatcfg.UnsetObjectFormat,
			targetFormat:  formatcfg.SHA1,
		},
		{
			name:          "change from SHA256 to SHA1",
			initialFormat: formatcfg.SHA256,
			targetFormat:  formatcfg.SHA1,
		},
		{
			name:          "set same format SHA1",
			initialFormat: formatcfg.SHA1,
			targetFormat:  formatcfg.SHA1,
		},
		{
			name:          "set same format SHA256",
			initialFormat: formatcfg.SHA256,
			targetFormat:  formatcfg.SHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sto := memory.NewStorage(memory.WithObjectFormat(tt.initialFormat))

			obj := sto.NewEncodedObject()
			obj.SetType(plumbing.BlobObject)
			_, err := sto.SetEncodedObject(obj)
			require.NoError(t, err)

			err = sto.SetObjectFormat(tt.targetFormat)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot change object format")
		})
	}
}

func TestSupportsExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ext   string
		value string
		want  bool
	}{
		{
			name:  "objectformat with sha1",
			ext:   "objectformat",
			value: "sha1",
			want:  true,
		},
		{
			name:  "objectformat with sha256",
			ext:   "objectformat",
			value: "sha256",
			want:  true,
		},
		{
			name:  "objectformat with empty string",
			ext:   "objectformat",
			value: "",
			want:  true,
		},
		{
			name:  "objectformat with unsupported value",
			ext:   "objectformat",
			value: "sha512",
			want:  false,
		},
		{
			name:  "unsupported extension name",
			ext:   "noop",
			value: "sha1",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sto := memory.NewStorage()
			got := sto.SupportsExtension(tt.ext, tt.value)
			assert.Equal(t, tt.want, got)
		})
	}
}
