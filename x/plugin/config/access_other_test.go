//go:build !windows

package config

import (
	"io/fs"
	"syscall"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

// Git ignores EACCES on a global file, not EPERM, which fs.ErrPermission
// matches as well.
func TestAutoGlobalSkipsOnlyEACCES(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		skip bool
	}{
		"EACCES": {err: syscall.EACCES, skip: true},
		"EPERM":  {err: syscall.EPERM},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(envGitConfigGlobal, "/global")
			src := NewAuto(WithFilesystem(openErrFS{Filesystem: memfs.New(), err: tc.err}))

			_, err := src.LoadProtocolPolicy(config.GlobalScope)
			if tc.skip {
				assert.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

// openErrFS fails every Open with err.
type openErrFS struct {
	billy.Filesystem
	err error
}

func (f openErrFS) Open(name string) (billy.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
}
