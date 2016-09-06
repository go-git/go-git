package packfile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/core"
)

var (
	// ErrEmptyPackfile is returned by ReadHeader when no data is found in the packfile
	ErrEmptyPackfile = NewError("empty packfile")
	// ErrBadSignature is returned by ReadHeader when the signature in the packfile is incorrect.
	ErrBadSignature = NewError("malformed pack file signature")
	// ErrUnsupportedVersion is returned by ReadHeader when the packfile version is
	// different than VersionSupported.
	ErrUnsupportedVersion = NewError("unsupported packfile version")
)

const (
	// VersionSupported is the packfile version supported by this parser.
	VersionSupported uint32 = 2
)

type ObjectHeader struct {
	Type            core.ObjectType
	Offset          int64
	Length          int64
	Reference       core.Hash
	OffsetReference int64
}

// A Parser is a collection of functions to read and process data form a packfile.
// Values from this type are not zero-value safe. See the NewParser function bellow.
type Parser struct {
	r *trackableReader

	// pendingObject is used to detect if an object has been read, or still
	// is waiting to be read
	pendingObject *ObjectHeader
}

// NewParser returns a new Parser that reads from the packfile represented by r.
func NewParser(r io.Reader) *Parser {
	return &Parser{r: &trackableReader{Reader: r}}
}

// Header reads the whole packfile header (signature, version and object count).
// It returns the version and the object count and performs checks on the
// validity of the signature and the version fields.
func (p *Parser) Header() (version, objects uint32, err error) {
	sig, err := p.readSignature()
	if err != nil {
		if err == io.EOF {
			err = ErrEmptyPackfile
		}

		return
	}

	if !p.isValidSignature(sig) {
		err = ErrBadSignature
		return
	}

	version, err = p.readVersion()
	if err != nil {
		return
	}

	if !p.isSupportedVersion(version) {
		err = ErrUnsupportedVersion.AddDetails("%d", version)
		return
	}

	objects, err = p.readCount()
	return
}

// readSignature reads an returns the signature field in the packfile.
func (p *Parser) readSignature() ([]byte, error) {
	var sig = make([]byte, 4)
	if _, err := io.ReadFull(p.r, sig); err != nil {
		return []byte{}, err
	}

	return sig, nil
}

// isValidSignature returns if sig is a valid packfile signature.
func (p *Parser) isValidSignature(sig []byte) bool {
	return bytes.Equal(sig, []byte{'P', 'A', 'C', 'K'})
}

// readVersion reads and returns the version field of a packfile.
func (p *Parser) readVersion() (uint32, error) {
	return p.readInt32()
}

// isSupportedVersion returns whether version v is supported by the parser.
// The current supported version is VersionSupported, defined above.
func (p *Parser) isSupportedVersion(v uint32) bool {
	return v == VersionSupported
}

// readCount reads and returns the count of objects field of a packfile.
func (p *Parser) readCount() (uint32, error) {
	return p.readInt32()
}

// ReadInt32 reads 4 bytes and returns them as a Big Endian int32.
func (p *Parser) readInt32() (uint32, error) {
	var v uint32
	if err := binary.Read(p.r, binary.BigEndian, &v); err != nil {
		return 0, err
	}

	return v, nil
}

func (p *Parser) NextObjectHeader() (*ObjectHeader, error) {
	if err := p.discardObjectIfNeeded(); err != nil {
		return nil, err
	}

	h := &ObjectHeader{}
	p.pendingObject = h

	var err error
	h.Offset, err = p.r.Offset()
	if err != nil {
		return nil, err
	}

	h.Type, h.Length, err = p.readObjectTypeAndLength()
	if err != nil {
		return nil, err
	}

	switch h.Type {
	case core.OFSDeltaObject:
		no, err := p.readNegativeOffset()
		if err != nil {
			return nil, err
		}

		h.OffsetReference = h.Offset + no
	case core.REFDeltaObject:
		var err error
		h.Reference, err = p.readHash()
		if err != nil {
			return nil, err
		}
	}

	return h, nil
}

func (s *Parser) discardObjectIfNeeded() error {
	if s.pendingObject == nil {
		return nil
	}

	h := s.pendingObject
	n, err := s.NextObject(ioutil.Discard)
	if err != nil {
		return err
	}

	if n != h.Length {
		return fmt.Errorf(
			"error discarding object, discarded %d, expected %d",
			n, h.Length,
		)
	}

	return nil
}

// ReadObjectTypeAndLength reads and returns the object type and the
// length field from an object entry in a packfile.
func (p Parser) readObjectTypeAndLength() (core.ObjectType, int64, error) {
	t, c, err := p.readType()
	if err != nil {
		return t, 0, err
	}

	l, err := p.readLength(c)

	return t, l, err
}

