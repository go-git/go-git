//go:build windows

package git

import (
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

func init() {
	fillSystemInfo = func(e *index.Entry, sys any) {
		if os, ok := sys.(*syscall.Win32FileAttributeData); ok {
			seconds := os.CreationTime.Nanoseconds() / 1000000000
			nanoseconds := os.CreationTime.Nanoseconds() - seconds*1000000000
			e.CreatedAt = time.Unix(seconds, nanoseconds)
		}
	}
}

func isSymlinkWindowsNonAdmin(err error) bool {
	if err != nil {
		if errLink, ok := err.(*os.LinkError); ok {
			if errNo, ok := errLink.Err.(syscall.Errno); ok {
				return errNo == windows.ERROR_PRIVILEGE_NOT_HELD
			}
		}
	}

	return false
}
