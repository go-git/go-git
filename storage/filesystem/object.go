package filesystem

import (
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	billy "github.com/go-git/go-billy/v6"

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

	dir *dotgit.DotGit

	// packIndexes holds idx readers keyed by packfile hash.
	// Only the fanout table (1 KiB per index) is kept in memory;
	// all entry reads use ReadAt on the idx file.
	packIndexes map[plumbing.Hash]*packIndex

	// packFiles caches open pack file handles keyed by packfile hash.
	packFiles   map[plumbing.Hash]billy.File
	packList    []plumbing.Hash
	packListIdx int

	// deltaCache is a weighted LRU cache for resolved delta base content.
	deltaCache *deltaBaseCache

	muI sync.RWMutex // packIndexes
	muP sync.RWMutex // packFiles

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
		deltaCache:  newDeltaBaseCache(defaultDeltaCacheMaxBytes),
		oh:          plumbing.FromObjectFormat(ops.ObjectFormat),
	}
}

func (s *ObjectStorage) requireIndex() error {
	s.muI.RLock()
	if s.packIndexes != nil {
		s.muI.RUnlock()
		return nil
	}
	s.muI.RUnlock()

	s.muI.Lock()
	defer s.muI.Unlock()

	s.packIndexes = make(map[plumbing.Hash]*packIndex)
	packs, err := s.dir.ObjectPacks()
	if err != nil {
		return err
	}

	for _, h := range packs {
		if err := s.loadPackIndex(h); err != nil {
			return fmt.Errorf("load pack index %s: %w", h, err)
		}
	}

	return nil
}

// Reindex indexes again all packfiles. Useful if git changed packfiles externally
func (s *ObjectStorage) Reindex() {
	s.muI.Lock()
	defer s.muI.Unlock()

	for _, idx := range s.packIndexes {
		_ = idx.Close()
	}
	s.packIndexes = nil
}

func (s *ObjectStorage) loadPackIndex(h plumbing.Hash) (err error) {
	f, err := s.dir.ObjectPackIdx(h)
	if err != nil {
		return err
	}

	hashSize := crypto.SHA1.Size()
	if h.Size() == crypto.SHA256.Size() {
		hashSize = crypto.SHA256.Size()
	}

	idx, err := openPackIndex(f, hashSize)
	if err != nil {
		_ = f.Close()
		return err
	}

	s.packIndexes[h] = idx
	return nil
}

