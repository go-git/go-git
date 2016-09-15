package packfile

import (
	"bytes"

	"gopkg.in/src-d/go-git.v4/core"
)

// Format specifies if the packfile uses ref-deltas or ofs-deltas.
type Format int

// Possible values of the Format type.
const (
	UnknownFormat Format = iota
	OFSDeltaFormat
	REFDeltaFormat
)

var (
	// ErrMaxObjectsLimitReached is returned by Decode when the number
	// of objects in the packfile is higher than
	// Decoder.MaxObjectsLimit.
	ErrMaxObjectsLimitReached = NewError("max. objects limit reached")
	// ErrInvalidObject is returned by Decode when an invalid object is
	// found in the packfile.
	ErrInvalidObject = NewError("invalid git object")
	// ErrPackEntryNotFound is returned by Decode when a reference in
	// the packfile references and unknown object.
	ErrPackEntryNotFound = NewError("can't find a pack entry")
	// ErrZLib is returned by Decode when there was an error unzipping
	// the packfile contents.
	ErrZLib = NewError("zlib reading error")
	// ErrCannotRecall is returned by RecallByOffset or RecallByHash if the object
	// to recall cannot be returned.
	ErrCannotRecall = NewError("cannot recall object")
	// ErrNonSeekable is returned if a NewDecoder is used with a non-seekable
	// reader and without a core.ObjectStorage or ReadObjectAt method is called
	// without a seekable scanner
	ErrNonSeekable = NewError("non-seekable scanner")
)

// Decoder reads and decodes packfiles from an input stream.
type Decoder struct {
	s  *Scanner
	o  core.ObjectStorage
	tx core.TxObjectStorage

	offsetToHash map[int64]core.Hash
	hashToOffset map[core.Hash]int64
	crcs         map[core.Hash]uint32
}

// NewDecoder returns a new Decoder that reads from r.
func NewDecoder(s *Scanner, o core.ObjectStorage) (*Decoder, error) {
	if !s.IsSeekable && o == nil {
		return nil, ErrNonSeekable
	}

	var tx core.TxObjectStorage
	if o != nil {
		tx = o.Begin()
	}

	return &Decoder{
		s:  s,
		o:  o,
		tx: tx,

		offsetToHash: make(map[int64]core.Hash, 0),
		hashToOffset: make(map[core.Hash]int64, 0),
		crcs:         make(map[core.Hash]uint32, 0),
	}, nil
}

// Decode reads a packfile and stores it in the value pointed to by s.
func (d *Decoder) Decode() (checksum core.Hash, err error) {
	if err := d.doDecode(); err != nil {
		return core.ZeroHash, err
	}

	return d.s.Checksum()
}

func (d *Decoder) doDecode() error {
	_, count, err := d.s.Header()
	if err != nil {
		return err
	}

	if d.o == nil {
		return d.readObjects(count)
	}

	if err := d.readObjects(count); err != nil {
		if err := d.tx.Rollback(); err != nil {
			return nil
		}

		return err
	}

	return d.tx.Commit()
}

func (d *Decoder) readObjects(count uint32) error {
	for i := 0; i < int(count); i++ {
		obj, err := d.ReadObject()
		if err != nil {
			return err
		}

		if d.o == nil {
			continue
		}

		if _, err := d.tx.Set(obj); err != nil {
			return err
		}

	}

	return nil
}

// ReadObject reads a object from the stream and return it
func (d *Decoder) ReadObject() (core.Object, error) {
	h, err := d.s.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	obj := d.newObject()
	obj.SetSize(h.Length)
	obj.SetType(h.Type)
	var crc uint32
	switch h.Type {
	case core.CommitObject, core.TreeObject, core.BlobObject, core.TagObject:
		crc, err = d.fillRegularObjectContent(obj)
	case core.REFDeltaObject:
		crc, err = d.fillREFDeltaObjectContent(obj, h.Reference)
	case core.OFSDeltaObject:
		crc, err = d.fillOFSDeltaObjectContent(obj, h.OffsetReference)
	default:
		err = ErrInvalidObject.AddDetails("type %q", h.Type)
	}

	if err != nil {
		return obj, err
	}

	hash := obj.Hash()
	d.setOffset(hash, h.Offset)
	d.setCRC(hash, crc)

	return obj, nil
}

func (d *Decoder) newObject() core.Object {
	if d.o == nil {
		return &core.MemoryObject{}
	}

	return d.o.NewObject()
}

// ReadObjectAt reads an object at the given location
func (d *Decoder) ReadObjectAt(offset int64) (core.Object, error) {
	if !d.s.IsSeekable {
		return nil, ErrNonSeekable
	}

	beforeJump, err := d.s.Seek(offset)
	if err != nil {
		return nil, err
	}

	defer func() {
		_, seekErr := d.s.Seek(beforeJump)
		if err == nil {
			err = seekErr
		}
	}()

	return d.ReadObject()
}

func (d *Decoder) fillRegularObjectContent(obj core.Object) (uint32, error) {
	w, err := obj.Writer()
	if err != nil {
		return 0, err
	}

	_, crc, err := d.s.NextObject(w)
	return crc, err
}

func (d *Decoder) fillREFDeltaObjectContent(obj core.Object, ref core.Hash) (uint32, error) {
	buf := bytes.NewBuffer(nil)
	_, crc, err := d.s.NextObject(buf)
	if err != nil {
		return 0, err
	}

	base, err := d.recallByHash(ref)
	if err != nil {
		return 0, err
	}

	obj.SetType(base.Type())
	return crc, ApplyDelta(obj, base, buf.Bytes())
}

func (d *Decoder) fillOFSDeltaObjectContent(obj core.Object, offset int64) (uint32, error) {
	buf := bytes.NewBuffer(nil)
	_, crc, err := d.s.NextObject(buf)
	if err != nil {
		return 0, err
	}

	base, err := d.recallByOffset(offset)
	if err != nil {
		return 0, err
	}

	obj.SetType(base.Type())
	return crc, ApplyDelta(obj, base, buf.Bytes())
}

func (d *Decoder) setOffset(h core.Hash, offset int64) {
	d.offsetToHash[offset] = h
	d.hashToOffset[h] = offset
}

func (d *Decoder) setCRC(h core.Hash, crc uint32) {
	d.crcs[h] = crc
}

func (d *Decoder) recallByOffset(o int64) (core.Object, error) {
	if d.s.IsSeekable {
		return d.ReadObjectAt(o)
	}

	if h, ok := d.offsetToHash[o]; ok {
		return d.tx.Get(core.AnyObject, h)
	}

	return nil, core.ErrObjectNotFound
}

func (d *Decoder) recallByHash(h core.Hash) (core.Object, error) {
	if d.s.IsSeekable {
		if o, ok := d.hashToOffset[h]; ok {
			return d.ReadObjectAt(o)
		}
	}

	obj, err := d.tx.Get(core.AnyObject, h)
	if err != core.ErrObjectNotFound {
		return obj, err
	}

	return nil, core.ErrObjectNotFound
}

// SetOffsets sets the offsets, required when using the method ReadObjectAt,
// without decoding the full packfile
func (d *Decoder) SetOffsets(offsets map[core.Hash]int64) {
	d.hashToOffset = offsets
}

// Offsets returns the objects read offset
func (d *Decoder) Offsets() map[core.Hash]int64 {
	return d.hashToOffset
}

// CRCs returns the CRC-32 for each objected read
func (d *Decoder) CRCs() map[core.Hash]uint32 {
	return d.crcs
}

// Close close the Scanner, usually this mean that the whole reader is read and
// discarded
func (d *Decoder) Close() error {
	return d.s.Close()
}
