package git

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/src-d/go-git.v2/core"
)

// Tree is basically like a directory - it references a bunch of other trees
// and/or blobs (i.e. files and sub-directories)
type Tree struct {
	Entries []TreeEntry
	Hash    core.Hash

	r *Repository
}

// TreeEntry represents a file
type TreeEntry struct {
	Name string
	Mode os.FileMode
	Hash core.Hash
}

func (t *Tree) Files() chan *File {
	ch := make(chan *File, 1)

	go func() {
		defer func() { close(ch) }()
		t.walkEntries("", ch)
	}()

	return ch
}

func (t *Tree) walkEntries(base string, ch chan *File) {
	for _, entry := range t.Entries {
		obj, _ := t.r.Storage.Get(entry.Hash)
		if obj.Type() == core.TreeObject {
			tree := &Tree{r: t.r}
			tree.Decode(obj)
			tree.walkEntries(filepath.Join(base, entry.Name), ch)
			continue
		}

		blob := &Blob{}
		blob.Decode(obj)

		ch <- &File{Name: filepath.Join(base, entry.Name), Reader: blob.Reader(), Hash: entry.Hash}
	}
}

// Decode transform an core.Object into a Tree struct
func (t *Tree) Decode(o core.Object) error {
	t.Hash = o.Hash()
	if o.Size() == 0 {
		return nil
	}

	r := bufio.NewReader(o.Reader())
	for {
		mode, err := r.ReadString(' ')
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		fm, err := strconv.ParseInt(mode[:len(mode)-1], 8, 32)
		if err != nil && err != io.EOF {
			return err
		}

		name, err := r.ReadString(0)
		if err != nil && err != io.EOF {
			return err
		}

		var hash core.Hash
		_, err = r.Read(hash[:])
		if err != nil && err != io.EOF {
			return err
		}

		t.Entries = append(t.Entries, TreeEntry{
			Hash: hash,
			Mode: os.FileMode(fm),
			Name: name[:len(name)-1],
		})
	}

	return nil
}

type TreeIter struct {
	iter
}

func NewTreeIter(r *Repository) *TreeIter {
	return &TreeIter{newIter(r)}
}

func (i *TreeIter) Next() (*Tree, error) {
	obj := <-i.ch
	if obj == nil {
		return nil, io.EOF
	}

	tree := &Tree{r: i.r}
	return tree, tree.Decode(obj)
}
