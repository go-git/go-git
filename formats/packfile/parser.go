package packfile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

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
	VersionSupported = 2
)

// A Parser is a collection of functions to read and process data form a packfile.
// Values from this type are not zero-value safe. See the NewParser function bellow.
type Parser struct {
	ReadRecaller
	ObjectFactory func() core.Object
}

// NewParser returns a new Parser that reads from the packfile represented by r.
func NewParser(r ReadRecaller) *Parser {
	return &Parser{ReadRecaller: r}
}

// ReadInt32 reads 4 bytes and returns them as a Big Endian int32.
func (p Parser) readInt32() (uint32, error) {
	var v uint32
	if err := binary.Read(p, binary.BigEndian, &v); err != nil {
		return 0, err
	}

	return v, nil
}

// ReadSignature reads an returns the signature field in the packfile.
func (p *Parser) ReadSignature() ([]byte, error) {
	var sig = make([]byte, 4)
	if _, err := io.ReadFull(p, sig); err != nil {
		return []byte{}, err
	}

	return sig, nil
}

// IsValidSignature returns if sig is a valid packfile signature.
func (p Parser) IsValidSignature(sig []byte) bool {
	return bytes.Equal(sig, []byte{'P', 'A', 'C', 'K'})
}

// ReadVersion reads and returns the version field of a packfile.
func (p *Parser) ReadVersion() (uint32, error) {
	return p.readInt32()
}

// IsSupportedVersion returns whether version v is supported by the parser.
// The current supported version is VersionSupported, defined above.
func (p *Parser) IsSupportedVersion(v uint32) bool {
	return v == VersionSupported
}

// ReadCount reads and returns the count of objects field of a packfile.
func (p *Parser) ReadCount() (uint32, error) {
	return p.readInt32()
}

// ReadHeader reads the whole packfile header (signature, version and
// object count). It returns the object count and performs checks on the
// validity of the signature and the version fields.
func (p Parser) ReadHeader() (uint32, error) {
	sig, err := p.ReadSignature()
	if err != nil {
		if err == io.EOF {
			return 0, ErrEmptyPackfile
		}
		return 0, err
	}

	if !p.IsValidSignature(sig) {
		return 0, ErrBadSignature
	}

	ver, err := p.ReadVersion()
	if err != nil {
		return 0, err
	}

	if !p.IsSupportedVersion(ver) {
		return 0, ErrUnsupportedVersion.AddDetails("%d", ver)
	}

	count, err := p.ReadCount()
	if err != nil {
		return 0, err
	}

	return count, nil
}

// ReadObjectTypeAndLength reads and returns the object type and the
// length field from an object entry in a packfile.
func (p Parser) ReadObjectTypeAndLength() (core.ObjectType, int64, error) {
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
	if c, err = p.ReadByte(); err != nil {
		return core.ObjectType(0), 0, err
	}
	typ := parseType(c)

	return typ, c, nil
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

// the length is codified in the last 4 bits of the first byte and in
// the last 7 bits of subsequent bytes.  Last byte has a 0 MSB.
func (p Parser) readLength(first byte) (int64, error) {
	length := int64(first & maskFirstLength)

	c := first
	shift := firstLengthBits
	var err error
	for moreBytesInLength(c) {
		if c, err = p.ReadByte(); err != nil {
			return 0, err
		}

		length += int64(c&maskLength) << shift
		shift += lengthBits
	}

	return length, nil
}

func moreBytesInLength(c byte) bool {
	return c&maskContinue > 0
}

// FillObject fills the given object from an object entry in the packfile.
// Non-deltified and deltified objects are supported.
func (p Parser) FillObject(obj core.Object) error {
	start, err := p.Offset()
	if err != nil {
		return err
	}

	t, l, err := p.ReadObjectTypeAndLength()
	if err != nil {
		return err
	}

	obj.SetSize(l)

	switch t {
	case core.CommitObject, core.TreeObject, core.BlobObject, core.TagObject:
		obj.SetType(t)
		err = p.FillFromNonDeltaContent(obj)
	case core.REFDeltaObject:
		err = p.FillREFDeltaObjectContent(obj)
	case core.OFSDeltaObject:
		err = p.FillOFSDeltaObjectContent(obj, start)
	default:
		err = ErrInvalidObject.AddDetails("tag %q", t)
	}

	return err
}

// FillFromNonDeltaContent reads and fill a non-deltified object
// from it zlib stream in an object entry in the packfile.
func (p Parser) FillFromNonDeltaContent(obj core.Object) error {
	w, err := obj.Writer()
	if err != nil {
		return err
	}

	return p.inflate(w)
}

func (p Parser) inflate(w io.Writer) (err error) {
	zr, err := zlib.NewReader(p)
	if err != nil {
		if err != zlib.ErrHeader {
			return fmt.Errorf("zlib reading error: %s", err)
		}
	}

	defer func() {
		closeErr := zr.Close()
		if err == nil {
			err = closeErr
		}
	}()

	_, err = io.Copy(w, zr)

	return err
}

// FillREFDeltaObjectContent reads and returns an object specified by a
// REF-Delta entry in the packfile, form the hash onwards.
func (p Parser) FillREFDeltaObjectContent(obj core.Object) error {
	refHash, err := p.ReadHash()
	if err != nil {
		return err
	}

	base, err := p.RecallByHash(refHash)
	if err != nil {
		return err
	}

	obj.SetType(base.Type())
	if err := p.ReadAndApplyDelta(obj, base); err != nil {
		return err
	}

	return nil
}

// ReadHash reads a hash.
func (p Parser) ReadHash() (core.Hash, error) {
	var h core.Hash
	if _, err := io.ReadFull(p, h[:]); err != nil {
		return core.ZeroHash, err
	}

	return h, nil
}

// ReadAndApplyDelta reads and apply the base patched with the contents
// of a zlib compressed diff data in the delta portion of an object
// entry in the packfile.
func (p Parser) ReadAndApplyDelta(target, base core.Object) error {
	buf := bytes.NewBuffer(nil)
	if err := p.inflate(buf); err != nil {
		return err
	}

	return ApplyDelta(target, base, buf.Bytes())
}

// FillOFSDeltaObjectContent reads an fill an object specified by an
// OFS-delta entry in the packfile from it negative offset onwards.  The
// start parameter is the offset of this particular object entry (the
// current offset minus the already processed type and length).
func (p Parser) FillOFSDeltaObjectContent(obj core.Object, start int64) error {

	jump, err := p.ReadNegativeOffset()
	if err != nil {
		return err
	}

	base, err := p.RecallByOffset(start + jump)
	if err != nil {
		return err
	}

	obj.SetType(base.Type())
	if err := p.ReadAndApplyDelta(obj, base); err != nil {
		return err
	}

	return nil
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
func (p Parser) ReadNegativeOffset() (int64, error) {
	var c byte
	var err error

	if c, err = p.ReadByte(); err != nil {
		return 0, err
	}

	var offset = int64(c & maskLength)
	for moreBytesInLength(c) {
		offset++
		if c, err = p.ReadByte(); err != nil {
			return 0, err
		}
		offset = (offset << lengthBits) + int64(c&maskLength)
	}

	return -offset, nil
}
