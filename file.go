package git

import (
	"bytes"
	"io"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
)

// File represents git file objects.
type File struct {
	Name string
	io.Reader
	Hash core.Hash
}

// Contents returns the contents of a file as a string.
func (f *File) Contents() string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(f)
	return buf.String()
}

// Lines returns a slice of lines from the contents of a file, stripping
// all end of line characters. If the last line is empty (does not end
// in an end of line), it is also stripped.
func (f *File) Lines() []string {
	splits := strings.Split(f.Contents(), "\n")
	// remove the last line if it is empty
	if splits[len(splits)-1] == "" {
		return splits[:len(splits)-1]
	}
	return splits
}

type FileIter struct {
	w TreeWalker
}

func NewFileIter(r *Repository, t *Tree) *FileIter {
	return &FileIter{w: *NewTreeWalker(r, t)}
}

func (iter *FileIter) Next() (*File, error) {
	for {
		name, entry, obj, err := iter.w.Next()
		if err != nil {
			return nil, err
		}

		if obj.Type() != core.BlobObject {
			// Skip non-blob objects
			continue
		}

		blob := &Blob{}
		blob.Decode(obj)

		return &File{Name: name, Reader: blob.Reader(), Hash: entry.Hash}, nil
	}
}

func (iter *FileIter) Close() {
	iter.w.Close()
}
