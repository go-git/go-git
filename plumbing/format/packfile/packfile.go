package packfile

import (
	"bufio"
	"container/list"
	"crypto"
	"fmt"
	"io"
	"sync"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	gogitsync "github.com/go-git/go-git/v6/utils/sync"
)

var (
	// ErrInvalidObject is returned by Decode when an invalid object is
	// found in the packfile.
	ErrInvalidObject = NewError("invalid git object")
	// ErrZLib is returned by Decode when there was an error unzipping
	// the packfile contents.
	ErrZLib = NewError("zlib reading error")
)

// deltaBaseCacheMemLimit and deltaBaseCacheObjLimit mirror the limits used by
// libgit2 (pack.h: GIT_PACK_CACHE_MEMORY_LIMIT / GIT_PACK_CACHE_SIZE_LIMIT).
const (
	deltaBaseCacheMemLimit = 16 * 1024 * 1024 // 16 MiB total
	deltaBaseCacheObjLimit = 1024 * 1024       // skip objects larger than 1 MiB
)

// deltaBaseCache is an LRU cache keyed by pack offset, used for delta base
// resolution. Using offsets as keys avoids calling obj.Hash() (which triggers
// a SHA recomputation) just to populate the cache. The cache is bounded by
// total byte size, not entry count, matching libgit2's design.
type deltaBaseCache struct {
	memUsed int64
	ll      *list.List
	items   map[int64]*list.Element
}

type deltaCacheEntry struct {
	offset int64
	size   int64
	obj    plumbing.EncodedObject
}

func newDeltaBaseCache() *deltaBaseCache {
	return &deltaBaseCache{
		ll:    list.New(),
		items: make(map[int64]*list.Element),
	}
}

func (c *deltaBaseCache) get(offset int64) (plumbing.EncodedObject, bool) {
	if e, ok := c.items[offset]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*deltaCacheEntry).obj, true
	}
	return nil, false
}

func (c *deltaBaseCache) put(offset int64, obj plumbing.EncodedObject) {
	if _, ok := c.items[offset]; ok {
		return
	}
	sz := obj.Size()
	if sz > deltaBaseCacheObjLimit {
		return
	}
	e := c.ll.PushFront(&deltaCacheEntry{offset: offset, size: sz, obj: obj})
	c.items[offset] = e
	c.memUsed += sz
	for c.memUsed > deltaBaseCacheMemLimit && c.ll.Len() > 0 {
		back := c.ll.Back()
		entry := back.Value.(*deltaCacheEntry)
		c.memUsed -= entry.size
		delete(c.items, entry.offset)
		c.ll.Remove(back)
	}
}

// Packfile allows retrieving information from inside a packfile.
type Packfile struct {
	idxfile.Index
	fs      billy.Filesystem
	file    billy.File
	scanner *Scanner

	cache      cache.Object
	deltaCache *deltaBaseCache
	rbuf       *bufio.Reader

	id           plumbing.Hash
	m            sync.Mutex
	objectIDSize int

	once    sync.Once
	onceErr error
}

