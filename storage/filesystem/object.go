//nolint:revive // ObjectStorage methods implement storer interfaces
package filesystem

import (
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

type ObjectStorage struct {
	options Options

	// objectCache is an object cache used to cache delta's bases and also recently
	// loaded loose objects.
	objectCache cache.Object

	dir   *dotgit.DotGit
	index map[plumbing.Hash]idxfile.Index

	packList    []plumbing.Hash
	packListIdx int
	packfiles   map[plumbing.Hash]*packfile.Packfile
	muI         sync.RWMutex
	muP         sync.RWMutex

	oh *plumbing.ObjectHasher
}

// NewObjectStorage creates a new ObjectStorage with the given .git directory and cache.
func NewObjectStorage(dir *dotgit.DotGit, objectCache cache.Object) *ObjectStorage {
	return NewObjectStorageWithOptions(dir, objectCache, Options{})
}

// NewObjectStorageWithOptions creates a new ObjectStorage with the given .git directory, cache and extra options
func NewObjectStorageWithOptions(dir *dotgit.DotGit, objectCache cache.Object, ops Options) *ObjectStorage {
	return &ObjectStorage{
		options:     ops,
		objectCache: objectCache,
		dir:         dir,
		oh:          plumbing.FromObjectFormat(ops.ObjectFormat),
	}
}

func (s *ObjectStorage) requireIndex() error {
	s.muI.RLock()
	if s.index != nil {
		s.muI.RUnlock()
		return nil
	}
	s.muI.RUnlock()

	s.muI.Lock()
	defer s.muI.Unlock()

	s.index = make(map[plumbing.Hash]idxfile.Index)
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

// Reindex indexes again all packfiles. Useful if git changed packfiles externally
func (s *ObjectStorage) Reindex() {
	s.index = nil
}

func (s *ObjectStorage) loadIdxFile(h plumbing.Hash) (err error) {
	f, err := s.dir.ObjectPackIdx(h)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	idxf := idxfile.NewMemoryIndex(h.Size())
	d := idxfile.NewDecoder(f)
	if err = d.Decode(idxf); err != nil {
		return err
	}

	s.index[h] = idxf
	return err
}

func (s *ObjectStorage) RawObjectWriter(typ plumbing.ObjectType, sz int64) (w io.WriteCloser, err error) {
	ow, err := s.dir.NewObject()
	if err != nil {
		return nil, err
	}

	err = ow.WriteHeader(typ, sz)
	if err != nil {
		return nil, err
	}

	return ow, nil
}

func (s *ObjectStorage) NewEncodedObject() plumbing.EncodedObject {
	return plumbing.NewMemoryObject(s.oh)
}

func (s *ObjectStorage) PackfileWriter() (io.WriteCloser, error) {
	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	w, err := s.dir.NewObjectPack()
	if err != nil {
		return nil, err
	}

	w.Notify = func(h plumbing.Hash, writer *idxfile.Writer) {
		index, err := writer.Index()
		if err == nil {
			s.index[h] = index
		}
	}

	return w, nil
}

// SetEncodedObject adds a new object to the storage.
func (s *ObjectStorage) SetEncodedObject(o plumbing.EncodedObject) (h plumbing.Hash, err error) {
	if o.Type() == plumbing.OFSDeltaObject || o.Type() == plumbing.REFDeltaObject {
		return plumbing.ZeroHash, plumbing.ErrInvalidType
	}

	ow, err := s.dir.NewObject()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	defer ioutil.CheckClose(ow, &err)

	or, err := o.Reader()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	defer ioutil.CheckClose(or, &err)

	if err = ow.WriteHeader(o.Type(), o.Size()); err != nil {
		return plumbing.ZeroHash, err
	}

	if _, err = ioutil.CopyBufferPool(ow, or); err != nil {
		return plumbing.ZeroHash, err
	}

	return o.Hash(), err
}

// LazyWriter returns a lazy ObjectWriter that is bound to a DotGit file.
// It first write the header passing on the object type and size, so
// that the object contents can be written later, without the need to
// create a MemoryObject and buffering its entire contents into memory.
func (s *ObjectStorage) LazyWriter() (w io.WriteCloser, wh func(typ plumbing.ObjectType, sz int64) error, err error) {
	ow, err := s.dir.NewObject()
	if err != nil {
		return nil, nil, err
	}

	return ow, ow.WriteHeader, nil
}

// HasEncodedObject returns nil if the object exists, without actually
// reading the object data from storage.
func (s *ObjectStorage) HasEncodedObject(h plumbing.Hash) (err error) {
	// Check unpacked objects
	f, err := s.dir.Object(h)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// Fall through to check packed objects.
	} else {
		defer ioutil.CheckClose(f, &err)
		return nil
	}

	// Check packed objects.
	if err := s.requireIndex(); err != nil {
		return err
	}
	_, _, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return plumbing.ErrObjectNotFound
	}
	return nil
}

