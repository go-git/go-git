//go:build !windows

package dotgit

import (
	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/utils/trace"
)

const readOnly = 0o444

func fixPermissions(fs billy.Filesystem, path string) {
	if chmodFS, ok := fs.(billy.Chmod); ok {
		if err := chmodFS.Chmod(path, readOnly); err != nil {
			trace.General.Printf("failed to chmod %s: %v", path, err)
		}
	}
}

func isReadOnly(fs billy.Filesystem, path string) (bool, error) {
	fi, err := fs.Stat(path)
	if err != nil {
		return false, err
	}

	if fi.Mode().Perm() == readOnly {
		return true, nil
	}

	return false, nil
}
