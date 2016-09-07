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
	scanner        *Scanner
	storage        core.ObjectStorage
	offsetToObject map[int64]core.Object
	hashToOffset   map[core.Hash]int64
}

// NewDecoder returns a new Decoder that reads from r.
func NewDecoder(p *Scanner, s core.ObjectStorage) *Decoder {
	return &Decoder{
		scanner:        p,
		storage:        s,
		offsetToObject: make(map[int64]core.Object, 0),
		hashToOffset:   make(map[core.Hash]int64, 0),
	}
}

// Decode reads a packfile and stores it in the value pointed to by s.
func (d *Decoder) Decode() (checksum core.Hash, err error) {
	if err := d.doDecode(); err != nil {
		return core.ZeroHash, err
	}

	return d.scanner.Checksum()
}

func (d *Decoder) doDecode() error {
	_, count, err := d.scanner.Header()
	if err != nil {
		return err
	}

	if d.storage == nil {
		return d.readObjects(count, nil)
	}

	tx := d.storage.Begin()
	if err := d.readObjects(count, tx); err != nil {
		if err := tx.Rollback(); err != nil {
			return nil
		}

		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (d *Decoder) readObjects(count uint32, tx core.TxObjectStorage) error {
	for i := 0; i < int(count); i++ {
		obj, err := d.readObject()
		if err != nil {
			return err
		}

		if tx == nil {
			continue
		}

		_, err = tx.Set(obj)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Decoder) readObject() (core.Object, error) {
	h, err := d.scanner.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	obj := d.newObject()
	obj.SetSize(h.Length)
	obj.SetType(h.Type)

	switch h.Type {
	case core.CommitObject, core.TreeObject, core.BlobObject, core.TagObject:
		err = d.fillRegularObjectContent(obj)
	case core.REFDeltaObject:
		err = d.fillREFDeltaObjectContent(obj, h.Reference)
	case core.OFSDeltaObject:
		err = d.fillOFSDeltaObjectContent(obj, h.OffsetReference)
	default:
		err = ErrInvalidObject.AddDetails("type %q", h.Type)
	}

	if err != nil {
		return obj, err
	}

	d.remember(h.Offset, obj)
	return obj, nil
}

func (d *Decoder) newObject() core.Object {
	if d.storage == nil {
		return &core.MemoryObject{}
	}

	return d.storage.NewObject()
}

func (d *Decoder) fillRegularObjectContent(obj core.Object) error {
	w, err := obj.Writer()
	if err != nil {
		return err
	}

	_, err = d.scanner.NextObject(w)
	return err
}

func (d *Decoder) fillREFDeltaObjectContent(obj core.Object, ref core.Hash) error {
	buf := bytes.NewBuffer(nil)
	if _, err := d.scanner.NextObject(buf); err != nil {
		return err
	}

	base, err := d.recallByHash(ref)
	if err != nil {
		return err
	}

	obj.SetType(base.Type())
	return ApplyDelta(obj, base, buf.Bytes())
}

func (d *Decoder) fillOFSDeltaObjectContent(obj core.Object, offset int64) error {
	buf := bytes.NewBuffer(nil)
	if _, err := d.scanner.NextObject(buf); err != nil {
		return err
	}

	base, err := d.recallByOffset(offset)
	if err != nil {
		return err
	}

	obj.SetType(base.Type())
	return ApplyDelta(obj, base, buf.Bytes())
}

// remember stores the offset of the object and its hash and the object itself.
// If a seeker was not provided to the decoder, the objects are stored in memory
func (d *Decoder) remember(o int64, obj core.Object) {
	h := obj.Hash()

	d.hashToOffset[h] = o
	if !d.scanner.IsSeekable() {
		d.offsetToObject[o] = obj
	}
}

// recallByHash returns the object for a given hash by looking for it again in
// the io.ReadeSeerker.
func (d *Decoder) recallByHash(h core.Hash) (core.Object, error) {
	o, ok := d.hashToOffset[h]
	if !ok {
		return nil, ErrCannotRecall.AddDetails("hash not found: %s", h)
	}

	return d.recallByOffset(o)
}

// recallByOffset returns the object for a given offset by looking for it again in
// the io.ReadeSeerker. For efficiency reasons, this method always find objects by
// offset, even if they have not been remembered or if they have been forgetted.
func (d *Decoder) recallByOffset(o int64) (core.Object, error) {
	obj, ok := d.offsetToObject[o]
	if ok {
		return obj, nil
	}

	if !ok && !d.scanner.IsSeekable() {
		return nil, ErrCannotRecall.AddDetails("no object found at offset %d", o)
	}

	return d.ReadObjectAt(o)
}

// ReadObjectAt reads an object at the given location
func (d *Decoder) ReadObjectAt(offset int64) (core.Object, error) {
	if !d.scanner.IsSeekable() {
		return nil, ErrNotSeeker
	}

	beforeJump, err := d.scanner.Seek(offset)
	if err != nil {
		return nil, err
	}

	defer func() {
		_, seekErr := d.scanner.Seek(beforeJump)
		if err == nil {
			err = seekErr
		}
	}()

	return d.readObject()
}

// Index returns an index of the objects read by hash and the position where
// was read
func (d *Decoder) Index() map[core.Hash]int64 {
	return d.hashToOffset
}

// Close close the Scanner, usually this mean that the whole reader is read and
// discarded
func (d *Decoder) Close() error {
	return d.scanner.Close()
}
