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
	"gopkg.in/src-d/go-git.v4/utils/fs"
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
	w, err := s.dir.NewObjectPack()
	if err != nil {
		return nil, err
	}

	w.Notify = func(h core.Hash, idx idxfile.Idxfile) {
		s.index[h] = make(index)
		for _, e := range idx.Entries {
			s.index[h][e.Hash] = int64(e.Offset)
		}
	}

	return w, nil
}

// Set adds a new object to the storage. As this functionality is not
// yet supported, this method always returns a "not implemented yet"
// error an zero hash.
func (s *ObjectStorage) Set(core.Object) (core.Hash, error) {
	return core.ZeroHash, fmt.Errorf("set - not implemented yet")
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

	defer f.Close()

	obj = s.NewObject()
	r, err := objfile.NewReader(f)
	if err != nil {
		return nil, err
	}

	defer r.Close()

	t, size, err := r.Header()
	if err != nil {
		return nil, err
	}

	obj.SetType(t)
	obj.SetSize(size)
	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(w, r)
	return obj, err
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
	d, err := packfile.NewDecoder(p, memory.NewStorage().ObjectStorage())
	if err != nil {
		return nil, err
	}

	d.SetOffsets(s.index[pack])
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
	objects, err := s.dir.Objects()
	if err != nil {
		return nil, err
	}

	seen := make(map[core.Hash]bool, 0)
	var iters []core.ObjectIter
	if len(objects) != 0 {
		iters = append(iters, &objectsIter{s: s, t: t, h: objects})
		seen = hashListAsMap(objects)
	}

	packi, err := s.buildPackfileIters(t, seen)
	if err != nil {
		return nil, err
	}

	iters = append(iters, packi...)
	return core.NewMultiObjectIter(iters), nil
}

func (s *ObjectStorage) buildPackfileIters(
	t core.ObjectType, seen map[core.Hash]bool) ([]core.ObjectIter, error) {
	packs, err := s.dir.ObjectPacks()
	if err != nil {
		return nil, err
	}

	var iters []core.ObjectIter
	for _, h := range packs {
		pack, err := s.dir.ObjectPack(h)
		if err != nil {
			return nil, err
		}

		iter, err := newPackfileIter(pack, t, seen)
		if err != nil {
			return nil, err
		}

		iters = append(iters, iter)
	}

	return iters, nil
}

func (o *ObjectStorage) Begin() core.TxObjectStorage {
	return &TxObjectStorage{}
}

type TxObjectStorage struct{}

func (tx *TxObjectStorage) Set(obj core.Object) (core.Hash, error) {
	return core.ZeroHash, fmt.Errorf("tx.Set - not implemented yet")
}

func (tx *TxObjectStorage) Get(core.ObjectType, core.Hash) (core.Object, error) {
	return nil, fmt.Errorf("tx.Get - not implemented yet")
}

func (tx *TxObjectStorage) Commit() error {
	return fmt.Errorf("tx.Commit - not implemented yet")
}

func (tx *TxObjectStorage) Rollback() error {
	return fmt.Errorf("tx.Rollback - not implemented yet")
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

type packfileIter struct {
	f fs.File
	d *packfile.Decoder
	t core.ObjectType

	seen     map[core.Hash]bool
	position uint32
	total    uint32
}

func newPackfileIter(f fs.File, t core.ObjectType, seen map[core.Hash]bool) (core.ObjectIter, error) {
	s := packfile.NewScanner(f)
	_, total, err := s.Header()
	if err != nil {
		return nil, err
	}

	d, err := packfile.NewDecoder(s, memory.NewStorage().ObjectStorage())
	if err != nil {
		return nil, err
	}

	return &packfileIter{
		f: f,
		d: d,
		t: t,

		total: total,
		seen:  seen,
	}, nil
}

func (iter *packfileIter) Next() (core.Object, error) {
	if iter.position >= iter.total {
		return nil, io.EOF
	}

	obj, err := iter.d.ReadObject()
	if err != nil {
		return nil, err
	}

	iter.position++
	if iter.seen[obj.Hash()] {
		return iter.Next()
	}

	if iter.t != core.AnyObject && iter.t != obj.Type() {
		return iter.Next()
	}

	return obj, nil
}

// ForEach is never called since is used inside of a MultiObjectIterator
func (iter *packfileIter) ForEach(cb func(core.Object) error) error {
	return nil
}

func (iter *packfileIter) Close() {
	iter.f.Close()
	iter.d.Close()
}

type objectsIter struct {
	s *ObjectStorage
	t core.ObjectType
	h []core.Hash
}

func (iter *objectsIter) Next() (core.Object, error) {
	if len(iter.h) == 0 {
		return nil, io.EOF
	}

	obj, err := iter.s.getFromUnpacked(iter.h[0])
	iter.h = iter.h[1:]

	if err != nil {
		return nil, err
	}

	if iter.t != core.AnyObject && iter.t != obj.Type() {
		return iter.Next()
	}

	return obj, err
}

// ForEach is never called since is used inside of a MultiObjectIterator
func (iter *objectsIter) ForEach(cb func(core.Object) error) error {
	return nil
}

func (iter *objectsIter) Close() {
	iter.h = []core.Hash{}
}

func hashListAsMap(l []core.Hash) map[core.Hash]bool {
	m := make(map[core.Hash]bool, len(l))
	for _, h := range l {
		m[h] = true
	}

	return m
}
