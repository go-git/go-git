package seekable

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/formats/packfile"
	"gopkg.in/src-d/go-git.v3/storage/seekable/internal/gitdir"
	"gopkg.in/src-d/go-git.v3/storage/seekable/internal/index"
	"gopkg.in/src-d/go-git.v3/utils/fs"
)

// ObjectStorage is an implementation of core.ObjectStorage that stores
// data on disk in the standard git format (this is, the .git directory).
//
// Zero values of this type are not safe to use, see the New function below.
//
// Currently only reads are supported, no writting.
//
// Also values from this type are not yet able to track changes on disk, this is,
// Gitdir values will get outdated as soon as repositories change on disk.
type ObjectStorage struct {
	dir   *gitdir.GitDir
	index index.Index
}

// New returns a new ObjectStorage for the git directory at the specified path.
func New(fs fs.FS, path string) (*ObjectStorage, error) {
	s := &ObjectStorage{}

	var err error
	s.dir, err = gitdir.New(fs, path)
	if err != nil {
		return nil, err
	}

	s.index, err = buildIndex(s.dir)

	return s, err
}

func buildIndex(dir *gitdir.GitDir) (index.Index, error) {
	fs, idxfile, err := dir.Idxfile()
	if err != nil {
		if err == gitdir.ErrIdxNotFound {
			return buildIndexFromPackfile(dir)
		}
		return nil, err
	}

	return buildIndexFromIdxfile(fs, idxfile)
}

func buildIndexFromPackfile(dir *gitdir.GitDir) (index.Index, error) {
	fs, packfile, err := dir.Packfile()
	if err != nil {
		return nil, err
	}

	f, err := fs.Open(packfile)
	if err != nil {
		return nil, err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	return index.NewFromPackfile(f)
}

func buildIndexFromIdxfile(fs fs.FS, path string) (index.Index, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	return index.NewFromIdx(f)
}

// Set adds a new object to the storage. As this functionality is not
// yet supported, this method always returns a "not implemented yet"
// error an zero hash.
func (s *ObjectStorage) Set(core.Object) (core.Hash, error) {
	return core.ZeroHash, fmt.Errorf("not implemented yet")
}

// Get returns the object with the given hash, by searching for it in
// the packfile.
func (s *ObjectStorage) Get(h core.Hash) (core.Object, error) {
	offset, err := s.index.Get(h)
	if err != nil {
		return nil, err
	}

	fs, path, err := s.dir.Packfile()
	if err != nil {
		return nil, err
	}

	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	_, err = f.Seek(offset, os.SEEK_SET)
	if err != nil {
		return nil, err
	}

	r := packfile.NewSeekable(f)
	r.HashToOffset = map[core.Hash]int64(s.index)
	p := packfile.NewParser(r)

	return p.ReadObject()
}

// Iter returns an iterator for all the objects in the packfile with the
// given type.
func (s *ObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	var objects []core.Object

	for hash := range s.index {
		object, err := s.Get(hash)
		if err != nil {
			return nil, err
		}
		if object.Type() == t {
			objects = append(objects, object)
		}
	}

	return core.NewObjectSliceIter(objects), nil
}

const (
	headErrPrefix    = "cannot get HEAD reference:"
	symrefCapability = "symref"
	headRefPrefix    = "HEAD:"
)

// Head returns the hash of the HEAD reference
func (s *ObjectStorage) Head() (core.Hash, error) {
	cap, err := s.dir.Capabilities()
	if err != nil {
		return core.ZeroHash, fmt.Errorf("%s %s", headErrPrefix, err)
	}

	ok := cap.Supports(symrefCapability)
	if !ok {
		return core.ZeroHash,
			fmt.Errorf("%s symref capability not supported", headErrPrefix)
	}

	symrefs := cap.Get(symrefCapability)
	var headRef string
	for _, ref := range symrefs.Values {
		if strings.HasPrefix(ref, headRefPrefix) {
			headRef = strings.TrimPrefix(ref, headRefPrefix)
		}
	}
	if headRef == "" {
		return core.ZeroHash, fmt.Errorf("%s HEAD reference not found",
			headErrPrefix)
	}

	refs, err := s.dir.Refs()
	if err != nil {
		return core.ZeroHash, fmt.Errorf("%s %s", headErrPrefix, err)
	}

	head, ok := refs[headRef]
	if !ok {
		return core.ZeroHash, fmt.Errorf("%s reference %q not found",
			headErrPrefix, headRef)
	}

	return head, nil
}
