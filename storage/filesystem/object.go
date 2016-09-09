package filesystem

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/formats/objfile"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
	"gopkg.in/src-d/go-git.v4/storage/memory"
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
	dir   *dotgit.DotGit
	index map[core.Hash]index
}

func newObjectStorage(dir *dotgit.DotGit) (*ObjectStorage, error) {
	s := &ObjectStorage{
		dir:   dir,
		index: make(map[core.Hash]index, 0),
	}
	return s, s.loadIdxFiles()
}

func (s *ObjectStorage) loadIdxFiles() error {
	packs, err := s.dir.ObjectPacks()
	if err != nil {
		return err
	}

	for _, h := range packs {
		if err := s.loadIdxFile(h); err != nil {
			return err
		}
	}

	return nil
}

func (s *ObjectStorage) loadIdxFile(h core.Hash) error {
	idx, err := s.dir.ObjectPackIdx(h)
	if err != nil {
		return err
	}

	s.index[h] = make(index)
	return s.index[h].Decode(idx)
}

func (s *ObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

// Writer method not supported on Memory storage
func (s *ObjectStorage) Writer() (io.WriteCloser, error) {
	return s.dir.NewObjectPack()
}

// Set adds a new object to the storage. As this functionality is not
// yet supported, this method always returns a "not implemented yet"
// error an zero hash.
func (s *ObjectStorage) Set(core.Object) (core.Hash, error) {
	return core.ZeroHash, fmt.Errorf("not implemented yet")
}

// Get returns the object with the given hash, by searching for it in
// the packfile and the git object directories.
func (s *ObjectStorage) Get(t core.ObjectType, h core.Hash) (core.Object, error) {
	obj, err := s.getFromUnpacked(h)
	if err == core.ErrObjectNotFound {
		obj, err = s.getFromPackfile(h)
	}

	if err != nil {
		return nil, err
	}

	if core.AnyObject != t && obj.Type() != t {
		return nil, core.ErrObjectNotFound
	}

	return obj, nil
}

func (s *ObjectStorage) getFromUnpacked(h core.Hash) (obj core.Object, err error) {
	f, err := s.dir.Object(h)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.ErrObjectNotFound
		}

		return nil, err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	obj = s.NewObject()
	objReader, err := objfile.NewReader(f)
	if err != nil {
		return nil, err
	}

	defer func() {
		errClose := objReader.Close()
		if err == nil {
			err = errClose
		}
	}()

	if err := objReader.FillObject(obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// Get returns the object with the given hash, by searching for it in
// the packfile.
func (s *ObjectStorage) getFromPackfile(h core.Hash) (core.Object, error) {
	pack, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return nil, core.ErrObjectNotFound
	}

	f, err := s.dir.ObjectPack(pack)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	p := packfile.NewScanner(f)
	d := packfile.NewDecoder(p, memory.NewStorage().ObjectStorage())
	return d.ReadObjectAt(offset)
}

func (s *ObjectStorage) findObjectInPackfile(h core.Hash) (core.Hash, int64) {
	for packfile, index := range s.index {
		if offset, ok := index[h]; ok {
			return packfile, offset
		}
	}

	return core.ZeroHash, -1
}

// Iter returns an iterator for all the objects in the packfile with the
// given type.
func (s *ObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	var objects []core.Object

	hashes, err := s.dir.Objects()
	if err != nil {
		return nil, err
	}

	for _, hash := range hashes {
		object, err := s.getFromUnpacked(hash)
		if err != nil {
			return nil, err
		}
		if object.Type() == t {
			objects = append(objects, object)
		}
	}

	for hash := range s.index {
		object, err := s.getFromPackfile(hash)
		if err != nil {
			return nil, err
		}
		if t == core.AnyObject || object.Type() == t {
			objects = append(objects, object)
		}
	}

	return core.NewObjectSliceIter(objects), nil
}

func (o *ObjectStorage) Begin() core.TxObjectStorage {
	return &TxObjectStorage{}
}

type TxObjectStorage struct{}

func (tx *TxObjectStorage) Set(obj core.Object) (core.Hash, error) {
	return core.ZeroHash, fmt.Errorf("not implemented yet")
}

func (tx *TxObjectStorage) Get(core.ObjectType, core.Hash) (core.Object, error) {
	return nil, fmt.Errorf("not implemented yet")
}

func (tx *TxObjectStorage) Commit() error {
	return fmt.Errorf("not implemented yet")
}

func (tx *TxObjectStorage) Rollback() error {
	return fmt.Errorf("not implemented yet")
}

type index map[core.Hash]int64

func (i index) Decode(r io.Reader) error {
	idx := &idxfile.Idxfile{}

	d := idxfile.NewDecoder(r)
	if err := d.Decode(idx); err != nil {
		return err
	}

	for _, e := range idx.Entries {
		i[e.Hash] = int64(e.Offset)
	}

	return nil
}
