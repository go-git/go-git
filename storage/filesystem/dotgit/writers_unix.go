//go:build !windows

package dotgit

import (
	"os"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/utils/trace"
)

const (
	readOnly      os.FileMode = 0o444
	groupWritable os.FileMode = 0o660
	allWritable   os.FileMode = 0o664
	groupDirMode  os.FileMode = 0o2770
	allDirMode    os.FileMode = 0o2775
)

func fixPermissions(fs billy.Filesystem, path string, sharedRepository config.SharedRepository) {
	if chmodFS, ok := fs.(billy.Chmod); ok {
		mode := readOnly
		switch sharedRepository {
		case config.SharedRepositoryGroup:
			mode = groupWritable
		case config.SharedRepositoryAll:
			mode = allWritable
		}

		if err := chmodFS.Chmod(path, mode); err != nil {
			trace.General.Printf("failed to chmod %s: %v", path, err)
		}
	}
}

func fixDirectoryPermissions(fs billy.Filesystem, path string, sharedRepository config.SharedRepository) {
	if chmodFS, ok := fs.(billy.Chmod); ok {
		mode := os.FileMode(0)
		switch sharedRepository {
		case config.SharedRepositoryGroup:
			mode = groupDirMode
		case config.SharedRepositoryAll:
			mode = allDirMode
		}

		if mode != 0 {
			if err := chmodFS.Chmod(path, mode); err != nil {
				trace.General.Printf("failed to chmod %s: %v", path, err)
			}
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
