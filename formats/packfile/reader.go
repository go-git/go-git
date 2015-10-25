package packfile

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/klauspost/compress/zlib"
)

const (
	DefaultMaxObjectsLimit = 1 << 20
	DefaultMaxObjectSize   = 1 << 32 // 4GB

)

type PackfileReader struct {
	// MaxObjectsLimit is the limit of objects to be load in the packfile, if
	// a packfile excess this number an error is throw, the default value
	// is defined by DefaultMaxObjectsLimit, usually the default limit is more
	// than enough to work with any repository, working extremly big repositories
	// where the number of object is bigger the memory can be exhausted.
	MaxObjectsLimit int

	// MaxObjectSize is the maximum size in bytes, reading objects with a bigger
	// size cause a error. The default value is defined by DefaultMaxObjectSize
	MaxObjectSize int

	r               *trackingReader
	objects         map[Hash]*RAWObject
	offsets         map[int]Hash
	deltas          []packfileDelta
	contentCallback ContentCallback
}

type packfileObject struct {
	bytes []byte
	typ   ObjectType
}

type packfileDelta struct {
	hash  Hash
	delta []byte
}

func NewPackfileReader(r io.Reader, fn ContentCallback) (*PackfileReader, error) {
	return &PackfileReader{
		MaxObjectsLimit: DefaultMaxObjectsLimit,
		MaxObjectSize:   DefaultMaxObjectSize,
		r:               &trackingReader{r: r},
		objects:         make(map[Hash]*RAWObject, 0),
		offsets:         make(map[int]Hash, 0),
		contentCallback: fn,
	}, nil
}

func (pr *PackfileReader) Read() (chan *RAWObject, error) {
	packfile := NewPackfile()

	if err := pr.validateHeader(); err != nil {
		if err == io.EOF {
			// This is an empty repo. It's OK.
			return nil, nil
		}

		return nil, err
	}

	ver, err := pr.readInt32()
	if err != nil {
		return nil, err
	}

	count, err := pr.readInt32()
	if err != nil {
		return nil, err
	}

	packfile.Version = uint32(ver)
	packfile.ObjectCount = int(count)

	if packfile.ObjectCount > pr.MaxObjectsLimit {
		return nil, NewError("too many objects %d, limit is %d",
			packfile.ObjectCount, pr.MaxObjectsLimit)
	}

	ch := make(chan *RAWObject, 1)

	go pr.readObjects(ch, count)

	packfile.Size = int64(pr.r.Pos())

	return ch, nil
}

func (pr *PackfileReader) validateHeader() error {
	var header = make([]byte, 4)
	if _, err := pr.r.Read(header); err != nil {
		return err
	}

	if !bytes.Equal(header, []byte{'P', 'A', 'C', 'K'}) {
		return NewError("Pack file does not start with 'PACK'")
	}

	return nil
}

func (pr *PackfileReader) readInt32() (uint32, error) {
	var value uint32
	if err := binary.Read(pr.r, binary.BigEndian, &value); err != nil {
		return 0, err
	}

	return value, nil
}

func (pr *PackfileReader) readObjects(ch chan *RAWObject, count uint32) error {
	// This code has 50-80 µs of overhead per object not counting zlib inflation.
	// Together with zlib inflation, it's 400-410 µs for small objects.
	// That's 1 sec for ~2450 objects, ~4.20 MB, or ~250 ms per MB,
	// of which 12-20 % is _not_ zlib inflation (ie. is our code).
	defer func() {
		close(ch)
	}()

	for i := 0; i < int(count); i++ {
		var pos = pr.Pos()
		obj, err := pr.readObject()
		if err != nil && err != io.EOF {
			fmt.Println(err)
			return err
		}

		pr.offsets[pos] = obj.Hash
		pr.objects[obj.Hash] = obj
		ch <- obj

		if err == io.EOF {
			break
		}
	}

	return nil
}

func (pr *PackfileReader) readObject() (*RAWObject, error) {

	o, err := newObjectReader(pr, pr.MaxObjectSize)
	if err != nil {
		return nil, err
	}

	raw := &RAWObject{Type: o.typ}

	switch o.typ {
	case REFDeltaObject:
		err = o.readREFDelta(raw)
	case OFSDeltaObject:
		err = o.readOFSDelta(raw)
	case CommitObject, TreeObject, BlobObject, TagObject:
		err = o.readObject(raw)
	default:
		err = NewError("Invalid git object tag %q", o.typ)
	}

	if err != nil {
		return nil, err
	}

	return raw, err
}

func (pr *PackfileReader) Pos() int { return pr.r.Pos() }

