package packfile

import (
	"bytes"
	"io"

	billy "gopkg.in/src-d/go-billy.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
	"gopkg.in/src-d/go-git.v4/plumbing/format/idxfile"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// Packfile allows retrieving information from inside a packfile.
type Packfile struct {
	idxfile.Index
	billy.File
	s              *Scanner
	deltaBaseCache cache.Object
	offsetToHash   map[int64]plumbing.Hash
}

// NewPackfile returns a packfile representation for the given packfile file
// and packfile idx.
func NewPackfile(index idxfile.Index, file billy.File) *Packfile {
	s := NewScanner(file)

	return &Packfile{
		index,
		file,
		s,
		cache.NewObjectLRUDefault(),
		make(map[int64]plumbing.Hash),
	}
}

// Get retrieves the encoded object in the packfile with the given hash.
func (p *Packfile) Get(h plumbing.Hash) (plumbing.EncodedObject, error) {
	offset, err := p.FindOffset(h)
	if err != nil {
		return nil, err
	}

	return p.GetByOffset(offset)
}

// GetByOffset retrieves the encoded object from the packfile with the given
// offset.
func (p *Packfile) GetByOffset(o int64) (plumbing.EncodedObject, error) {
	if h, ok := p.offsetToHash[o]; ok {
		if obj, ok := p.deltaBaseCache.Get(h); ok {
			return obj, nil
		}
	}

	if _, err := p.s.SeekFromStart(o); err != nil {
		return nil, err
	}

	return p.nextObject()
}

func (p *Packfile) nextObject() (plumbing.EncodedObject, error) {
	h, err := p.s.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	obj := new(plumbing.MemoryObject)
	obj.SetSize(h.Length)
	obj.SetType(h.Type)

	switch h.Type {
	case plumbing.CommitObject, plumbing.TreeObject, plumbing.BlobObject, plumbing.TagObject:
		err = p.fillRegularObjectContent(obj)
	case plumbing.REFDeltaObject:
		err = p.fillREFDeltaObjectContent(obj, h.Reference)
	case plumbing.OFSDeltaObject:
		err = p.fillOFSDeltaObjectContent(obj, h.OffsetReference)
	default:
		err = ErrInvalidObject.AddDetails("type %q", h.Type)
	}

	if err != nil {
		return obj, err
	}

	p.offsetToHash[h.Offset] = obj.Hash()

	return obj, nil
}

func (p *Packfile) fillRegularObjectContent(obj plumbing.EncodedObject) error {
	w, err := obj.Writer()
	if err != nil {
		return err
	}

	_, _, err = p.s.NextObject(w)
	return err
}

func (p *Packfile) fillREFDeltaObjectContent(obj plumbing.EncodedObject, ref plumbing.Hash) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	_, _, err := p.s.NextObject(buf)
	if err != nil {
		return err
	}

	base, ok := p.cacheGet(ref)
	if !ok {
		base, err = p.Get(ref)
		if err != nil {
			return err
		}
	}

	obj.SetType(base.Type())
	err = ApplyDelta(obj, base, buf.Bytes())
	p.cachePut(obj)
	bufPool.Put(buf)

	return err
}

func (p *Packfile) fillOFSDeltaObjectContent(obj plumbing.EncodedObject, offset int64) error {
	buf := bytes.NewBuffer(nil)
	_, _, err := p.s.NextObject(buf)
	if err != nil {
		return err
	}

	var base plumbing.EncodedObject
	h, ok := p.offsetToHash[offset]
	if ok {
		base, ok = p.cacheGet(h)
	}

	if !ok {
		base, err = p.GetByOffset(offset)
		if err != nil {
			return err
		}

		p.cachePut(base)
	}

	obj.SetType(base.Type())
	err = ApplyDelta(obj, base, buf.Bytes())
	p.cachePut(obj)

	return err
}

func (p *Packfile) cacheGet(h plumbing.Hash) (plumbing.EncodedObject, bool) {
	if p.deltaBaseCache == nil {
		return nil, false
	}

	return p.deltaBaseCache.Get(h)
}

func (p *Packfile) cachePut(obj plumbing.EncodedObject) {
	if p.deltaBaseCache == nil {
		return
	}

	p.deltaBaseCache.Put(obj)
}

// GetAll returns an iterator with all encoded objects in the packfile.
// The iterator returned is not thread-safe, it should be used in the same
// thread as the Packfile instance.
func (p *Packfile) GetAll() (storer.EncodedObjectIter, error) {
	s := NewScanner(p.File)

	_, count, err := s.Header()
	if err != nil {
		return nil, err
	}

	return &objectIter{
		// Easiest way to provide an object decoder is just to pass a Packfile
		// instance. To not mess with the seeks, it's a new instance with a
		// different scanner but the same cache and offset to hash map for
		// reusing as much cache as possible.
		d:     &Packfile{p.Index, nil, s, p.deltaBaseCache, p.offsetToHash},
		count: int(count),
	}, nil
}

// ID returns the ID of the packfile, which is the checksum at the end of it.
func (p *Packfile) ID() (plumbing.Hash, error) {
	if _, err := p.File.Seek(-20, io.SeekEnd); err != nil {
		return plumbing.ZeroHash, err
	}

	var hash plumbing.Hash
	if _, err := io.ReadFull(p.File, hash[:]); err != nil {
		return plumbing.ZeroHash, err
	}

	return hash, nil
}

// Close the packfile and its resources.
func (p *Packfile) Close() error {
	return p.File.Close()
}

type objectDecoder interface {
	nextObject() (plumbing.EncodedObject, error)
}

type objectIter struct {
	d     objectDecoder
	count int
	pos   int
}

func (i *objectIter) Next() (plumbing.EncodedObject, error) {
	if i.pos >= i.count {
		return nil, io.EOF
	}

	i.pos++
	return i.d.nextObject()
}

func (i *objectIter) ForEach(f func(plumbing.EncodedObject) error) error {
	for {
		o, err := i.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := f(o); err != nil {
			return err
		}
	}
}

func (i *objectIter) Close() {
	i.pos = i.count
}
