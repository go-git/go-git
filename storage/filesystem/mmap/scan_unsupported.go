//go:build !darwin && !linux

// Package mmap holds features that rely on the in-memory
// representation of git files for the filesystem storage.
package mmap

import (
	"errors"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

// PackScanner is not supported on this platform.
type PackScanner struct{}

// NewPackScanner is not supported on this platform and always returns an error.
func NewPackScanner(_ hash.ObjectFormat, _, _, _ billy.File) (*PackScanner, error) {
	return nil, errors.New("pack scanner is only supported in linux or darwin")
}
