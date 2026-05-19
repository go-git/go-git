package filesystem

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/objfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ObjectStorage implements object storage backed by the filesystem.
type ObjectStorage struct {
	options Options

	// objectCache is an object cache used to cache delta's bases and also recently
	// loaded loose objects.
	objectCache cache.Object

	dir *dotgit.DotGit

	// index maps pack file hashes to their decoded indices.
	index map[plumbing.Hash]idxfile.Index
	muI   sync.RWMutex // Protects index map

	packList    []plumbing.Hash
	packListIdx int
	// packfiles caches open packfile handles when KeepDescriptors is enabled.
	packfiles map[plumbing.Hash]*packfile.Packfile
	muP       sync.RWMutex // Protects packfiles map and packList

	oh *plumbing.ObjectHasher

	// alternates holds cached ObjectStorage instances for alternate repositories.
	// Initialized lazily via initAlternates to avoid recreating them on every lookup.
	// Protected by muA; use findInAlternates for concurrent lookups.
	alternates     []*ObjectStorage
	alternatesInit bool
	alternatesErr  error
	muA            sync.RWMutex
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

// initAlternates initializes the cached alternate ObjectStorage instances.
// Uses double-checked locking to ensure thread-safe, one-time initialization.
// Returns a non-nil error only for real I/O failures; a missing alternates
// file (os.ErrNotExist) is silently ignored since alternates are optional.
func (s *ObjectStorage) initAlternates() error {
	s.muA.RLock()
	if s.alternatesInit {
		err := s.alternatesErr
		s.muA.RUnlock()
		return err
	}
	s.muA.RUnlock()

	s.muA.Lock()
	defer s.muA.Unlock()

	if s.alternatesInit {
		return s.alternatesErr
	}
	s.alternatesInit = true

	dotgits, err := s.dir.Alternates()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.alternatesErr = err
		}
		return s.alternatesErr
	}
	for _, dg := range dotgits {
		s.alternates = append(s.alternates,
			NewObjectStorageWithOptions(dg, s.objectCache, s.options))
	}
	return nil
}

// resetAlternates closes cached alternates and marks them for re-initialization.
// Must be called when the on-disk alternates list changes (e.g. via AddAlternate).
func (s *ObjectStorage) resetAlternates() {
	s.muA.Lock()
	defer s.muA.Unlock()

	for _, alt := range s.alternates {
		_ = alt.Close()
	}
	s.alternates = nil
	s.alternatesErr = nil
	s.alternatesInit = false
}

// findInAlternates concurrently searches alternate object stores using an
// errgroup bounded by GOMAXPROCS. The first alternate to succeed captures
// the result and cancels the remaining searches. The read lock on muA is
// held for the duration to prevent resetAlternates from closing in-use
// alternates.
func findInAlternates[T any](s *ObjectStorage, fn func(*ObjectStorage) (T, error)) (T, error) {
	var zero T

	if err := s.initAlternates(); err != nil {
		return zero, err
	}

	s.muA.RLock()
	defer s.muA.RUnlock()

	n := len(s.alternates)
	if n == 0 {
		return zero, plumbing.ErrObjectNotFound
	}
	if n == 1 {
		return fn(s.alternates[0])
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := new(errgroup.Group)
	g.SetLimit(runtime.GOMAXPROCS(0))

	var (
		foundVal  T
		foundOnce sync.Once
		found     bool
	)

	for _, alt := range s.alternates {
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			v, err := fn(alt)
			if err == nil {
				foundOnce.Do(func() {
					foundVal = v
					found = true
					cancel()
				})
				return nil
			}
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				return nil
			}
			return err
		})
	}

	err := g.Wait()
	if err != nil && !found {
		return zero, errors.Join(err, plumbing.ErrObjectNotFound)
	}
	return foundVal, nil
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
	s.muI.Lock()
	s.index = nil
	s.muI.Unlock()
}

func (s *ObjectStorage) loadIdxFile(h plumbing.Hash) error {
	if s.options.UseInMemoryIdx {
		return s.loadMemoryIndex(h)
	}

	// Use LazyIndex on a best-effort basis.
	if idx, err := s.loadLazyIndex(h); err == nil {
		// If an index already exists, and implements io.Closer, try to close it.
		if i, found := s.index[h]; found && i != nil {
			if closer, ok := i.(io.Closer); ok {
				_ = closer.Close()
			}
		}

		s.index[h] = idx
		return nil
	}

	return s.loadMemoryIndex(h)
}

func (s *ObjectStorage) loadLazyIndex(h plumbing.Hash) (*idxfile.LazyIndex, error) {
	openIdx := func() (idxfile.ReadAtCloser, error) {
		return s.dir.ObjectPackIdx(h)
	}
	openRev := func() (idxfile.ReadAtCloser, error) {
		return s.dir.OpenPackRev(h)
	}

	return idxfile.NewLazyIndex(openIdx, openRev, h)
}

func (s *ObjectStorage) loadMemoryIndex(h plumbing.Hash) (err error) {
	f, err := s.dir.ObjectPackIdx(h)
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)

	var hasher hash.Hash
	if h.Size() == crypto.SHA256.Size() {
		hasher = hash.New(crypto.SHA256)
	} else {
		hasher = hash.New(crypto.SHA1)
	}

	idxf := idxfile.NewMemoryIndex(h.Size())
	d := idxfile.NewDecoder(f, hasher)
	if err = d.Decode(idxf); err != nil {
		return err
	}

	if idxf.PackfileChecksum != h {
		return fmt.Errorf("%w: packfile mismatch: target is %q not %q",
			idxfile.ErrMalformedIdxFile, idxf.PackfileChecksum.String(), h.String())
	}

	s.index[h] = idxf
	return err
}

