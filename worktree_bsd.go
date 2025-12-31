//go:build darwin || freebsd || netbsd

package git

import (
	"syscall"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

func init() {
	fillSystemInfo = func(e *index.Entry, sys any) {
		if os, ok := sys.(*syscall.Stat_t); ok {
			e.CreatedAt = time.Unix(os.Atimespec.Unix())
			e.Dev = uint32(os.Dev)   //nolint:gosec // G115: dev number fits in uint32
			e.Inode = uint32(os.Ino) //nolint:gosec // G115: inode fits in uint32
			e.GID = os.Gid
			e.UID = os.Uid
		}
	}
}

func isSymlinkWindowsNonAdmin(error) bool {
	return false
}
