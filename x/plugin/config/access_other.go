//go:build !windows

package config

import (
	"errors"
	"syscall"
)

// isAccessDenied reports whether err is EACCES, the one error besides a
// missing file that Git ignores with ACCESS_EACCES_OK [1]. EPERM, which
// fs.ErrPermission also matches, is not ignored.
//
// [1]: https://github.com/git/git/blob/v2.54.0/wrapper.c#L635-L639
func isAccessDenied(err error) bool {
	return errors.Is(err, syscall.EACCES)
}
