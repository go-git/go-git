//go:build wasip1

package git

import (
	"syscall"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

func init() {
	fillSystemInfo = func(e *index.Entry, sys any) {
		if os, ok := sys.(*syscall.Stat_t); ok {
			e.CreatedAt = time.Unix(int64(os.Ctime), 0)
			e.Dev = uint32(os.Dev)
			e.Inode = uint32(os.Ino)
			e.GID = os.Gid
			e.UID = os.Uid
		}
	}
}

func isSymlinkWindowsNonAdmin(error) bool {
	return false
}

func preReceiveHook(string) []byte {
	return []byte{}
}
