package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/src-d/go-git.v3"
	gogitFS "gopkg.in/src-d/go-git.v3/utils/fs"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(1)
	}

	fs := newFS(os.Args[1])

	repo, err := git.NewRepositoryFromFS(fs, ".git")
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}

	iter, err := repo.Commits()
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
	defer iter.Close()

	for {
		commit, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		fmt.Println(commit)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "%s <path to .git dir>", os.Args[0])
}

// A simple proxy filesystem example: It mimics local filesystems, using
// 'base' as its root and a funny path separator ("--").
//
// Example: when constructed with 'newFS("tmp")', a path like 'foo--bar'
// will represent the local path "/tmp/foo/bar".
type fs struct {
	base string
}

const separator = "--"

func newFS(path string) *fs {
	return &fs{
		base: path,
	}
}

func (fs *fs) Stat(path string) (info os.FileInfo, err error) {
	f, err := os.Open(fs.ToReal(path))
	if err != nil {
		return nil, err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	return f.Stat()
}

func (fs *fs) ToReal(path string) string {
	parts := strings.Split(path, separator)
	return filepath.Join(fs.base, filepath.Join(parts...))
}

func (fs *fs) Open(path string) (gogitFS.ReadSeekCloser, error) {
	return os.Open(fs.ToReal(path))
}

func (fs *fs) ReadDir(path string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(fs.ToReal(path))
}

func (fs *fs) Join(elem ...string) string {
	return strings.Join(elem, separator)
}
