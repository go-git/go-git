package packfile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
)

const MaxObjectsLimit = 1000000

type PackfileReader struct {
	r io.Reader

	objects map[string]packfileObject
	offsets map[int]string
	deltas  []packfileDelta

	// The give back logic is explained in the giveBack method.
	startedGivingBack bool
	givebackBuffer    []byte
	givenBack         io.Reader
	contentCallback   ContentCallback
}

// Sometimes, after reading an object from a packfile, there will be
// a few bytes with garbage data before the next object comes by.
// There is no way of reliably noticing this until when trying to read the
// next object and failing because zlib parses an invalid header. We can't
// notice before, because parsing the object's header (size, type, etc.)
// doesn't fail.
//
// At that point, we want to give back to the reader the bytes we've read
// since the last object, shift the input by one byte, and try again. That's
// why we save the bytes we read on each object and, if it fails in the middle
// of parsing it, those bytes will be read the next times you call Read() on
// a objectReader derived from a PackfileReader.readObject, until they run out.
func (pr *PackfileReader) giveBack() {
	pr.givenBack = bytes.NewReader(pr.givebackBuffer)
	pr.givebackBuffer = nil
}

type packfileObject struct {
	bytes []byte
	typ   int8
}

type packfileDelta struct {
	hash  string
	delta []byte
}

func NewPackfileReader(r io.Reader, contentCallback ContentCallback) (*PackfileReader, error) {
	return &PackfileReader{
		r:               r,
		objects:         map[string]packfileObject{},
		offsets:         map[int]string{},
		contentCallback: contentCallback,
	}, nil
}

func (p *PackfileReader) Read() (*Packfile, error) {
	packfile := NewPackfile()

	if err := p.validateSignature(); err != nil {
		if err == io.EOF {
			// This is an empty repo. It's OK.
			return packfile, nil
		}
		return nil, err
	}

	var err error
	ver, err := p.readInt32()
	if err != nil {
		return nil, err
	}

	count, err := p.readInt32()
	if err != nil {
		return nil, err
	}

	packfile.Version = uint32(ver)
	packfile.ObjectCount = int(count)

	if packfile.ObjectCount > MaxObjectsLimit {
		return nil, NewError("too many objects (%d)", packfile.ObjectCount)
	}

	if err := p.readObjects(packfile); err != nil {
		return nil, err
	}

	return packfile, nil
}

func (p *PackfileReader) validateSignature() error {
	var signature = make([]byte, 4)
	if _, err := p.r.Read(signature); err != nil {
		return err
	}

	if !bytes.Equal(signature, []byte{'P', 'A', 'C', 'K'}) {
		return NewError("Pack file does not start with 'PACK'")
	}

	return nil
}

func (p *PackfileReader) readInt32() (uint32, error) {
	var value uint32
	if err := binary.Read(p.r, binary.BigEndian, &value); err != nil {
		fmt.Println(err)

		return 0, err
	}

	return value, nil
}

func (p *PackfileReader) readObjects(packfile *Packfile) error {
	// This code has 50-80 µs of overhead per object not counting zlib inflation.
	// Together with zlib inflation, it's 400-410 µs for small objects.
	// That's 1 sec for ~2450 objects, ~4.20 MB, or ~250 ms per MB,
	// of which 12-20 % is _not_ zlib inflation (ie. is our code).

	p.startedGivingBack = true
	var unknownForBytes [4]byte

	offset := 12
	for i := 0; i < packfile.ObjectCount; i++ {
		r, err := p.readObject(packfile, offset)
		if err != nil && err != io.EOF {
			return err
		}

		p.offsets[offset] = r.hash
		offset += r.counter + 4

		p.r.Read(unknownForBytes[:])

		if err == io.EOF {
			break
		}
	}

	return nil
}

const (
	OBJ_COMMIT    = 1
	OBJ_TREE      = 2
	OBJ_BLOB      = 3
	OBJ_TAG       = 4
	OBJ_OFS_DELTA = 6
	OBJ_REF_DELTA = 7
)

const SIZE_LIMIT uint64 = 1 << 32 //4GB

type objectReader struct {
	pr     *PackfileReader
	pf     *Packfile
	offset int
	hash   string

	typ     int8
	size    uint64
	counter int
}

func (p *PackfileReader) readObject(packfile *Packfile, offset int) (*objectReader, error) {
	o, err := newObjectReader(p, packfile, offset)
	if err != nil {
		return nil, err
	}

	switch o.typ {
	case OBJ_REF_DELTA:
		err = o.readREFDelta()
	case OBJ_OFS_DELTA:
		err = o.readOFSDelta()
	case OBJ_COMMIT, OBJ_TREE, OBJ_BLOB, OBJ_TAG:
		err = o.readObject()
	default:
		err = NewError("Invalid git object tag %q", o.typ)
	}
	if err == ErrZlibHeader {
		p.giveBack()
		io.CopyN(ioutil.Discard, p.r, 1)
		return p.readObject(packfile, offset)
	}

	return o, err
}

