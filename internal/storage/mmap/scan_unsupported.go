//go:build !darwin && !linux

// Package mmap holds features that rely on the in-memory
// representation of git files for the filesystem storage.
package mmap

import (
	"errors"

	"github.com/go-git/go-billy/v6"
)

// PackScanner is not supported on this platform.
type PackScanner struct{}

// NewPackScanner is not supported on this platform and always returns an error.
func NewPackScanner(_ int, _, _, _ billy.File) (*PackScanner, error) {
	return nil, errors.New("pack scanner is only supported in linux or darwin")
}
