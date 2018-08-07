package packfile

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
)

var (
	// ErrObjectContentAlreadyRead is returned when the content of the object
	// was already read, since the content can only be read once.
	ErrObjectContentAlreadyRead = errors.New("object content was already read")

	// ErrReferenceDeltaNotFound is returned when the reference delta is not
	// found.
	ErrReferenceDeltaNotFound = errors.New("reference delta not found")
)

// Observer interface is implemented by index encoders.
type Observer interface {
	// OnHeader is called when a new packfile is opened.
	OnHeader(count uint32) error
	// OnInflatedObjectHeader is called for each object header read.
	OnInflatedObjectHeader(t plumbing.ObjectType, objSize int64, pos int64) error
	// OnInflatedObjectContent is called for each decoded object.
	OnInflatedObjectContent(h plumbing.Hash, pos int64, crc uint32, content []byte) error
	// OnFooter is called when decoding is done.
	OnFooter(h plumbing.Hash) error
}

// Parser decodes a packfile and calls any observer associated to it. Is used
// to generate indexes.
type Parser struct {
	scanner    *Scanner
	count      uint32
	oi         []*objectInfo
	oiByHash   map[plumbing.Hash]*objectInfo
	oiByOffset map[int64]*objectInfo
	hashOffset map[plumbing.Hash]int64
	checksum   plumbing.Hash

	cache        *cache.ObjectLRU
	contentCache map[int64][]byte

	ob []Observer
}

// NewParser creates a new Parser struct.
func NewParser(scanner *Scanner, ob ...Observer) *Parser {
	var contentCache map[int64][]byte
	if !scanner.IsSeekable {
		contentCache = make(map[int64][]byte)
	}

	return &Parser{
		scanner:      scanner,
		ob:           ob,
		count:        0,
		cache:        cache.NewObjectLRUDefault(),
		contentCache: contentCache,
	}
}

// Parse start decoding phase of the packfile.
func (p *Parser) Parse() (plumbing.Hash, error) {
	if err := p.init(); err != nil {
		return plumbing.ZeroHash, err
	}

	if err := p.firstPass(); err != nil {
		return plumbing.ZeroHash, err
	}

	if err := p.resolveDeltas(); err != nil {
		return plumbing.ZeroHash, err
	}

	for _, o := range p.ob {
		if err := o.OnFooter(p.checksum); err != nil {
			return plumbing.ZeroHash, err
		}
	}

	return p.checksum, nil
}

func (p *Parser) init() error {
	_, c, err := p.scanner.Header()
	if err != nil {
		return err
	}

	for _, o := range p.ob {
		if err := o.OnHeader(c); err != nil {
			return err
		}
	}

	p.count = c
	p.oiByHash = make(map[plumbing.Hash]*objectInfo, p.count)
	p.oiByOffset = make(map[int64]*objectInfo, p.count)
	p.oi = make([]*objectInfo, p.count)

	return nil
}