func (s *ObjectStorage) encodedObjectSizeFromUnpacked(h plumbing.Hash) (size int64, err error) {
	f, err := s.dir.Object(h)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, plumbing.ErrObjectNotFound
		}

		return 0, err
	}

	r, err := objfile.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer ioutil.CheckClose(r, &err)

	_, size, err = r.Header()
	return size, err
}

func (s *ObjectStorage) packfile(idx idxfile.Index, pack plumbing.Hash) (*packfile.Packfile, error) {
	if p := s.packfileFromCache(pack); p != nil {
		return p, nil
	}

	f, err := s.dir.ObjectPack(pack)
	if err != nil {
		return nil, err
	}

	p := packfile.NewPackfile(f,
		packfile.WithIdx(idx),
		packfile.WithFs(s.dir.Fs()),
		packfile.WithCache(s.objectCache),
		packfile.WithObjectIDSize(pack.Size()),
	)
	return p, s.storePackfileInCache(pack, p)
}

func (s *ObjectStorage) packfileFromCache(hash plumbing.Hash) *packfile.Packfile {
	s.muP.Lock()
	defer s.muP.Unlock()

	if s.packfiles == nil {
		if s.options.KeepDescriptors {
			s.packfiles = make(map[plumbing.Hash]*packfile.Packfile)
		} else if s.options.MaxOpenDescriptors > 0 {
			s.packList = make([]plumbing.Hash, s.options.MaxOpenDescriptors)
			s.packfiles = make(map[plumbing.Hash]*packfile.Packfile, s.options.MaxOpenDescriptors)
		}
	}

	return s.packfiles[hash]
}

func (s *ObjectStorage) storePackfileInCache(hash plumbing.Hash, p *packfile.Packfile) error {
	s.muP.Lock()
	defer s.muP.Unlock()

	if s.options.KeepDescriptors {
		s.packfiles[hash] = p
		return nil
	}

	if s.options.MaxOpenDescriptors <= 0 {
		return nil
	}

	// start over as the limit of packList is hit
	if s.packListIdx >= len(s.packList) {
		s.packListIdx = 0
	}

	// close the existing packfile if open
	if next := s.packList[s.packListIdx]; !next.IsZero() {
		open := s.packfiles[next]
		delete(s.packfiles, next)
		if open != nil {
			if err := open.Close(); err != nil {
				return err
			}
		}
	}

	// cache newly open packfile
	s.packList[s.packListIdx] = hash
	s.packfiles[hash] = p
	s.packListIdx++

	return nil
}

func (s *ObjectStorage) encodedObjectSizeFromPackfile(h plumbing.Hash) (size int64, err error) {
	if err := s.requireIndex(); err != nil {
		return 0, err
	}

	pack, _, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return 0, plumbing.ErrObjectNotFound
	}

	idx := s.index[pack]
	hash, err := idx.FindHash(offset)
	if err == nil {
		obj, ok := s.objectCache.Get(hash)
		if ok {
			return obj.Size(), nil
		}
	} else if err != nil && !errors.Is(err, plumbing.ErrObjectNotFound) {
		return 0, err
	}

	p, err := s.packfile(idx, pack)
	if err != nil {
		return 0, err
	}

	if !s.options.KeepDescriptors && s.options.MaxOpenDescriptors == 0 {
		defer ioutil.CheckClose(p, &err)
	}

	return p.GetSizeByOffset(offset)
}

// EncodedObjectSize returns the plaintext size of the given object,
// without actually reading the full object data from storage.
func (s *ObjectStorage) EncodedObjectSize(h plumbing.Hash) (size int64, err error) {
	size, err = s.encodedObjectSizeFromUnpacked(h)
	if err != nil && !errors.Is(err, plumbing.ErrObjectNotFound) {
		return 0, err
	} else if err == nil {
		return size, nil
	}

	return s.encodedObjectSizeFromPackfile(h)
}

