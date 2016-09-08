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
	// ErrNotSeeker not seeker supported
	ErrNotSeeker = NewError("no seeker capable decode")
	// ErrCannotRecall is returned by RecallByOffset or RecallByHash if the object
	// to recall cannot be returned.
	ErrCannotRecall = NewError("cannot recall object")
)

// Decoder reads and decodes packfiles from an input stream.
type Decoder struct {
	s  *Scanner
	o  core.ObjectStorage
	tx core.TxObjectStorage

	offsets map[int64]core.Hash
	crcs    map[core.Hash]uint32
}

// NewDecoder returns a new Decoder that reads from r.
func NewDecoder(s *Scanner, o core.ObjectStorage) *Decoder {
	return &Decoder{
		s:  s,
		o:  o,
		tx: o.Begin(),

		offsets: make(map[int64]core.Hash, 0),
		crcs:    make(map[core.Hash]uint32, 0),
	}
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

	if err := d.readObjects(count); err != nil {
		if err := d.tx.Rollback(); err != nil {
			return nil
		}

		return err
	}

	if err := d.tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (d *Decoder) readObjects(count uint32) error {
	for i := 0; i < int(count); i++ {
		obj, err := d.readObject()
		if err != nil {
			return err
		}

		if _, err := d.tx.Set(obj); err != nil {
			return err
		}
	}

	return nil
}

func (d *Decoder) readObject() (core.Object, error) {
	h, err := d.s.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	obj := d.o.NewObject()
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

	d.remember(obj, h.Offset, crc)
	return obj, nil
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

func (d *Decoder) remember(obj core.Object, offset int64, crc uint32) {
	h := obj.Hash()

	d.offsets[offset] = h
	d.crcs[h] = crc
}

func (d *Decoder) recallByOffset(o int64) (core.Object, error) {
	h, ok := d.offsets[o]
	if ok {
		return d.recallByHash(h)
	}

	return nil, ErrCannotRecall.AddDetails("no object found at offset %d", o)
}

func (d *Decoder) recallByHash(h core.Hash) (core.Object, error) {
	return d.tx.Get(core.AnyObject, h)
}

// ReadObjectAt reads an object at the given location
func (d *Decoder) ReadObjectAt(offset int64) (core.Object, error) {
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

	return d.readObject()
}

// Offsets returns the objects read offset
func (d *Decoder) Offsets() map[core.Hash]int64 {
	i := make(map[core.Hash]int64, len(d.offsets))
	for o, h := range d.offsets {
		i[h] = o
	}

	return i
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