func newObjectReader(pr *PackfileReader, pf *Packfile, offset int) (*objectReader, error) {
	o := &objectReader{pr: pr, pf: pf, offset: offset}
	var buf [1]byte
	if _, err := o.Read(buf[:]); err != nil {
		return nil, err
	}

	o.typ = int8((buf[0] >> 4) & 7)
	o.size = uint64(buf[0] & 15)

	var shift uint = 4
	for buf[0]&0x80 == 0x80 {
		if _, err := o.Read(buf[:]); err != nil {
			return nil, err
		}

		o.size += uint64(buf[0]&0x7f) << shift
		shift += 7
	}

	return o, nil
}

func (o *objectReader) readREFDelta() error {
	var ref [20]byte
	o.Read(ref[:])

	buf, err := o.inflate()
	if err != nil {
		return err
	}

	refhash := fmt.Sprintf("%x", ref)
	referenced, ok := o.pr.objects[refhash]
	if !ok {
		o.pr.deltas = append(o.pr.deltas, packfileDelta{hash: refhash, delta: buf[:]})
	} else {
		patched := PatchDelta(referenced.bytes, buf[:])
		if patched == nil {
			return NewError("error while patching %x", ref)
		}
		o.typ = referenced.typ
		err = o.addObject(patched)
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *objectReader) readOFSDelta() error {
	// read negative offset
	var b uint8
	binary.Read(o, binary.BigEndian, &b)
	var noffset int = int(b & 0x7f)
	for (b & 0x80) != 0 {
		noffset += 1
		binary.Read(o, binary.BigEndian, &b)
		noffset = (noffset << 7) + int(b&0x7f)
	}

	buf, err := o.inflate()
	if err != nil {
		return err
	}

	refhash := o.pr.offsets[o.offset-noffset]
	referenced, ok := o.pr.objects[refhash]
	if !ok {
		return NewError("can't find a pack entry at %d", o.offset-noffset)
	} else {
		patched := PatchDelta(referenced.bytes, buf)
		if patched == nil {
			return NewError("error while patching %x", refhash)
		}
		o.typ = referenced.typ
		err = o.addObject(patched)
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *objectReader) readObject() error {
	buf, err := o.inflate()
	if err != nil {
		return err
	}

	return o.addObject(buf)
}

func (o *objectReader) addObject(bytes []byte) error {
	var hash string

	switch o.typ {
	case OBJ_COMMIT:
		c, err := NewCommit(bytes)
		if err != nil {
			return err
		}
		o.pf.Commits[c.Hash()] = c
		hash = c.Hash()
	case OBJ_TREE:
		c, err := NewTree(bytes)
		if err != nil {
			return err
		}
		o.pf.Trees[c.Hash()] = c
		hash = c.Hash()
	case OBJ_BLOB:
		c, err := NewBlob(bytes)
		if err != nil {
			return err
		}
		o.pf.Blobs[c.Hash()] = c
		hash = c.Hash()

		if o.pr.contentCallback != nil {
			o.pr.contentCallback(hash, bytes)
		}
	}

	o.pr.objects[hash] = packfileObject{bytes: bytes, typ: o.typ}
	o.hash = hash

	return nil

}

func (o *objectReader) inflate() ([]byte, error) {
	//Quick fix "Invalid git object tag '\x00'" when the length of a object is 0
	if o.size == 0 {
		var buf [4]byte
		if _, err := o.Read(buf[:]); err != nil {
			return nil, err
		}

		return nil, nil
	}

	zr, err := zlib.NewReader(o)
	if err != nil {
		if err.Error() == "zlib: invalid header" {
			return nil, ErrZlibHeader
		} else {
			return nil, NewError("error opening packfile's object zlib: %v", err)
		}
	}

	defer zr.Close()

	if o.size > SIZE_LIMIT {
		return nil, NewError("the object size exceeed the allowed limit: %d", o.size)
	}

	var arrbuf [4096]byte // Stack-allocated for <4 KB objects.
	var buf []byte
	if uint64(len(arrbuf)) >= o.size {
		buf = arrbuf[:o.size]
	} else {
		buf = make([]byte, o.size)
	}

	read := 0
	for read < int(o.size) {
		n, err := zr.Read(buf[read:])
		if err != nil {
			return nil, err
		}

		read += n
	}

	if read != int(o.size) {
		return nil, NewError("inflated size mismatch, expected %d, got %d", o.size, read)
	}

	return buf, nil
}

func (o *objectReader) Read(p []byte) (int, error) {
	i := 0
	if o.pr.givenBack != nil {
		i1, err := o.pr.givenBack.Read(p)
		if err == nil {
			i += i1
		} else {
			o.pr.givenBack = nil
		}
	}

	i2, err := o.pr.r.Read(p[i:])
	i += i2
	o.counter += i
	if err == nil && o.pr.startedGivingBack {
		o.pr.givebackBuffer = append(o.pr.givebackBuffer, p[:i]...)
	}
	return i, err
}

func (o *objectReader) ReadByte() (byte, error) {
	var c byte
	if err := binary.Read(o, binary.BigEndian, &c); err != nil {
		return 0, err
	}

	return c, nil
}

type ReaderError struct {
	Msg string // description of error
}

func NewError(format string, args ...interface{}) error {
	return &ReaderError{Msg: fmt.Sprintf(format, args...)}
}

func (e *ReaderError) Error() string { return e.Msg }

var ErrZlibHeader = errors.New("zlib: invalid header")
