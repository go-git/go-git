package config

import (
	"errors"
	"io/fs"
)

// isAccessDenied reports whether err means the file cannot be read for
// lack of permission, which Windows reports as ERROR_ACCESS_DENIED rather
// than EACCES.
func isAccessDenied(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}
