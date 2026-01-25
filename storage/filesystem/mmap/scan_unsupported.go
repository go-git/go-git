//go:build !darwin && !linux

package mmap

import (
	"errors"

	"github.com/go-git/go-billy/v6"
)

type PackScanner struct{}

func NewPackScanner(hashSize int, pack, idx, rev billy.File) (*PackScanner, error) {
	return nil, errors.New("pack scanner is only supported in linux or darwin")
}
