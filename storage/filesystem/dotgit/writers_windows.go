//go:build windows

package dotgit

import (
	"fmt"
	"path/filepath"

	"github.com/go-git/go-billy/v6"
	"golang.org/x/sys/windows"

	"github.com/go-git/go-git/v6/utils/trace"
)

func fixPermissions(fs billy.Filesystem, path string) {
	fullpath := filepath.Join(fs.Root(), path)
	p, err := windows.UTF16PtrFromString(fullpath)
	if err != nil {
		trace.General.Printf("failed to chmod %s: %v", fullpath, err)
		return
	}

	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		trace.General.Printf("failed to chmod %s: %v", fullpath, err)
		return
	}

	if attrs&windows.FILE_ATTRIBUTE_READONLY != 0 {
		return
	}

	err = windows.SetFileAttributes(p,
		attrs|windows.FILE_ATTRIBUTE_READONLY,
	)
	if err != nil {
		trace.General.Printf("failed to chmod %s: %v", fullpath, err)
	}
}

func isReadOnly(fs billy.Filesystem, path string) (bool, error) {
	fullpath := filepath.Join(fs.Root(), path)
	p, err := windows.UTF16PtrFromString(fullpath)
	if err != nil {
		return false, fmt.Errorf("%w: %q", err, fullpath)
	}

	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false, fmt.Errorf("%w: %q", err, fullpath)
	}

	if attrs&windows.FILE_ATTRIBUTE_READONLY != 0 {
		return true, nil
	}

	return false, nil
}
