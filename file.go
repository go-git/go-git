package git

import (
	"bytes"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
)

// File represents git file objects.
type File struct {
	Name string
	Blob
}

func newFile(name string, b *Blob) *File {
	return &File{Name: name, Blob: *b}
}

// Contents returns the contents of a file as a string.
func (f *File) Contents() string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(f.Reader())

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
		name, _, obj, err := iter.w.Next()
		if err != nil {
			return nil, err
		}

		if obj.Type() != core.BlobObject {
			// Skip non-blob objects
			continue
		}

		blob := &Blob{}
		blob.Decode(obj)

		return newFile(name, blob), nil
	}
}

func (iter *FileIter) Close() {
	iter.w.Close()
}