func (s *ObjectStorage) hashSize() int {
	if s.oh != nil {
		return s.oh.Size()
	}
	return crypto.SHA1.Size()
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
		_, _ = writer.Index()
		s.muI.Lock()
		if s.packIndexes != nil {
			_ = s.loadPackIndex(h)
		}
		s.muI.Unlock()
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
	_, offset := s.findObjectInPackfile(h)
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

// openPackFile returns an open pack file handle, using the cache if possible.
func (s *ObjectStorage) openPackFile(h plumbing.Hash) (billy.File, error) {
	if f := s.packFileFromCache(h); f != nil {
		return f, nil
	}

	f, err := s.dir.ObjectPack(h)
	if err != nil {
		return nil, err
	}

	if err := validatePackHeader(f); err != nil {
		_ = f.Close()
		return nil, err
	}

	return f, s.storePackFileInCache(h, f)
}

func (s *ObjectStorage) packFileFromCache(hash plumbing.Hash) billy.File {
	s.muP.Lock()
	defer s.muP.Unlock()

	if s.packFiles == nil {
		if s.options.KeepDescriptors {
			s.packFiles = make(map[plumbing.Hash]billy.File)
		} else if s.options.MaxOpenDescriptors > 0 {
			s.packList = make([]plumbing.Hash, s.options.MaxOpenDescriptors)
			s.packFiles = make(map[plumbing.Hash]billy.File, s.options.MaxOpenDescriptors)
		}
	}

	if s.packFiles == nil {
		return nil
	}

	return s.packFiles[hash]
}

func (s *ObjectStorage) storePackFileInCache(hash plumbing.Hash, f billy.File) error {
	s.muP.Lock()
	defer s.muP.Unlock()

	if s.options.KeepDescriptors {
		if s.packFiles == nil {
			s.packFiles = make(map[plumbing.Hash]billy.File)
		}
		s.packFiles[hash] = f
		return nil
	}

	if s.options.MaxOpenDescriptors <= 0 {
		return nil
	}

	if s.packFiles == nil {
		s.packList = make([]plumbing.Hash, s.options.MaxOpenDescriptors)
		s.packFiles = make(map[plumbing.Hash]billy.File, s.options.MaxOpenDescriptors)
	}

	// start over as the limit of packList is hit
	if s.packListIdx >= len(s.packList) {
		s.packListIdx = 0
	}

	// close the existing packfile if open
	if next := s.packList[s.packListIdx]; !next.IsZero() {
		open := s.packFiles[next]
		delete(s.packFiles, next)
		if open != nil {
			if err := open.Close(); err != nil {
				return err
			}
		}
	}

	// cache newly open packfile
	s.packList[s.packListIdx] = hash
	s.packFiles[hash] = f
	s.packListIdx++

	return nil
}

// TODO: This really shouldn't exist in the first place
func (s *ObjectStorage) encodedObjectSizeFromPackfile(h plumbing.Hash) (size int64, err error) {
	if err := s.requireIndex(); err != nil {
		return 0, err
	}

	packHash, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return 0, plumbing.ErrObjectNotFound
	}

	obj, ok := s.objectCache.Get(h)
	if ok {
		return obj.Size(), nil
	}

	f, err := s.openPackFile(packHash)
	if err != nil {
		return 0, err
	}

	closeAfter := !s.options.KeepDescriptors && s.options.MaxOpenDescriptors == 0
	if closeAfter {
		defer func() { _ = f.Close() }()
	}

	meta, err := readEntryMeta(f, offset, s.hashSize())
	if err != nil {
		return 0, err
	}

	if !meta.typ.IsDelta() {
		return meta.size, nil
	}

	return readDeltaDeclaredSize(f, meta.dataOffset)
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

	if s.packIndexes != nil {
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

// getFromPackfile retrieves an object from packed storage using
// two-stage delta chain resolution if deltified.
func (s *ObjectStorage) getFromPackfile(h plumbing.Hash, canBeDelta bool) (plumbing.EncodedObject, error) {
	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	packHash, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return nil, plumbing.ErrObjectNotFound
	}

	if obj, ok := s.objectCache.Get(h); ok {
		return obj, nil
	}

	f, err := s.openPackFile(packHash)
	if err != nil {
		return nil, err
	}

	closeAfter := !s.options.KeepDescriptors && s.options.MaxOpenDescriptors == 0
	if closeAfter {
		defer func() { _ = f.Close() }()
	}

	meta, err := readEntryMeta(f, offset, s.hashSize())
	if err != nil {
		return nil, err
	}

	if canBeDelta && meta.typ.IsDelta() {
		return s.getDeltaObject(f, meta, h, offset, packHash)
	}

	if !meta.typ.IsDelta() {
		return s.readBaseObject(f, meta, h)
	}

	return s.resolvePackedDelta(f, meta, h, offset, packHash)
}

// readBaseObject inflates a non-delta object directly from the pack file.
func (s *ObjectStorage) readBaseObject(f billy.File, meta entryMeta, h plumbing.Hash) (plumbing.EncodedObject, error) {
	content, err := inflateFromPack(f, meta.dataOffset, meta.size)
	if err != nil {
		return nil, fmt.Errorf("read base object %s: %w", h, err)
	}

	obj := s.NewEncodedObject()
	obj.SetType(meta.typ)
	obj.SetSize(meta.size)

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	_, err = w.Write(content)
	closeErr := w.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	s.objectCache.Put(obj)
	return obj, nil
}

// resolvePackedDelta builds a delta chain and resolves it to produce the final object.
func (s *ObjectStorage) resolvePackedDelta(
	f billy.File,
	meta entryMeta,
	h plumbing.Hash,
	offset int64,
	packHash plumbing.Hash,
) (plumbing.EncodedObject, error) {
	packFiles, closeFiles, err := s.collectPackFiles(packHash, f)
	if err != nil {
		return nil, err
	}
	defer closeFiles()

	s.muI.RLock()
	indexes := s.packIndexes
	s.muI.RUnlock()

	chain, err := buildDeltaChain(meta, packHash, offset, packFiles, indexes, s.hashSize())
	if err != nil {
		return nil, fmt.Errorf("build delta chain for %s: %w", h, err)
	}

	declaredSize, err := readDeltaDeclaredSize(f, meta.dataOffset)
	if err != nil {
		return nil, fmt.Errorf("read delta size for %s: %w", h, err)
	}

	typ, content, err := resolveDeltaChain(chain, packFiles, s.deltaCache, declaredSize)
	if err != nil {
		return nil, fmt.Errorf("resolve delta chain for %s: %w", h, err)
	}

	obj := s.NewEncodedObject()
	obj.SetType(typ)
	obj.SetSize(int64(len(content)))

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	_, err = w.Write(content)
	closeErr := w.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	s.objectCache.Put(obj)
	return obj, nil
}

// getDeltaObject returns a raw delta object without resolving the delta chain.
func (s *ObjectStorage) getDeltaObject(
	f billy.File,
	meta entryMeta,
	h plumbing.Hash,
	offset int64,
	packHash plumbing.Hash,
) (plumbing.EncodedObject, error) {
	deltaContent, err := inflateFromPack(f, meta.dataOffset, -1)
	if err != nil {
		return nil, fmt.Errorf("inflate delta for %s: %w", h, err)
	}

	obj := s.NewEncodedObject()
	obj.SetType(meta.typ)
	obj.SetSize(int64(len(deltaContent)))

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}

	_, err = w.Write(deltaContent)
	closeErr := w.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	var baseHash plumbing.Hash
	switch meta.typ {
	case plumbing.REFDeltaObject:
		baseHash = meta.baseRefHash
	case plumbing.OFSDeltaObject:
		s.muI.RLock()
		idx := s.packIndexes[packHash]
		s.muI.RUnlock()
		if idx != nil {
			// TODO: still horrible.
			baseHash, _ = idx.FindHash(meta.baseOfsOffset)
		}
	}

	return packfile.NewDeltaObject(obj, h, baseHash, 0, meta.size, nil), nil
}

