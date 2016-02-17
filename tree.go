package git

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
)

// Tree is basically like a directory - it references a bunch of other trees
// and/or blobs (i.e. files and sub-directories)
type Tree struct {
	Entries map[string]TreeEntry
	Hash    core.Hash

	r *Repository
}

// TreeEntry represents a file
type TreeEntry struct {
	Name string
	Mode os.FileMode
	Hash core.Hash
}

// New errors defined by this package.
var ErrFileNotFound = errors.New("file not found")

// File returns the hash of the file identified by the `path` argument.
// The path is interpreted as relative to the tree receiver.
func (t *Tree) File(path string) (*File, error) {
	hash, err := t.findHash(path)
	if err != nil {
		return nil, ErrFileNotFound
	}

	obj, err := t.r.Storage.Get(*hash)
	if err != nil {
		if err == core.ObjectNotFoundErr {
			return nil, ErrFileNotFound // a git submodule
		}
		return nil, err
	}

	if obj.Type() != core.BlobObject {
		return nil, ErrFileNotFound // a directory
	}

	blob := &Blob{}
	blob.Decode(obj)

	return &File{Name: path, Reader: blob.Reader(), Hash: *hash}, nil
}

func (t *Tree) findHash(path string) (*core.Hash, error) {
	pathParts := strings.Split(path, "/")

	var tree *Tree
	var err error
	for tree = t; len(pathParts) > 1; pathParts = pathParts[1:] {
		if tree, err = tree.dir(pathParts[0]); err != nil {
			return nil, err
		}
	}

	entry, err := tree.entry(pathParts[0])
	if err != nil {
		return nil, err
	}

	return &entry.Hash, nil
}

var errDirNotFound = errors.New("directory not found")

func (t *Tree) dir(baseName string) (*Tree, error) {
	entry, err := t.entry(baseName)
	if err != nil {
		return nil, errDirNotFound
	}

	obj, err := t.r.Storage.Get(entry.Hash)
	if err != nil {
		if err == core.ObjectNotFoundErr { // git submodule
			return nil, errDirNotFound
		}
		return nil, err
	}

	if obj.Type() != core.TreeObject {
		return nil, errDirNotFound // a file
	}

	tree := &Tree{r: t.r}
	tree.Decode(obj)

	return tree, nil
}

var errEntryNotFound = errors.New("entry not found")

func (t *Tree) entry(baseName string) (*TreeEntry, error) {
	entry, ok := t.Entries[baseName]
	if !ok {
		return nil, errEntryNotFound
	}

	return &entry, nil
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
		obj, err := t.r.Storage.Get(entry.Hash)
		if err != nil {
			if err == core.ObjectNotFoundErr {
				continue // ignore entries without hash (= submodule dirs)
			}
			//FIXME: Refactor this function to return an error. Ideally this would be
			//       moved into a FileIter type.
		}

		if obj.Type() == core.TreeObject {
			tree := &Tree{r: t.r}
			tree.Decode(obj)
			tree.walkEntries(path.Join(base, entry.Name), ch)
			continue
		}

		blob := &Blob{}
		blob.Decode(obj)

		ch <- &File{Name: path.Join(base, entry.Name), Reader: blob.Reader(), Hash: entry.Hash}
	}
}

// Decode transform an core.Object into a Tree struct
func (t *Tree) Decode(o core.Object) error {
	if o.Type() != core.TreeObject {
		return ErrUnsupportedObject
	}

	t.Hash = o.Hash()
	if o.Size() == 0 {
		return nil
	}

	t.Entries = make(map[string]TreeEntry)

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

		baseName := name[:len(name)-1]
		t.Entries[baseName] = TreeEntry{
			Hash: hash,
			Mode: os.FileMode(fm),
			Name: baseName,
		}
	}

	return nil
}

type TreeIter struct {
	core.ObjectIter
	r *Repository
}

func NewTreeIter(r *Repository, iter core.ObjectIter) *TreeIter {
	return &TreeIter{iter, r}
}

func (iter *TreeIter) Next() (*Tree, error) {
	obj, err := iter.ObjectIter.Next()
	if err != nil {
		return nil, err
	}

	tree := &Tree{r: iter.r}
	return tree, tree.Decode(obj)
}
