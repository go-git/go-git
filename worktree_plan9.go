package git

import (
	"syscall"

	"github.com/go-git/go-git/v6/plumbing/format/index"
)

func init() {
	fillSystemInfo = func(e *index.Entry, sys any) {
		if os, ok := sys.(*syscall.Dir); ok {
			e.Dev = uint32(os.Dev)

			// Plan 9 has no Inode.
			// ext2srv(4) appears to store Inode in Qid.Path.
			e.Inode = uint32(os.Qid.Path)

			// Plan 9 has string UID/GID
			e.GID = 0
			e.UID = 0
		}
	}
}

func isSymlinkWindowsNonAdmin(error) bool {
	return true
}