// collectPackFiles assembles a map of all available pack file handles
// for delta chain resolution.
func (s *ObjectStorage) collectPackFiles(primaryPack plumbing.Hash, primaryFile billy.File) (map[plumbing.Hash]billy.File, func(), error) {
	files := map[plumbing.Hash]billy.File{primaryPack: primaryFile}
	var toClose []billy.File

	s.muI.RLock()
	indexes := s.packIndexes
	s.muI.RUnlock()

	for ph := range indexes {
		if ph == primaryPack {
			continue
		}
		f, err := s.dir.ObjectPack(ph)
		if err != nil {
			for _, cf := range toClose {
				_ = cf.Close()
			}
			return nil, nil, err
		}
		files[ph] = f
		toClose = append(toClose, f)
	}

	cleanup := func() {
		for _, cf := range toClose {
			_ = cf.Close()
		}
	}

	return files, cleanup, nil
}

func (s *ObjectStorage) findObjectInPackfile(h plumbing.Hash) (plumbing.Hash, int64) {
	s.muI.RLock()
	defer s.muI.RUnlock()

	for packHash, idx := range s.packIndexes {
		offset, err := idx.FindOffset(h)
		if err == nil {
			return packHash, offset
		}
	}

	return plumbing.ZeroHash, -1
}

// HashesWithPrefix returns all objects with a hash that starts with a prefix by searching for
// them in the packfile and the git object directories.
func (s *ObjectStorage) HashesWithPrefix(prefix []byte) ([]plumbing.Hash, error) {
	hashes, err := s.dir.ObjectsWithPrefix(prefix)
	if err != nil {
		return nil, err
	}

	seen := hashListAsMap(hashes)

	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	s.muI.RLock()
	defer s.muI.RUnlock()

	for _, idx := range s.packIndexes {
		matches, err := idx.HashesWithPrefix(prefix)
		if err != nil {
			return nil, err
		}
		for _, h := range matches {
			if _, ok := seen[h]; ok {
				continue
			}
			seen[h] = struct{}{}
			hashes = append(hashes, h)
		}
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

			s.muI.RLock()
			idx := s.packIndexes[h]
			s.muI.RUnlock()

			return newPackfileIterFromPackIndex(
				s, pack, t, seen, idx, h,
			)
		},
	}, nil
}

// Close closes all opened files.
func (s *ObjectStorage) Close() error {
	var firstError error

	s.muP.RLock()
	if s.options.KeepDescriptors || s.options.MaxOpenDescriptors > 0 {
		for _, f := range s.packFiles {
			err := f.Close()
			if firstError == nil && err != nil {
				firstError = err
			}
		}
	}
	s.muP.RUnlock()

	s.muP.Lock()
	s.packFiles = nil
	s.muP.Unlock()

	s.muI.Lock()
	for _, idx := range s.packIndexes {
		err := idx.Close()
		if firstError == nil && err != nil {
			firstError = err
		}
	}
	s.packIndexes = nil
	s.muI.Unlock()

	if s.deltaCache != nil {
		s.deltaCache.clear()
	}

	_ = s.dir.Close()

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
