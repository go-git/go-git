package dotgit

import (
	"compress/zlib"
	"io"
	"path"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"
)

type objectCloseTrackingFS struct {
	billy.Filesystem
	objectPath string
	closes     int
}

func (fs *objectCloseTrackingFS) Open(name string) (billy.File, error) {
	f, err := fs.Filesystem.Open(name)
	if err != nil || name != fs.objectPath {
		return f, err
	}
	return &objectCloseTrackingFile{File: f, fs: fs}, nil
}

type objectCloseTrackingFile struct {
	billy.File
	fs *objectCloseTrackingFS
}

func (f *objectCloseTrackingFile) Close() error {
	f.fs.closes++
	return f.File.Close()
}

func TestEncodedObjectReaderClosesObjectFile(t *testing.T) {
	tests := []struct {
		name      string
		raw       []byte
		zipped    bool
		typ       plumbing.ObjectType
		size      int64
		wantError bool
	}{
		{"invalid zlib", []byte("not-zlib"), false, plumbing.BlobObject, 2, true},
		{"invalid object header", []byte("bogus-header"), true, plumbing.BlobObject, 2, true},
		{"type mismatch", []byte("blob 2\x00ok"), true, plumbing.TreeObject, 2, true},
		{"size mismatch", []byte("blob 2\x00ok"), true, plumbing.BlobObject, 3, true},
		{"success", []byte("blob 2\x00ok"), true, plumbing.BlobObject, 2, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := &objectCloseTrackingFS{Filesystem: memfs.New()}
			d := New(fs)
			require.NoError(t, d.Initialize())
			h := plumbing.NewHash("0000000000000000000000000000000000000001")
			fs.objectPath = d.objectPath(h)
			require.NoError(t, fs.MkdirAll(path.Dir(fs.objectPath), 0755))
			f, err := fs.Create(fs.objectPath)
			require.NoError(t, err)
			if tc.zipped {
				w := zlib.NewWriter(f)
				_, err = w.Write(tc.raw)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			} else {
				_, err = f.Write(tc.raw)
				require.NoError(t, err)
			}
			require.NoError(t, f.Close())

			r, err := NewEncodedObject(d, h, tc.typ, tc.size).Reader()
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, r)
			} else {
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
			}
			require.Equal(t, 1, fs.closes, "loose-object file must be closed exactly once")
		})
	}
}