// EncodedObject returns the object with the given hash, by searching for it in
// the packfile and the git object directories.
func (s *ObjectStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	var obj plumbing.EncodedObject
	var err error

	if s.index != nil {
		obj, err = s.getFromPackfile(h, false)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			obj, err = s.getFromUnpacked(h)
		}
	} else {
		obj, err = s.getFromUnpacked(h)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			obj, err = s.getFromPackfile(h, false)
		}
	}

	// If the error is still object not found, check if it's a shared object
	// repository.
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		dotgits, e := s.dir.Alternates()
		if e == nil {
			// Create a new object storage with the DotGit(s) and check for the
			// required hash object. Skip when not found.
			for _, dg := range dotgits {
				o := NewObjectStorage(dg, s.objectCache)
				enobj, enerr := o.EncodedObject(t, h)
				if enerr != nil {
					continue
				}
				return enobj, nil
			}
		}
	}

	if err != nil {
		return nil, err
	}

	if obj == nil || (plumbing.AnyObject != t && obj.Type() != t) {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

// DeltaObject returns the object with the given hash, by searching for
// it in the packfile and the git object directories.
func (s *ObjectStorage) DeltaObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.getFromUnpacked(h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		obj, err = s.getFromPackfile(h, true)
	}

	if err != nil {
		return nil, err
	}

	if plumbing.AnyObject != t && obj.Type() != t {
		return nil, plumbing.ErrObjectNotFound
	}

	return obj, nil
}

func (s *ObjectStorage) getFromUnpacked(h plumbing.Hash) (obj plumbing.EncodedObject, err error) {
	f, err := s.dir.Object(h)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, plumbing.ErrObjectNotFound
		}

		return nil, err
	}
	defer ioutil.CheckClose(f, &err)

	if cacheObj, found := s.objectCache.Get(h); found {
		return cacheObj, nil
	}

	r, err := objfile.NewReader(f)
	if err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(r, &err)

	t, size, err := r.Header()
	if err != nil {
		return nil, err
	}

	if s.options.LargeObjectThreshold > 0 && size > s.options.LargeObjectThreshold {
		obj = dotgit.NewEncodedObject(s.dir, h, t, size)
		return obj, nil
	}

	obj = s.NewEncodedObject()

	obj.SetType(t)
	obj.SetSize(size)
	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(w, &err)

	_, err = ioutil.CopyBufferPool(w, r)
	if err != nil {
		return nil, err
	}

	s.objectCache.Put(obj)

	return obj, nil
}

// Get returns the object with the given hash, by searching for it in
// the packfile.
func (s *ObjectStorage) getFromPackfile(h plumbing.Hash, canBeDelta bool) (plumbing.EncodedObject, error) {
	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	pack, hash, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return nil, plumbing.ErrObjectNotFound
	}

	s.muI.RLock()
	idx := s.index[pack]
	s.muI.RUnlock()

	p, err := s.packfile(idx, pack)
	if err != nil {
		return nil, err
	}

	if !s.options.KeepDescriptors && s.options.MaxOpenDescriptors == 0 {
		defer ioutil.CheckClose(p, &err)
	}

	if canBeDelta {
		return s.decodeDeltaObjectAt(p, offset, hash)
	}

	return p.GetByOffset(offset)
}

// TODO: refactor this logic into packfile package.
func (s *ObjectStorage) decodeDeltaObjectAt(
	p *packfile.Packfile,
	offset int64,
	hash plumbing.Hash,
) (plumbing.EncodedObject, error) {
	scan, err := p.Scanner() //nolint:staticcheck // TODO: Refactor to avoid deprecated Scanner method
	if err != nil {
		return nil, err
	}
	err = scan.SeekFromStart(offset)
	if err != nil {
		return nil, err
	}

	if !scan.Scan() {
		return nil, fmt.Errorf("failed to decode delta object")
	}

	header := scan.Data().Value().(packfile.ObjectHeader)

	var base plumbing.Hash

	switch header.Type {
	case plumbing.REFDeltaObject:
		base = header.Reference
	case plumbing.OFSDeltaObject:
		base, err = p.FindHash(header.OffsetReference)
		if err != nil {
			return nil, err
		}
	default:
		return p.GetByOffset(offset)
	}

	obj := &plumbing.MemoryObject{}
	obj.SetType(header.Type)
	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	if err := scan.WriteObject(&header, w); err != nil {
		return nil, err
	}

	return newDeltaObject(obj, hash, base, header.Size), nil
}

