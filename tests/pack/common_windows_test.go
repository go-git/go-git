//go:build windows

package pack_test

import (
	"github.com/go-git/go-billy/v6"
)

func newPackScanner(_, _, _ billy.File) packHandler[uint64] {
	return nil
}