// RawObjectWriter returns a writer for a new loose object of the given type and size.
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

// NewEncodedObject returns a new in-memory encoded object.
func (s *ObjectStorage) NewEncodedObject() plumbing.EncodedObject {
	return plumbing.NewMemoryObject(s.oh)
}

// PackfileWriter returns a writer for creating a new packfile.
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
			s.muI.Lock()
			s.index[h] = index
			s.muI.Unlock()
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
	if offset != -1 {
		return nil
	}

	_, err = findInAlternates(s, func(alt *ObjectStorage) (struct{}, error) {
		return struct{}{}, alt.HasEncodedObject(h)
	})
	return err
}

func (s *ObjectStorage) encodedObjectSizeFromUnpacked(h plumbing.Hash) (size int64, err error) {
	f, err := s.dir.Object(h)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, plumbing.ErrObjectNotFound
		}

		return 0, err
	}

	r, err := objfile.NewReader(f, s.options.ObjectFormat)
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

	size, err = s.encodedObjectSizeFromPackfile(h)
	if err == nil {
		return size, nil
	}

	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		return 0, err
	}

	return findInAlternates(s, func(alt *ObjectStorage) (int64, error) {
		return alt.EncodedObjectSize(h)
	})
}

// EncodedObject returns the object with the given hash, by searching for it in
// the packfile and the git object directories.
func (s *ObjectStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	var obj plumbing.EncodedObject
	var err error

	s.muI.RLock()
	hasIndex := s.index != nil
	s.muI.RUnlock()

	if hasIndex {
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

	if errors.Is(err, plumbing.ErrObjectNotFound) {
		obj, err = findInAlternates(s, func(alt *ObjectStorage) (plumbing.EncodedObject, error) {
			return alt.EncodedObject(t, h)
		})
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

	r, err := objfile.NewReader(f, s.options.ObjectFormat)
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

	// Copy index values into slice while holding lock to avoid races during iteration
	s.muI.RLock()
	indices := make([]idxfile.Index, 0, len(s.index))
	for _, v := range s.index {
		indices = append(indices, v)
	}
	s.muI.RUnlock()

	for _, index := range indices {
		ei, err := index.Entries()
		if err != nil {
			return nil, err
		}
		for {
			e, err := ei.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				_ = ei.Close()
				return nil, err
			}
			if e.Hash.HasPrefix(prefix) {
				if _, ok := seen[e.Hash]; ok {
					continue
				}
				seen[e.Hash] = struct{}{}
				hashes = append(hashes, e.Hash)
			}
		}
		_ = ei.Close()
	}

	if err := s.initAlternates(); err != nil {
		return nil, err
	}
	s.muA.RLock()
	defer s.muA.RUnlock()
	for _, alt := range s.alternates {
		altHashes, err := alt.HashesWithPrefix(prefix)
		if err != nil {
			return nil, err
		}
		for _, h := range altHashes {
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
			idx := s.index[h]
			s.muI.RUnlock()
			return newPackfileIter(
				s.dir.Fs(), pack, t, seen, idx,
				s.objectCache, s.options.KeepDescriptors, h.Size(),
			)
		},
	}, nil
}

// Close closes all opened files including cached alternate storages.
func (s *ObjectStorage) Close() error {
	var firstError error

	s.muA.RLock()
	for _, alt := range s.alternates {
		if err := alt.Close(); err != nil && firstError == nil {
			firstError = err
		}
	}
	s.muA.RUnlock()

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

	// If the index being used implements io.Closer, make sure we call it.
	// LazyIndex.Close permanently disables the index and releases any
	// idle file descriptors. The same pattern applies to other Index
	// implementations that hold resources.
	s.muI.RLock()
	for _, idx := range s.index {
		if closer, ok := idx.(io.Closer); ok {
			if err := closer.Close(); firstError == nil && err != nil {
				firstError = err
			}
		}
	}
	s.muI.RUnlock()

	s.packfiles = nil
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

// ForEachObjectHash iterates over every object hash in the storage.
func (s *ObjectStorage) ForEachObjectHash(fun func(plumbing.Hash) error) error {
	err := s.dir.ForEachObjectHash(fun)
	if err == storer.ErrStop {
		return nil
	}
	return err
}

// LooseObjectTime returns the modification time of a loose object.
func (s *ObjectStorage) LooseObjectTime(hash plumbing.Hash) (time.Time, error) {
	fi, err := s.dir.ObjectStat(hash)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// DeleteLooseObject removes a loose object from storage.
func (s *ObjectStorage) DeleteLooseObject(hash plumbing.Hash) error {
	return s.dir.ObjectDelete(hash)
}

// ObjectPacks returns the list of packfile hashes.
func (s *ObjectStorage) ObjectPacks() ([]plumbing.Hash, error) {
	return s.dir.ObjectPacks()
}

// DeleteOldObjectPackAndIndex removes a pack and its index if older than t.
func (s *ObjectStorage) DeleteOldObjectPackAndIndex(h plumbing.Hash, t time.Time) error {
	return s.dir.DeleteOldObjectPackAndIndex(h, t)
}