func (s *ObjectStorage) findObjectInPackfile(h plumbing.Hash) (plumbing.Hash, plumbing.Hash, int64) {
	defer s.muI.Unlock()
	s.muI.Lock()

	for packfile, index := range s.index {
		offset, err := index.FindOffset(h)
		if err == nil {
			return packfile, h, offset
		}
	}

	return plumbing.ZeroHash, plumbing.ZeroHash, -1
}

// HashesWithPrefix returns all objects with a hash that starts with a prefix by searching for
// them in the packfile and the git object directories.
func (s *ObjectStorage) HashesWithPrefix(prefix []byte) ([]plumbing.Hash, error) {
	hashes, err := s.dir.ObjectsWithPrefix(prefix)
	if err != nil {
		return nil, err
	}

	seen := hashListAsMap(hashes)

	// TODO: This could be faster with some idxfile changes,
	// or diving into the packfile.
	if err := s.requireIndex(); err != nil {
		return nil, err
	}
	for _, index := range s.index {
		ei, err := index.Entries()
		if err != nil {
			return nil, err
		}
		for {
			e, err := ei.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}
			if e.Hash.HasPrefix(prefix) {
				if _, ok := seen[e.Hash]; ok {
					continue
				}
				hashes = append(hashes, e.Hash)
			}
		}
		ei.Close()
	}

	return hashes, nil
}

// IterEncodedObjects returns an iterator for all the objects in the packfile
// with the given type.
func (s *ObjectStorage) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	objects, err := s.dir.Objects()
	if err != nil {
		return nil, err
	}

	seen := make(map[plumbing.Hash]struct{})
	var iters []storer.EncodedObjectIter
	if len(objects) != 0 {
		iters = append(iters, &objectsIter{s: s, t: t, h: objects})
		seen = hashListAsMap(objects)
	}

	packi, err := s.buildPackfileIters(t, seen)
	if err != nil {
		return nil, err
	}

	iters = append(iters, packi)
	return storer.NewMultiEncodedObjectIter(iters), nil
}

func (s *ObjectStorage) buildPackfileIters(
	t plumbing.ObjectType,
	seen map[plumbing.Hash]struct{},
) (storer.EncodedObjectIter, error) {
	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	packs, err := s.dir.ObjectPacks()
	if err != nil {
		return nil, err
	}
	return &lazyPackfilesIter{
		hashes: packs,
		open: func(h plumbing.Hash) (storer.EncodedObjectIter, error) {
			pack, err := s.dir.ObjectPack(h)
			if err != nil {
				return nil, err
			}
			return newPackfileIter(
				s.dir.Fs(), pack, t, seen, s.index[h],
				s.objectCache, s.options.KeepDescriptors, crypto.SHA1.Size(),
			)
		},
	}, nil
}

// Close closes all opened files.
func (s *ObjectStorage) Close() error {
	var firstError error

	s.muP.RLock()
	defer s.muP.RUnlock()

	if s.options.KeepDescriptors || s.options.MaxOpenDescriptors > 0 {
		for _, packfile := range s.packfiles {
			err := packfile.Close()
			if firstError == nil && err != nil {
				firstError = err
			}
		}
	}

	s.packfiles = nil
	s.dir.Close()

	return firstError
}

func hashListAsMap(l []plumbing.Hash) map[plumbing.Hash]struct{} {
	m := make(map[plumbing.Hash]struct{}, len(l))
	for _, h := range l {
		m[h] = struct{}{}
	}
	return m
}

func (s *ObjectStorage) ForEachObjectHash(fun func(plumbing.Hash) error) error {
	err := s.dir.ForEachObjectHash(fun)
	if err == storer.ErrStop {
		return nil
	}
	return err
}

func (s *ObjectStorage) LooseObjectTime(hash plumbing.Hash) (time.Time, error) {
	fi, err := s.dir.ObjectStat(hash)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

func (s *ObjectStorage) DeleteLooseObject(hash plumbing.Hash) error {
	return s.dir.ObjectDelete(hash)
}

func (s *ObjectStorage) ObjectPacks() ([]plumbing.Hash, error) {
	return s.dir.ObjectPacks()
}

func (s *ObjectStorage) DeleteOldObjectPackAndIndex(h plumbing.Hash, t time.Time) error {
	return s.dir.DeleteOldObjectPackAndIndex(h, t)
}
