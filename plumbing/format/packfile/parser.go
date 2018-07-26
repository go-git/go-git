package packfile

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/cache"
)

// Observer interface is implemented by index encoders.
type Observer interface {
	// OnHeader is called when a new packfile is opened.
	OnHeader(count uint32) error
	// OnInflatedObjectHeader is called for each object header read.
	OnInflatedObjectHeader(t plumbing.ObjectType, objSize int64, pos int64) error
	// OnInflatedObjectContent is called for each decoded object.
	OnInflatedObjectContent(h plumbing.Hash, pos int64, crc uint32) error
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

	cache *cache.ObjectLRU

	ob []Observer
}

// NewParser creates a new Parser struct.
func NewParser(scanner *Scanner, ob ...Observer) *Parser {
	return &Parser{
		scanner: scanner,
		ob:      ob,
		count:   0,
		cache:   cache.NewObjectLRUDefault(),
	}
}

// Parse start decoding phase of the packfile.
func (p *Parser) Parse() (plumbing.Hash, error) {
	err := p.init()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	err = p.firstPass()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	err = p.resolveDeltas()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	for _, o := range p.ob {
		err := o.OnFooter(p.checksum)
		if err != nil {
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
		err := o.OnHeader(c)
		if err != nil {
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
		buf.Truncate(0)

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
				// TODO improve error
				return errors.New("Reference delta not found")
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
			ota.Write(buf.Bytes())
			ota.SHA1 = ota.Sum()
		}

		p.oiByOffset[oh.Offset] = ota
		p.oiByHash[oh.Reference] = ota

		p.oi[i] = ota
	}

	checksum, err := p.scanner.Checksum()
	p.checksum = checksum

	if err == io.EOF {
		return nil
	}

	return err
}

func (p *Parser) resolveDeltas() error {
	for _, obj := range p.oi {
		for _, o := range p.ob {
			err := o.OnInflatedObjectHeader(obj.Type, obj.Length, obj.Offset)
			if err != nil {
				return err
			}

			err = o.OnInflatedObjectContent(obj.SHA1, obj.Offset, obj.Crc32)
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
				_, err = p.resolveObject(child, base)
				if err != nil {
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
		_, err = r.Read(buf)
		if err != nil {
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
	base []byte) ([]byte, error) {

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
	p.scanner.SeekFromStart(o.Offset)
	_, err := p.scanner.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	buf.Truncate(0)

	_, _, err = p.scanner.NextObject(buf)
	if err != nil {
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
	hash := plumbing.ComputeHash(ota.Type, patched)

	ota.SHA1 = hash

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

func (o *objectInfo) IsDelta() bool {
	return o.Type.IsDelta()
}

func (o *objectInfo) Size() int64 {
	return o.Length
}
