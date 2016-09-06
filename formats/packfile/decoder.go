package packfile

import (
	"bytes"
	"io"
	"os"

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
	// ErrDuplicatedObject is returned by Remember if an object appears several
	// times in a packfile.
	ErrDuplicatedObject = NewError("duplicated object")
	// ErrCannotRecall is returned by RecallByOffset or RecallByHash if the object
	// to recall cannot be returned.
	ErrCannotRecall = NewError("cannot recall object")
)

// Decoder reads and decodes packfiles from an input stream.
type Decoder struct {
	p              *Parser
	s              core.ObjectStorage
	seeker         io.Seeker
	offsetToObject map[int64]core.Object
	hashToOffset   map[core.Hash]int64
}

// NewDecoder returns a new Decoder that reads from r.
func NewDecoder(s core.ObjectStorage, p *Parser, seeker io.Seeker) *Decoder {
	return &Decoder{
		p:              p,
		s:              s,
		seeker:         seeker,
		offsetToObject: make(map[int64]core.Object, 0),
		hashToOffset:   make(map[core.Hash]int64, 0),
	}
}

// Decode reads a packfile and stores it in the value pointed to by s.
func (d *Decoder) Decode() error {
	_, count, err := d.p.Header()
	if err != nil {
		return err
	}

	tx := d.s.Begin()
	if err := d.readObjects(tx, count); err != nil {
		if err := tx.Rollback(); err != nil {
			return nil
		}

		return err
	}

	return tx.Commit()
}

func (d *Decoder) readObjects(tx core.TxObjectStorage, count uint32) error {
	// This code has 50-80 µs of overhead per object not counting zlib inflation.
	// Together with zlib inflation, it's 400-410 µs for small objects.
	// That's 1 sec for ~2450 objects, ~4.20 MB, or ~250 ms per MB,
	// of which 12-20 % is _not_ zlib inflation (ie. is our code).
	for i := 0; i < int(count); i++ {
		obj, err := d.readObject()
		if err != nil {
			return err
		}

		_, err = tx.Set(obj)
		if err == io.EOF {
			break
		}
	}

	return nil
}

func (d *Decoder) readObject() (core.Object, error) {
	h, err := d.p.NextObjectHeader()
	if err != nil {
		return nil, err
	}

	obj := d.s.NewObject()
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

	return obj, d.remember(h.Offset, obj)
}

func (d *Decoder) fillRegularObjectContent(obj core.Object) error {
	w, err := obj.Writer()
	if err != nil {
		return err
	}

	_, err = d.p.NextObject(w)
	return err
}

func (d *Decoder) fillREFDeltaObjectContent(obj core.Object, ref core.Hash) error {
	base, err := d.recallByHash(ref)
	if err != nil {
		return err
	}
	obj.SetType(base.Type())
	if err := d.readAndApplyDelta(obj, base); err != nil {
		return err
	}

	return nil
}

func (d *Decoder) fillOFSDeltaObjectContent(obj core.Object, offset int64) error {
	base, err := d.recallByOffset(offset)
	if err != nil {
		return err
	}

	obj.SetType(base.Type())
	if err := d.readAndApplyDelta(obj, base); err != nil {
		return err
	}

	return nil
}

// ReadAndApplyDelta reads and apply the base patched with the contents
// of a zlib compressed diff data in the delta portion of an object
// entry in the packfile.
func (d *Decoder) readAndApplyDelta(target, base core.Object) error {
	buf := bytes.NewBuffer(nil)
	if _, err := d.p.NextObject(buf); err != nil {
		return err
	}

	return ApplyDelta(target, base, buf.Bytes())
}

// Remember stores the offset of the object and its hash, but not the
// object itself.  This implementation does not check for already stored
// offsets, as it is too expensive to build this information from an
// index every time a get operation is performed on the SeekableReadRecaller.
func (r *Decoder) remember(o int64, obj core.Object) error {
	h := obj.Hash()
	r.hashToOffset[h] = o
	r.offsetToObject[o] = obj
	return nil
}

// RecallByHash returns the object for a given hash by looking for it again in
// the io.ReadeSeerker.
func (r *Decoder) recallByHash(h core.Hash) (core.Object, error) {
	o, ok := r.hashToOffset[h]
	if !ok {
		return nil, ErrCannotRecall.AddDetails("hash not found: %s", h)
	}

	return r.recallByOffset(o)
}

// RecallByOffset returns the object for a given offset by looking for it again in
// the io.ReadeSeerker. For efficiency reasons, this method always find objects by
// offset, even if they have not been remembered or if they have been forgetted.
func (r *Decoder) recallByOffset(o int64) (obj core.Object, err error) {
	obj, ok := r.offsetToObject[o]
	if ok {
		return obj, nil
	}

	if !ok && r.seeker == nil {
		return nil, ErrCannotRecall.AddDetails("no object found at offset %d", o)
	}

	// remember current offset
	beforeJump, err := r.seeker.Seek(0, os.SEEK_CUR)
	if err != nil {
		return nil, err
	}

	defer func() {
		// jump back
		_, seekErr := r.seeker.Seek(beforeJump, os.SEEK_SET)
		if err == nil {
			err = seekErr
		}
	}()

	// jump to requested offset
	_, err = r.seeker.Seek(o, os.SEEK_SET)
	if err != nil {
		return nil, err
	}

	return r.readObject()
}
