// +build darwin freebsd netbsd openbsd

package git

import (
	"syscall"
	"time"

	"srcd.works/go-git.v4/plumbing/format/index"
)

func init() {
	fillSystemInfo = func(e *index.Entry, os *syscall.Stat_t) {
		e.CreatedAt = time.Unix(int64(os.Atimespec.Sec), int64(os.Atimespec.Nsec))
		e.Dev = uint32(os.Dev)
		e.Inode = uint32(os.Ino)
		e.GID = os.Gid
		e.UID = os.Uid
	}
}
