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
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

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

// indexSFKey is the singleflight key used by [ObjectStorage.requireIndex]
// to coalesce concurrent first-readers around a single populateIndex
// scan. The literal value is opaque; only its uniqueness within
// indexSF matters.
const indexSFKey = "populate"

// reindexSFKey is the singleflight key used by [ObjectStorage.Reindex]
// to collapse concurrent rescans. Kept distinct from indexSFKey so a
// cold first-load and an externally-driven rescan do not deduplicate
// against each other.
const reindexSFKey = "reindex"

// packEntry pairs a pack hash with its idxfile.Index for inclusion
// in ObjectStorage.packs. The slice is always reassigned (never
// modified in place) so readers can hold a stable snapshot of the
// slice header after releasing muI.RLock.
type packEntry struct {
	h   plumbing.Hash
	idx idxfile.Index
}

// ObjectStorage implements object storage backed by the filesystem.
type ObjectStorage struct {
	options Options

	// objectCache is an object cache used to cache delta's bases and also recently
	// loaded loose objects.
	objectCache cache.Object

	dir   *dotgit.DotGit
	index map[plumbing.Hash]idxfile.Index
	// packs mirrors s.index as a slice of (hash, idx) pairs,
	// written in lockstep with s.index under muI.Lock and always
	// reassigned (never modified in place). Readers can snapshot
	// the slice header under RLock and release the lock before
	// any per-pack I/O — the backing array and the embedded idx
	// pointers stay valid for the snapshot's lifetime, so slow
	// LazyIndex FindOffset calls do not block a concurrent
	// Reindex on muI.Lock.
	packs []packEntry
	muI   sync.RWMutex

	// indexSF coalesces concurrent first-readers so populateIndex
	// runs once per cold-load even under thundering-herd contention.
	indexSF singleflight.Group

	// lastHitPackIdx records the s.packs index that served the most
	// recent successful findObjectInPackfile probe, encoded as the
	// slice position plus one (0 = no hint). Storing an Int32 instead
	// of a *plumbing.Hash eliminates the per-call escape-to-heap of
	// the previous MRU pointer design. Stale entries cost one
	// MayContain + FindOffset but never misroute lookups.
	lastHitPackIdx atomic.Int32

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
func findInAlternates[T any](ctx context.Context, s *ObjectStorage, fn func(*ObjectStorage) (T, error)) (T, error) {
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

	ctx, cancel := context.WithCancel(ctx)
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

// requireIndex ensures s.index is populated, performing a cold-load
// on first access. Concurrent first-readers are coalesced via
// singleflight so populateIndex runs once per thundering herd; the
// winning goroutine publishes the local map under s.muI.Lock and
// joiners block in singleflight.Do until the in-flight call returns.
func (s *ObjectStorage) requireIndex() error {
	s.muI.RLock()
	if s.index != nil {
		s.muI.RUnlock()
		return nil
	}
	s.muI.RUnlock()

	_, err, _ := s.indexSF.Do(indexSFKey, func() (any, error) {
		// Re-check inside the singleflight window: a racing winner
		// may have already published, in which case there is
		// nothing for this caller to do.
		s.muI.RLock()
		if s.index != nil {
			s.muI.RUnlock()
			return nil, nil
		}
		s.muI.RUnlock()

		local, entries, err := s.populateIndex()
		if err != nil {
			return nil, err
		}

		s.muI.Lock()
		if s.index == nil {
			s.index = local
			s.packs = entries
		} else {
			// A racing winner published while we were loading.
			// Close any indexes we built so SharedFile refcounts
			// (held by LazyIndex) do not leak.
			for _, idx := range local {
				_ = idx.Close()
			}
		}
		s.muI.Unlock()
		return nil, nil
	})
	return err
}

// Reindex re-populates s.index from disk and atomically swaps the
// freshly-loaded map and packs slice in, so the next read is a
// hot-cache hit. Call when the on-disk pack inventory has changed
// externally.
//
// Concurrent Reindex calls coalesce through indexSF on reindexSFKey:
// one goroutine performs populateIndex and the swap, the rest block
// in singleflight.Do and observe the same outcome. Without this the
// callers would each scan the disk and race to publish, producing
// duplicate I/O.
//
// populateIndex runs before the swap so s.index is never nil during
// Reindex: a concurrent PackfileWriter.Notify can rely on s.index
// being a writable map under muI.Lock. Previously-cached LazyIndex
// entries are not closed here; in-flight readers that borrowed an
// entry before the swap keep using it, and the underlying
// SharedFile FDs close via the grace timer (no-pool mode) or fdpool
// LRU eviction (pool mode) once they fall out of use. This mirrors
// canonical Git's reprepare model where packfile_store_reprepare
// leaves existing packs in place and only the FD-level LRU evicts.
//
// The MRU hint indexes into the previous packs slice and is reset
// after the swap so a stale hint cannot misroute a probe against
// the new slice.
func (s *ObjectStorage) Reindex() error {
	_, err, _ := s.indexSF.Do(reindexSFKey, func() (any, error) {
		local, entries, err := s.populateIndex()
		if err != nil {
			return nil, err
		}

		s.muI.Lock()
		s.index = local
		s.packs = entries
		s.lastHitPackIdx.Store(0)
		s.muI.Unlock()

		return nil, nil
	})
	return err
}

// populateIndex loads every pack's idx in parallel and returns the
// resulting map. The caller is responsible for publishing the map
// into s.index under s.muI.Lock; populateIndex itself takes no
// locks on s.muI, so callers must not hold it while invoking.
func (s *ObjectStorage) populateIndex() (map[plumbing.Hash]idxfile.Index, []packEntry, error) {
	packHashes, err := s.dir.ObjectPacks()
	if err != nil {
		return nil, nil, err
	}

	// Per-pack writes target disjoint slice positions, so no mutex
	// is needed across the errgroup workers.
	entries := make([]packEntry, len(packHashes))
	g := new(errgroup.Group)
	g.SetLimit(runtime.GOMAXPROCS(0))

	for i, h := range packHashes {
		g.Go(func() error {
			idx, err := s.loadIdx(h)
			if err != nil {
				return err
			}
			entries[i] = packEntry{h: h, idx: idx}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		// Best-effort cleanup of indexes that did finish before
		// the failing one: close any that hold descriptors so we
		// do not leak SharedFile refcounts on the error path.
		for _, e := range entries {
			if e.idx == nil {
				continue
			}
			_ = e.idx.Close()
		}
		return nil, nil, err
	}

	local := make(map[plumbing.Hash]idxfile.Index, len(entries))
	for _, e := range entries {
		local[e.h] = e.idx
	}
	return local, entries, nil
}

// loadIdx loads a single pack's idx and returns the constructed
// idxfile.Index. It does not mutate s.index; callers are
// responsible for installation.
func (s *ObjectStorage) loadIdx(h plumbing.Hash) (idxfile.Index, error) {
	if !s.options.UseInMemoryIdx {
		// Use LazyIndex on a best-effort basis; fall through to
		// MemoryIndex if construction fails (e.g. a malformed
		// .rev file), matching the legacy loadIdxFile path.
		if idx, err := s.loadLazyIndex(h); err == nil {
			return idx, nil
		}
	}
	return s.loadMemoryIndexValue(h)
}

func (s *ObjectStorage) loadLazyIndex(h plumbing.Hash) (*idxfile.LazyIndex, error) {
	openIdx := func() (idxfile.ReadAtCloser, error) {
		return s.dir.ObjectPackIdx(h)
	}
	openRev := func() (idxfile.ReadAtCloser, error) {
		return s.dir.OpenPackRev(h)
	}

	return idxfile.NewLazyIndexWithPool(openIdx, openRev, h, s.options.Pool)
}

// loadMemoryIndexValue decodes a pack's idx into a MemoryIndex and
// returns it. Unlike the now-removed loadMemoryIndex helper it does
// not write into s.index; the caller installs the returned value.
func (s *ObjectStorage) loadMemoryIndexValue(h plumbing.Hash) (idx idxfile.Index, err error) {
	f, err := s.dir.ObjectPackIdx(h)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	if idxf.PackfileChecksum != h {
		return nil, fmt.Errorf("%w: packfile mismatch: target is %q not %q",
			idxfile.ErrMalformedIdxFile, idxf.PackfileChecksum.String(), h.String())
	}

	return idxf, err
}

// RawObjectWriter returns a writer for a new loose object of the given type and size.
//
// TODO(ctx): propagate ctx into dotgit; it currently stops at this boundary.
func (s *ObjectStorage) RawObjectWriter(ctx context.Context, typ plumbing.ObjectType, sz int64) (w io.WriteCloser, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
func (s *ObjectStorage) PackfileWriter(ctx context.Context) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	w, err := s.dir.NewObjectPack()
	if err != nil {
		return nil, err
	}

	w.Notify = func(h plumbing.Hash, writer *idxfile.Writer) {
		index, err := writer.Index()
		if err != nil {
			return
		}
		s.muI.Lock()
		if _, existed := s.index[h]; !existed {
			// Copy-on-grow rather than append-in-place so any
			// reader that snapshotted the old slice header keeps
			// seeing a stable backing array.
			next := make([]packEntry, len(s.packs)+1)
			copy(next, s.packs)
			next[len(s.packs)] = packEntry{h: h, idx: index}
			s.packs = next
		}
		s.index[h] = index
		s.muI.Unlock()
	}

	return w, nil
}

// SetEncodedObject adds a new object to the storage.
func (s *ObjectStorage) SetEncodedObject(ctx context.Context, o plumbing.EncodedObject) (h plumbing.Hash, err error) {
	if err := ctx.Err(); err != nil {
		return plumbing.ZeroHash, err
	}

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
func (s *ObjectStorage) HasEncodedObject(ctx context.Context, h plumbing.Hash) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Pack-membership-first when the index is healthy: a hit on
	// the in-memory fanout shortcut avoids a loose Stat. If the
	// index fails to load (e.g. a corrupt .idx on disk), fall
	// through so a loose object can still answer the probe and
	// the lookup degrades to loose-only — mirrors canonical Git's
	// tolerance of partial pack-store corruption.
	idxErr := s.requireIndex()
	if idxErr == nil {
		if _, _, offset := s.findObjectInPackfile(h); offset != -1 {
			return nil
		}
	}

	// Existence-only on the loose path: Stat instead of Open
	// avoids a per-call open()+close() pair when the object lives
	// in loose.
	if _, statErr := s.dir.ObjectStat(h); statErr == nil {
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if idxErr != nil {
		return idxErr
	}

	_, err = findInAlternates(ctx, s, func(alt *ObjectStorage) (struct{}, error) {
		return struct{}{}, alt.HasEncodedObject(ctx, h)
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
	return packfile.NewPackfile(nil,
		packfile.WithPackHandle(func() (packfile.PackHandle, error) {
			return s.dir.PackHandle(pack)
		}),
		packfile.WithIdx(idx),
		packfile.WithFs(s.dir.Fs()),
		packfile.WithCache(s.objectCache),
		packfile.WithObjectIDSize(pack.Size()),
	), nil
}

// EncodedObjectSize returns the plaintext size of the given object,
// without actually reading the full object data from storage.
func (s *ObjectStorage) EncodedObjectSize(ctx context.Context, h plumbing.Hash) (size int64, err error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Pack-membership-first when the index is healthy: a single
	// in-memory fanout probe routes packed reads through the pack
	// reader and skips the loose Stat. If the index fails to load
	// (e.g. corrupt .idx), fall through to loose so the lookup
	// degrades gracefully — same shape as HasEncodedObject.
	idxErr := s.requireIndex()
	if idxErr == nil {
		if pack, idx, offset := s.findObjectInPackfile(h); pack != plumbing.ZeroHash {
			if cached, ok := s.objectCache.Get(h); ok {
				return cached.Size(), nil
			}
			p, perr := s.packfile(idx, pack)
			if perr != nil {
				return 0, perr
			}
			size, err = p.GetSizeByOffset(offset)
			if err == nil {
				return size, nil
			}
			if !errors.Is(err, plumbing.ErrObjectNotFound) {
				return 0, err
			}
			// Membership claimed the hash but the pack lost it —
			// fall through to loose and alternates.
		}
	}

	size, err = s.encodedObjectSizeFromUnpacked(h)
	if err == nil {
		return size, nil
	}
	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		return 0, err
	}
	if idxErr != nil {
		return 0, idxErr
	}

	return findInAlternates(ctx, s, func(alt *ObjectStorage) (int64, error) {
		return alt.EncodedObjectSize(ctx, h)
	})
}

// EncodedObject returns the object with the given hash, by searching for it in
// the packfile and the git object directories.
func (s *ObjectStorage) EncodedObject(ctx context.Context, t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var obj plumbing.EncodedObject
	var err error

	// Pack-membership-first when the index is healthy: see
	// EncodedObjectSize for the routing rationale. The shared
	// object cache is keyed by hash only — gating the cache
	// check on findObjectInPackfile keeps reads safe when callers
	// share a cache across ObjectStorages (see
	// TestGetFromObjectFileSharedCache). A failed requireIndex
	// (corrupt .idx) degrades to loose-only rather than failing
	// the whole read.
	idxErr := s.requireIndex()
	routed := false
	if idxErr == nil {
		if pack, idx, offset := s.findObjectInPackfile(h); pack != plumbing.ZeroHash {
			routed = true
			if cached, ok := s.objectCache.Get(h); ok {
				if t == plumbing.AnyObject || cached.Type() == t {
					return cached, nil
				}
				return nil, plumbing.ErrObjectNotFound
			}
			obj, err = s.getFromPackfileAt(pack, idx, h, offset, false)
		}
	}
	if !routed {
		obj, err = s.getFromUnpacked(h)
	}

	if errors.Is(err, plumbing.ErrObjectNotFound) {
		obj, err = findInAlternates(ctx, s, func(alt *ObjectStorage) (plumbing.EncodedObject, error) {
			return alt.EncodedObject(ctx, t, h)
		})
		if errors.Is(err, plumbing.ErrObjectNotFound) && idxErr != nil {
			return nil, idxErr
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
func (s *ObjectStorage) DeltaObject(ctx context.Context, t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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

// getFromPackfile resolves h via the packfile path: cheap pack-
// membership probe, then fetch from the located pack.
func (s *ObjectStorage) getFromPackfile(h plumbing.Hash, canBeDelta bool) (plumbing.EncodedObject, error) {
	if err := s.requireIndex(); err != nil {
		return nil, err
	}

	pack, idx, offset := s.findObjectInPackfile(h)
	if offset == -1 {
		return nil, plumbing.ErrObjectNotFound
	}
	return s.getFromPackfileAt(pack, idx, h, offset, canBeDelta)
}

// getFromPackfileAt fetches the object at a pre-located pack
// position, skipping the membership probe. Used by callers that
// already ran findObjectInPackfile (e.g. EncodedObject's pack-
// membership-first fast path) so the lookup is not repeated.
func (s *ObjectStorage) getFromPackfileAt(pack plumbing.Hash, idx idxfile.Index, h plumbing.Hash, offset int64, canBeDelta bool) (plumbing.EncodedObject, error) {
	p, err := s.packfile(idx, pack)
	if err != nil {
		return nil, err
	}
	defer ioutil.CheckClose(p, &err)

	if canBeDelta {
		return s.decodeDeltaObjectAt(p, offset, h)
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

// findObjectInPackfile locates h across the storage's packs and
// returns (pack-hash, idx, offset). offset == -1 means not found
// in any pack; the returned idx is then nil. Snapshots s.packs
// under RLock and releases the lock before calling MayContain /
// FindOffset, so slow LazyIndex I/O does not block concurrent
// Reindex / requireIndex publish on muI.Lock.
//
// MRU policy diverges from canonical Git's find_pack_entry
// (packfile.c), which moves the hit pack to the head of the
// packed_git linked list so every subsequent walk starts there.
// go-git stores a single-slot atomic hint instead. A true reorder
// would require write-locking s.packs on every successful find,
// defeating the snapshot-under-RLock pattern that the rest of the
// read path relies on. The hint can go stale under concurrent
// Reindex / PackfileWriter.Notify; staleness costs at most one
// extra MayContain + FindOffset probe and never misroutes, since
// FindOffset's contract returns an offset only for the hash it
// was asked about.
func (s *ObjectStorage) findObjectInPackfile(h plumbing.Hash) (plumbing.Hash, idxfile.Index, int64) {
	s.muI.RLock()
	packs := s.packs
	s.muI.RUnlock()

	if len(packs) == 0 {
		return plumbing.ZeroHash, nil, -1
	}

	// MRU: probe the last successfully-hit pack first. The hint is
	// encoded as packs index + 1; 0 means no hint. A stale entry
	// costs one MayContain + FindOffset but never misroutes.
	hint := int(s.lastHitPackIdx.Load()) - 1
	if hint >= 0 && hint < len(packs) {
		pe := packs[hint]
		if pe.idx != nil && pe.idx.MayContain(h) {
			if offset, err := pe.idx.FindOffset(h); err == nil {
				return pe.h, pe.idx, offset
			}
		}
	} else {
		hint = -1
	}

	for i, pe := range packs {
		if i == hint {
			// Skip the MRU pack — we already tried it above.
			continue
		}
		if !pe.idx.MayContain(h) {
			continue
		}
		offset, err := pe.idx.FindOffset(h)
		if err == nil {
			// Update the hint only when it actually changed.
			// Saves a no-op atomic Store on the hot stay-on-pack
			// pattern (caught by the MRU probe above) and avoids
			// any allocation — the encoded value is a small int.
			next := int32(i + 1)
			if s.lastHitPackIdx.Load() != next {
				s.lastHitPackIdx.Store(next)
			}
			return pe.h, pe.idx, offset
		}
	}

	return plumbing.ZeroHash, nil, -1
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
	// Snapshot the index map under muI.RLock so the iteration
	// below is safe against a concurrent Reindex swap or a
	// PackfileWriter.Notify insert. The borrowed LazyIndex values
	// stay alive for the duration of the loop via this slice; the
	// underlying SharedFile FDs are governed by their refcount and
	// the fdpool, not by removal from s.index.
	s.muI.RLock()
	indexes := make([]idxfile.Index, 0, len(s.index))
	for _, idx := range s.index {
		indexes = append(indexes, idx)
	}
	s.muI.RUnlock()

	for _, index := range indexes {
		ei, err := index.EntriesWithPrefix(prefix)
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
			if _, ok := seen[e.Hash]; ok {
				continue
			}
			seen[e.Hash] = struct{}{}
			hashes = append(hashes, e.Hash)
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
func (s *ObjectStorage) IterEncodedObjects(ctx context.Context, t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
			pack, err := s.dir.OpenPackForReading(h)
			if err != nil {
				return nil, err
			}
			s.muI.RLock()
			idx := s.index[h]
			s.muI.RUnlock()
			return newPackfileIter(
				s.dir.Fs(), pack, t, seen, idx,
				s.objectCache, false, h.Size(),
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

	// Close each cached Index. LazyIndex.Close releases idle file
	// descriptors and permanently disables the index; MemoryIndex's
	// Close is a no-op.
	s.muI.RLock()
	for _, idx := range s.index {
		if err := idx.Close(); firstError == nil && err != nil {
			firstError = err
		}
	}
	s.muI.RUnlock()

	_ = s.dir.Close()

	return firstError
}

// CloseIdleDescriptors releases the FDs held by this
// [ObjectStorage] and every cached alternate. The object cache,
// the packfile cache, the alternates cache, and the `s.index`
// map (with the [idxfile.LazyIndex] entries inside it) all
// survive — only the file descriptors backing those LazyIndex
// entries are released.
//
// The call fans out across three independent FD owners: the
// [dotgit.DotGit] `PackHandle` catalog (.pack and the
// `LazyIndex` inside each `PackHandle`), the ObjectStorage-level
// idx map (which holds its own LazyIndex per pack, distinct
// from the one inside the `PackHandle`), and any cached
// alternate `ObjectStorage`.
//
// Idempotent and safe to call concurrently with reads. In-flight
// reads complete normally; the FDs they hold refcounts on close
// the instant the last reader releases. After
// [ObjectStorage.Close] the call is a no-op.
//
// Parent storage is released before alternates — a deliberate
// divergence from Close, which goes alternates-first. The
// parent-first order favours OS reclaim of this storage's FDs
// when an alternate's release is slow (e.g. a network FS).
func (s *ObjectStorage) CloseIdleDescriptors() error {
	var errs []error

	if err := s.dir.CloseIdleDescriptors(); err != nil {
		errs = append(errs, err)
	}

	// ObjectStorage maintains a separate idxfile cache in s.index
	// (populated by loadIdxFile → loadLazyIndex). LazyIndex
	// entries own .idx/.rev FDs distinct from those inside the
	// dotgit PackHandle catalog; they need their own soft-close
	// fan-out. The storer.IdleReleaser assertion picks up
	// LazyIndex automatically and silently skips index
	// implementations that hold no FDs (notably MemoryIndex).
	s.muI.RLock()
	for _, idx := range s.index {
		if r, ok := idx.(storer.IdleReleaser); ok {
			if err := r.CloseIdleDescriptors(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	s.muI.RUnlock()

	s.muA.RLock()
	for _, alt := range s.alternates {
		if err := alt.CloseIdleDescriptors(); err != nil {
			errs = append(errs, err)
		}
	}
	s.muA.RUnlock()

	return errors.Join(errs...)
}

func hashListAsMap(l []plumbing.Hash) map[plumbing.Hash]struct{} {
	m := make(map[plumbing.Hash]struct{}, len(l))
	for _, h := range l {
		m[h] = struct{}{}
	}
	return m
}

// ForEachObjectHash iterates over every object hash in the storage.
func (s *ObjectStorage) ForEachObjectHash(ctx context.Context, fun func(plumbing.Hash) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	err := s.dir.ForEachObjectHash(fun)
	if err == storer.ErrStop {
		return nil
	}
	return err
}

// LooseObjectTime returns the modification time of a loose object.
func (s *ObjectStorage) LooseObjectTime(ctx context.Context, hash plumbing.Hash) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	fi, err := s.dir.ObjectStat(hash)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

// DeleteLooseObject removes a loose object from storage.
func (s *ObjectStorage) DeleteLooseObject(ctx context.Context, hash plumbing.Hash) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.dir.ObjectDelete(hash)
}

// ObjectPacks returns the list of packfile hashes.
func (s *ObjectStorage) ObjectPacks(ctx context.Context) ([]plumbing.Hash, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.dir.ObjectPacks()
}

// DeleteOldObjectPackAndIndex removes a pack and its index if older than t.
// Also drops the in-memory s.index map entry and the matching s.packs
// slice entry for the removed pack so subsequent routing decisions in
// findObjectInPackfile no longer claim membership for a hash that
// lives only in the now-deleted pack. If the MRU hint pointed at the
// deleted slot, invalidate it.
func (s *ObjectStorage) DeleteOldObjectPackAndIndex(ctx context.Context, h plumbing.Hash, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.dir.DeleteOldObjectPackAndIndex(h, t); err != nil {
		return err
	}
	s.muI.Lock()
	defer s.muI.Unlock()

	idx, ok := s.index[h]
	if !ok {
		return nil
	}
	delete(s.index, h)

	// Drop the matching s.packs entry. Allocate a fresh slice and
	// copy the rest (mirror of PackfileWriter.Notify's copy-on-grow)
	// so readers holding the old slice header keep a stable view.
	for i, pe := range s.packs {
		if pe.h != h {
			continue
		}
		next := make([]packEntry, 0, len(s.packs)-1)
		next = append(next, s.packs[:i]...)
		next = append(next, s.packs[i+1:]...)
		s.packs = next
		// Invalidate the MRU hint if it pointed at the deleted slot.
		// findObjectInPackfile's bounds check + FindOffset contract
		// make a stale hint safe, but resetting under the lock here
		// removes the staleness window entirely.
		if s.lastHitPackIdx.Load() == int32(i+1) {
			s.lastHitPackIdx.Store(0)
		}
		break
	}

	_ = idx.Close()
	return nil
}