func (p *Parser) firstPass() error {
	buf := new(bytes.Buffer)

	for i := uint32(0); i < p.count; i++ {
		buf.Reset()

		oh, err := p.scanner.NextObjectHeader()
		if err != nil {
			return err
		}

		delta := false
		var ota *objectInfo
		switch t := oh.Type; t {
		case plumbing.OFSDeltaObject, plumbing.REFDeltaObject:
			delta = true

			var parent *objectInfo
			var ok bool

			if t == plumbing.OFSDeltaObject {
				parent, ok = p.oiByOffset[oh.OffsetReference]
			} else {
				parent, ok = p.oiByHash[oh.Reference]
			}

			if !ok {
				return ErrReferenceDeltaNotFound
			}

			ota = newDeltaObject(oh.Offset, oh.Length, t, parent)

			parent.Children = append(parent.Children, ota)
		default:
			ota = newBaseObject(oh.Offset, oh.Length, t)
		}

		size, crc, err := p.scanner.NextObject(buf)
		if err != nil {
			return err
		}

		ota.Crc32 = crc
		ota.PackSize = size
		ota.Length = oh.Length

		if !delta {
			if _, err := ota.Write(buf.Bytes()); err != nil {
				return err
			}
			ota.SHA1 = ota.Sum()
			p.oiByHash[ota.SHA1] = ota
		}

		p.oiByOffset[oh.Offset] = ota

		p.oi[i] = ota
	}

	var err error
	p.checksum, err = p.scanner.Checksum()
	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

func (p *Parser) resolveDeltas() error {
	for _, obj := range p.oi {
		content, err := obj.Content()
		if err != nil {
			return err
		}

		for _, o := range p.ob {
			err := o.OnInflatedObjectHeader(obj.Type, obj.Length, obj.Offset)
			if err != nil {
				return err
			}

			err = o.OnInflatedObjectContent(obj.SHA1, obj.Offset, obj.Crc32, content)
			if err != nil {
				return err
			}
		}

		if !obj.IsDelta() && len(obj.Children) > 0 {
			var err error
			base, err := p.get(obj)
			if err != nil {
				return err
			}

			for _, child := range obj.Children {
				if _, err := p.resolveObject(child, base); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (p *Parser) get(o *objectInfo) ([]byte, error) {
	e, ok := p.cache.Get(o.SHA1)
	if ok {
		r, err := e.Reader()
		if err != nil {
			return nil, err
		}

		buf := make([]byte, e.Size())
		if _, err = r.Read(buf); err != nil {
			return nil, err
		}

		return buf, nil
	}

	// Read from disk
	if o.DiskType.IsDelta() {
		base, err := p.get(o.Parent)
		if err != nil {
			return nil, err
		}

		data, err := p.resolveObject(o, base)
		if err != nil {
			return nil, err
		}

		if len(o.Children) > 0 {
			m := &plumbing.MemoryObject{}
			m.Write(data)
			m.SetType(o.Type)
			m.SetSize(o.Size())
			p.cache.Put(m)
		}

		return data, nil
	}

	data, err := p.readData(o)
	if err != nil {
		return nil, err
	}

	if len(o.Children) > 0 {
		m := &plumbing.MemoryObject{}
		m.Write(data)
		m.SetType(o.Type)
		m.SetSize(o.Size())
		p.cache.Put(m)
	}

	return data, nil
}

func (p *Parser) resolveObject(
	o *objectInfo,
	base []byte,
) ([]byte, error) {
	if !o.DiskType.IsDelta() {
		return nil, nil
	}

	data, err := p.readData(o)
	if err != nil {
		return nil, err
	}

	data, err = applyPatchBase(o, data, base)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (p *Parser) readData(o *objectInfo) ([]byte, error) {
	buf := new(bytes.Buffer)

	// TODO: skip header. Header size can be calculated with the offset of the
	// next offset in the first pass.
	if _, err := p.scanner.SeekFromStart(o.Offset); err != nil {
		return nil, err
	}

	if _, err := p.scanner.NextObjectHeader(); err != nil {
		return nil, err
	}

	buf.Reset()

	if _, _, err := p.scanner.NextObject(buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func applyPatchBase(ota *objectInfo, data, base []byte) ([]byte, error) {
	patched, err := PatchDelta(base, data)
	if err != nil {
		return nil, err
	}

	ota.Type = ota.Parent.Type
	ota.Hasher = plumbing.NewHasher(ota.Type, int64(len(patched)))
	if _, err := ota.Write(patched); err != nil {
		return nil, err
	}
	ota.SHA1 = ota.Sum()

	return patched, nil
}

type objectInfo struct {
	plumbing.Hasher

	Offset       int64
	Length       int64
	HeaderLength int64
	PackSize     int64
	Type         plumbing.ObjectType
	DiskType     plumbing.ObjectType

	Crc32 uint32

	Parent   *objectInfo
	Children []*objectInfo
	SHA1     plumbing.Hash

	content *bytes.Buffer
}

func newBaseObject(offset, length int64, t plumbing.ObjectType) *objectInfo {
	return newDeltaObject(offset, length, t, nil)
}

func newDeltaObject(
	offset, length int64,
	t plumbing.ObjectType,
	parent *objectInfo,
) *objectInfo {
	children := make([]*objectInfo, 0)

	obj := &objectInfo{
		Hasher:   plumbing.NewHasher(t, length),
		Offset:   offset,
		Length:   length,
		PackSize: 0,
		Type:     t,
		DiskType: t,
		Crc32:    0,
		Parent:   parent,
		Children: children,
	}

	return obj
}

func (o *objectInfo) Write(bs []byte) (int, error) {
	n, err := o.Hasher.Write(bs)
	if err != nil {
		return 0, err
	}

	o.content = bytes.NewBuffer(nil)

	_, _ = o.content.Write(bs)
	return n, nil
}

// Content returns the content of the object. This operation can only be done
// once.
func (o *objectInfo) Content() ([]byte, error) {
	if o.content == nil {
		return nil, ErrObjectContentAlreadyRead
	}

	r := o.content
	o.content = nil
	return r.Bytes(), nil
}

func (o *objectInfo) IsDelta() bool {
	return o.Type.IsDelta()
}

func (o *objectInfo) Size() int64 {
	return o.Length
}