// NewPackfile returns a packfile representation for the given packfile file
// and packfile idx.
// If the filesystem is provided, the packfile will return FSObjects, otherwise
// it will return MemoryObjects.
func NewPackfile(
	file billy.File,
	opts ...PackfileOption,
) *Packfile {
	p := &Packfile{
		file:         file,
		objectIDSize: crypto.SHA1.Size(),
	}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Get retrieves the encoded object in the packfile with the given hash.
func (p *Packfile) Get(h plumbing.Hash) (plumbing.EncodedObject, error) {
	if err := p.init(); err != nil {
		return nil, err
	}
	p.m.Lock()
	defer p.m.Unlock()

	return p.get(h)
}

// GetByOffset retrieves the encoded object from the packfile at the given
// offset.
func (p *Packfile) GetByOffset(offset int64) (plumbing.EncodedObject, error) {
	if err := p.init(); err != nil {
		return nil, err
	}
	p.m.Lock()
	defer p.m.Unlock()

	return p.getByOffset(offset)
}

// GetSizeByOffset retrieves the size of the encoded object from the
// packfile with the given offset.
func (p *Packfile) GetSizeByOffset(offset int64) (size int64, err error) {
	if err := p.init(); err != nil {
		return 0, err
	}

	d, err := p.GetByOffset(offset)
	if err != nil {
		return 0, err
	}

	return d.Size(), nil
}

// GetAll returns an iterator with all encoded objects in the packfile.
// The iterator returned is not thread-safe, it should be used in the same
// thread as the Packfile instance.
func (p *Packfile) GetAll() (storer.EncodedObjectIter, error) {
	return p.GetByType(plumbing.AnyObject)
}

// GetByType returns all the objects of the given type.
func (p *Packfile) GetByType(typ plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	if err := p.init(); err != nil {
		return nil, err
	}

	switch typ {
	case plumbing.AnyObject,
		plumbing.BlobObject,
		plumbing.TreeObject,
		plumbing.CommitObject,
		plumbing.TagObject:
		entries, err := p.EntriesByOffset()
		if err != nil {
			return nil, err
		}

		return &objectIter{
			p:    p,
			iter: entries,
			typ:  typ,
		}, nil
	default:
		return nil, plumbing.ErrInvalidType
	}
}

// Scanner returns the Packfile's inner scanner.
//
// Deprecated: this will be removed in future versions of the packfile package
// to avoid exposing the package internals and to improve its thread-safety.
// TODO: Remove Scanner method
func (p *Packfile) Scanner() (*Scanner, error) {
	if err := p.init(); err != nil {
		return nil, err
	}

	return p.scanner, nil
}

// ID returns the ID of the packfile, which is the checksum at the end of it.
func (p *Packfile) ID() (plumbing.Hash, error) {
	if err := p.init(); err != nil {
		return plumbing.ZeroHash, err
	}

	return p.id, nil
}

// get is not threat-safe, and should only be called within packfile.go.
func (p *Packfile) get(h plumbing.Hash) (plumbing.EncodedObject, error) {
	if obj, ok := p.cache.Get(h); ok {
		return obj, nil
	}

	offset, err := p.FindOffset(h)
	if err != nil {
		return nil, err
	}

	oh, err := p.headerFromOffset(offset)
	if err != nil {
		return nil, err
	}

	obj, err := p.objectFromHeader(oh)
	if err != nil {
		return nil, err
	}

	// The hash is already known here; pre-set it so ObjectLRU.Put does not
	// trigger a SHA recomputation on delta-reconstructed MemoryObjects.
	if mo, ok := obj.(*plumbing.MemoryObject); ok {
		mo.SetHash(h)
	}
	p.cache.Put(obj)

	return obj, nil
}

// getByOffset is not threat-safe, and should only be called within packfile.go.
func (p *Packfile) getByOffset(offset int64) (plumbing.EncodedObject, error) {
	h, err := p.FindHash(offset)
	if err != nil {
		return nil, err
	}

	if obj, ok := p.cache.Get(h); ok {
		return obj, nil
	}

	oh, err := p.headerFromOffset(offset)
	if err != nil {
		return nil, err
	}

	obj, err := p.objectFromHeader(oh)
	if err != nil {
		return nil, err
	}

	// The hash is already known here; pre-set it so ObjectLRU.Put does not
	// trigger a SHA recomputation on delta-reconstructed MemoryObjects.
	if mo, ok := obj.(*plumbing.MemoryObject); ok {
		mo.SetHash(h)
	}
	p.cache.Put(obj)

	return obj, nil
}

func (p *Packfile) init() error {
	p.once.Do(func() {
		if p.file == nil {
			p.onceErr = fmt.Errorf("file is not set")
			return
		}

		if p.Index == nil {
			p.onceErr = fmt.Errorf("index is not set")
			return
		}

		p.rbuf = gogitsync.GetBufioReader(nil)

		opts := []ScannerOption{WithBufioReader(p.rbuf), WithoutPackChecksum(), WithoutObjectChecksum()}

		if p.objectIDSize == format.SHA256Size {
			opts = append(opts, WithSHA256())
		}

		p.scanner = NewScanner(p.file, opts...)
		// Validate packfile signature.
		if !p.scanner.Scan() {
			p.onceErr = p.scanner.Error()
			return
		}

		_, err := p.scanner.Seek(-int64(p.objectIDSize), io.SeekEnd)
		if err != nil {
			p.onceErr = err
			return
		}

		p.id.ResetBySize(p.objectIDSize)
		_, err = p.id.ReadFrom(p.scanner)
		if err != nil {
			p.onceErr = err
		}

		if p.cache == nil {
			p.cache = cache.NewObjectLRUDefault()
		}

		p.deltaCache = newDeltaBaseCache()
	})

	return p.onceErr
}

func (p *Packfile) headerFromOffset(offset int64) (*ObjectHeader, error) {
	err := p.scanner.SeekFromStart(offset)
	if err != nil {
		return nil, err
	}

	if !p.scanner.Scan() {
		return nil, plumbing.ErrObjectNotFound
	}

	oh := p.scanner.Data().Value().(ObjectHeader)
	return &oh, nil
}

// Close the packfile and its resources.
func (p *Packfile) Close() error {
	p.m.Lock()
	defer p.m.Unlock()

	gogitsync.PutBufioReader(p.rbuf)

	closer, ok := p.file.(io.Closer)
	if !ok {
		return nil
	}

	return closer.Close()
}

func (p *Packfile) objectFromHeader(oh *ObjectHeader) (plumbing.EncodedObject, error) {
	if oh == nil {
		return nil, plumbing.ErrObjectNotFound
	}

	// If we have filesystem, and the object is not a delta type, return a FSObject.
	// This avoids having to inflate the object more than once.
	if !oh.Type.IsDelta() && p.fs != nil {
		fs := NewFSObject(
			oh.ID(),
			oh.Type,
			oh.ContentOffset,
			oh.Size,
			p.Index,
			p.fs,
			p.file,
			p.file.Name(),
			p.cache,
		)

		p.cache.Put(fs)
		return fs, nil
	}

	return p.getMemoryObject(oh)
}

func (p *Packfile) getMemoryObject(oh *ObjectHeader) (plumbing.EncodedObject, error) {
	of := format.SHA1
	if p.objectIDSize == format.SHA256.Size() {
		of = format.SHA256
	}
	h := plumbing.FromObjectFormat(of)
	obj := plumbing.NewMemoryObject(h)

	obj.SetSize(oh.Size)
	obj.SetType(oh.Type)

	// Pre-populate from the scanner-computed hash for non-delta objects.
	// For delta objects the hash is unknown here; callers (get/getByOffset)
	// set it after reconstruction using the index-provided hash.
	if !oh.Hash.IsZero() {
		obj.SetHash(oh.Hash)
	}

	w, err := obj.Writer()
	if err != nil {
		return nil, err
	}
	defer ioutil.CheckClose(w, &err)

	switch oh.Type {
	case plumbing.CommitObject, plumbing.TreeObject, plumbing.BlobObject, plumbing.TagObject:
		err = p.scanner.inflateContent(oh.ContentOffset, w)

	case plumbing.REFDeltaObject, plumbing.OFSDeltaObject:
		var parent plumbing.EncodedObject

		switch oh.Type {
		case plumbing.REFDeltaObject:
			// REFDelta bases are looked up by hash (already known from the pack
			// stream), so the hash-keyed cache is appropriate here.
			var ok bool
			parent, ok = p.cache.Get(oh.Reference)
			if !ok {
				parent, err = p.get(oh.Reference)
			}
		case plumbing.OFSDeltaObject:
			// OFSDelta bases are looked up by offset. Use the offset-keyed
			// deltaCache to avoid triggering a SHA recomputation on the base.
			var ok bool
			parent, ok = p.deltaCache.get(oh.OffsetReference)
			if !ok {
				parent, err = p.getByOffset(oh.OffsetReference)
			}
		}

		if err != nil {
			return nil, fmt.Errorf("cannot find base object: %w", err)
		}

		if oh.content == nil {
			oh.content = gogitsync.GetBytesBuffer()
		}

		err = p.scanner.inflateContent(oh.ContentOffset, oh.content)
		if err != nil {
			return nil, fmt.Errorf("cannot inflate content: %w", err)
		}

		obj.SetType(parent.Type())
		err = ApplyDelta(obj, parent, oh.content)

	default:
		err = ErrInvalidObject.AddDetails("type %q", oh.Type)
	}

	if err != nil {
		return nil, err
	}

	// Store in the offset-keyed delta cache so that subsequent OFSDelta
	// objects can use this as a base without triggering a SHA recomputation.
	p.deltaCache.put(oh.Offset, obj)

	return obj, nil
}
