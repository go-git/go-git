//go:build !plan9

package config

import (
	"errors"
	"syscall"
)

// isNotDir reports whether err says that a component of a path is not a
// directory, which git treats like a missing file when including one.
//
// https://github.com/git/git/blob/8103b446517e0c44e67561b9d0ccce56efa60a71/wrapper.c#L732-L737
func isNotDir(err error) bool {
	return errors.Is(err, syscall.ENOTDIR)
}