func (p Parser) readType() (core.ObjectType, byte, error) {
	var c byte
	var err error
	if c, err = p.r.ReadByte(); err != nil {
		return core.ObjectType(0), 0, err
	}

	typ := parseType(c)

	return typ, c, nil
}

// the length is codified in the last 4 bits of the first byte and in
// the last 7 bits of subsequent bytes.  Last byte has a 0 MSB.
func (p *Parser) readLength(first byte) (int64, error) {
	length := int64(first & maskFirstLength)

	c := first
	shift := firstLengthBits
	var err error
	for moreBytesInLength(c) {
		if c, err = p.r.ReadByte(); err != nil {
			return 0, err
		}

		length += int64(c&maskLength) << shift
		shift += lengthBits
	}

	return length, nil
}

func (p *Parser) NextObject(w io.Writer) (written int64, err error) {
	p.pendingObject = nil
	return p.copyObject(w)
}

// ReadRegularObject reads and write a non-deltified object
// from it zlib stream in an object entry in the packfile.
func (p *Parser) copyObject(w io.Writer) (int64, error) {
	zr, err := zlib.NewReader(p.r)
	if err != nil {
		if err != zlib.ErrHeader {
			return -1, fmt.Errorf("zlib reading error: %s", err)
		}
	}

	defer func() {
		closeErr := zr.Close()
		if err == nil {
			err = closeErr
		}
	}()

	return io.Copy(w, zr)
}

func (p *Parser) Checksum() (core.Hash, error) {
	return p.readHash()
}

// ReadHash reads a hash.
func (p *Parser) readHash() (core.Hash, error) {
	var h core.Hash
	if _, err := io.ReadFull(p.r, h[:]); err != nil {
		return core.ZeroHash, err
	}

	return h, nil
}

// ReadNegativeOffset reads and returns an offset from a OFS DELTA
// object entry in a packfile. OFS DELTA offsets are specified in Git
// VLQ special format:
//
// Ordinary VLQ has some redundancies, example:  the number 358 can be
// encoded as the 2-octet VLQ 0x8166 or the 3-octet VLQ 0x808166 or the
// 4-octet VLQ 0x80808166 and so forth.
//
// To avoid these redundancies, the VLQ format used in Git removes this
// prepending redundancy and extends the representable range of shorter
// VLQs by adding an offset to VLQs of 2 or more octets in such a way
// that the lowest possible value for such an (N+1)-octet VLQ becomes
// exactly one more than the maximum possible value for an N-octet VLQ.
// In particular, since a 1-octet VLQ can store a maximum value of 127,
// the minimum 2-octet VLQ (0x8000) is assigned the value 128 instead of
// 0. Conversely, the maximum value of such a 2-octet VLQ (0xff7f) is
// 16511 instead of just 16383. Similarly, the minimum 3-octet VLQ
// (0x808000) has a value of 16512 instead of zero, which means
// that the maximum 3-octet VLQ (0xffff7f) is 2113663 instead of
// just 2097151.  And so forth.
//
// This is how the offset is saved in C:
//
//     dheader[pos] = ofs & 127;
//     while (ofs >>= 7)
//         dheader[--pos] = 128 | (--ofs & 127);
//
func (p *Parser) readNegativeOffset() (int64, error) {
	var c byte
	var err error

	if c, err = p.r.ReadByte(); err != nil {
		return 0, err
	}

	var offset = int64(c & maskLength)
	for moreBytesInLength(c) {
		offset++
		if c, err = p.r.ReadByte(); err != nil {
			return 0, err
		}
		offset = (offset << lengthBits) + int64(c&maskLength)
	}

	return -offset, nil
}

func moreBytesInLength(c byte) bool {
	return c&maskContinue > 0
}

var (
	maskContinue    = uint8(128) // 1000 0000
	maskType        = uint8(112) // 0111 0000
	maskFirstLength = uint8(15)  // 0000 1111
	firstLengthBits = uint8(4)   // the first byte has 4 bits to store the length
	maskLength      = uint8(127) // 0111 1111
	lengthBits      = uint8(7)   // subsequent bytes has 7 bits to store the length
)

func parseType(b byte) core.ObjectType {
	return core.ObjectType((b & maskType) >> firstLengthBits)
}

type trackableReader struct {
	io.Reader
	count int64
}

// Read reads up to len(p) bytes into p.
func (r *trackableReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.count += int64(n)

	return
}

// ReadByte reads a byte.
func (r *trackableReader) ReadByte() (byte, error) {
	var p [1]byte
	_, err := r.Reader.Read(p[:])
	r.count++

	return p[0], err
}

// Offset returns the number of bytes read.
func (r *trackableReader) Offset() (int64, error) {
	return r.count, nil
}