type objectReader struct {
	pr      *PackfileReader
	pf      *Packfile
	maxSize uint64

	hash  Hash
	steps int
	typ   ObjectType
	size  uint64
}

func newObjectReader(pr *PackfileReader, maxSize int) (*objectReader, error) {
	o := &objectReader{pr: pr, maxSize: uint64(maxSize)}

	var buf [1]byte
	if _, err := o.Read(buf[:]); err != nil {
		return nil, err
	}

	o.typ = ObjectType((buf[0] >> 4) & 7)
	o.size = uint64(buf[0] & 15)
	o.steps++ // byte we just read to get `o.typ` and `o.size`

	var shift uint = 4
	for buf[0]&0x80 == 0x80 {
		if _, err := o.Read(buf[:]); err != nil {
			return nil, err
		}

		o.size += uint64(buf[0]&0x7f) << shift
		o.steps++ // byte we just read to update `o.size`
		shift += 7
	}

	return o, nil
}

func (o *objectReader) readREFDelta(raw *RAWObject) error {
	var ref Hash
	if _, err := o.Read(ref[:]); err != nil {
		return err
	}

	buf, err := o.inflate()
	if err != nil {
		return err
	}

	referenced, ok := o.pr.objects[ref]
	if !ok {
		o.pr.deltas = append(o.pr.deltas, packfileDelta{hash: ref, delta: buf[:]})
	} else {
		patched := PatchDelta(referenced.Bytes, buf[:])
		if patched == nil {
			return NewError("error while patching %x", ref)
		}

		raw.Type = referenced.Type
		raw.Bytes = patched
		raw.Hash = ComputeHash(raw.Type, raw.Bytes)
	}

	return nil
}

func decodeOffset(src io.ByteReader, steps int) (int, error) {
	b, err := src.ReadByte()
	if err != nil {
		return 0, err
	}
	var offset = int(b & 0x7f)
	for (b & 0x80) != 0 {
		offset++ // WHY?
		b, err = src.ReadByte()
		if err != nil {
			return 0, err
		}

		offset = (offset << 7) + int(b&0x7f)
	}

	// offset needs to be aware of the bytes we read for `o.typ` and `o.size`
	offset += steps
	return -offset, nil
}

func (o *objectReader) readOFSDelta(raw *RAWObject) error {
	var pos = o.pr.Pos()

	// read negative offset
	offset, err := decodeOffset(o.pr.r, o.steps)
	if err != nil {
		return err
	}

	buf, err := o.inflate()
	if err != nil {
		return err
	}

	ref := o.pr.offsets[pos+offset]
	referenced, ok := o.pr.objects[ref]
	if !ok {
		return NewError("can't find a pack entry at %d", pos+offset)
	}

	patched := PatchDelta(referenced.Bytes, buf)
	if patched == nil {
		return NewError("error while patching %q", ref)
	}

	raw.Type = referenced.Type
	raw.Bytes = patched
	raw.Hash = ComputeHash(raw.Type, raw.Bytes)

	return nil
}

func (o *objectReader) readObject(raw *RAWObject) error {
	buf, err := o.inflate()
	if err != nil {
		return err
	}

	raw.Bytes = buf
	raw.Hash = ComputeHash(raw.Type, raw.Bytes)

	return nil
}

func (o *objectReader) inflate() ([]byte, error) {
	zr, err := zlib.NewReader(o.pr.r)
	if err != nil {
		if err == zlib.ErrHeader {
			return nil, zlib.ErrHeader
		}

		return nil, NewError("error opening packfile's object zlib: %v", err)
	}

	defer zr.Close()

	if o.size > o.maxSize {
		return nil, NewError("the object size %q exceeed the allowed limit: %q",
			o.size, o.maxSize)
	}

	var buf bytes.Buffer
	io.Copy(&buf, zr) // also: io.CopyN(&buf, zr, int64(o.size))

	var bufLen = buf.Len()
	if bufLen != int(o.size) {
		return nil, NewError("inflated size mismatch, expected %d, got %d", o.size, bufLen)
	}

	return buf.Bytes(), nil
}

func (o *objectReader) Read(p []byte) (int, error) {
	return o.pr.r.Read(p)
}

func (o *objectReader) ReadByte() (byte, error) {
	return o.pr.r.ReadByte()
}

type ReaderError struct {
	Msg string // description of error
}

func NewError(format string, args ...interface{}) error {
	return &ReaderError{Msg: fmt.Sprintf(format, args...)}
}

func (e *ReaderError) Error() string { return e.Msg }
