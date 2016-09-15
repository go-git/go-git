package packfile

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
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
	// ErrSeekNotSupported returned if seek is not support
	ErrSeekNotSupported = NewError("not seek support")
)

const (
	// VersionSupported is the packfile version supported by this parser.
	VersionSupported uint32 = 2
)

// ObjectHeader contains the information related to the object, this information
// is collected from the previous bytes to the content of the object.
type ObjectHeader struct {
	Type            core.ObjectType
	Offset          int64
	Length          int64
	Reference       core.Hash
	OffsetReference int64
}

type Scanner struct {
	r   reader
	crc hash.Hash32

	// pendingObject is used to detect if an object has been read, or still
	// is waiting to be read
	pendingObject    *ObjectHeader
	version, objects uint32

	// lsSeekable says if this scanner can do Seek or not, to have a Scanner
	// seekable a r implementing io.Seeker is required
	IsSeekable bool
}

// NewScanner returns a new Scanner based on a reader, if the given reader
// implements io.ReadSeeker the Scanner will be also Seekable
func NewScanner(r io.Reader) *Scanner {
	seeker, ok := r.(io.ReadSeeker)
	if !ok {
		seeker = &trackableReader{Reader: r}
	}

	crc := crc32.NewIEEE()
	return &Scanner{
		r: &teeReader{
			newByteReadSeeker(seeker),
			crc,
		},
		crc:        crc,
		IsSeekable: ok,
	}
}

// Header reads the whole packfile header (signature, version and object count).
// It returns the version and the object count and performs checks on the
// validity of the signature and the version fields.
func (s *Scanner) Header() (version, objects uint32, err error) {
	if s.version != 0 {
		return s.version, s.objects, nil
	}

	sig, err := s.readSignature()
	if err != nil {
		if err == io.EOF {
			err = ErrEmptyPackfile
		}

		return
	}

	if !s.isValidSignature(sig) {
		err = ErrBadSignature
		return
	}

	version, err = s.readVersion()
	s.version = version
	if err != nil {
		return
	}

	if !s.isSupportedVersion(version) {
		err = ErrUnsupportedVersion.AddDetails("%d", version)
		return
	}

	objects, err = s.readCount()
	s.objects = objects
	return
}

// readSignature reads an returns the signature field in the packfile.
func (s *Scanner) readSignature() ([]byte, error) {
	var sig = make([]byte, 4)
	if _, err := io.ReadFull(s.r, sig); err != nil {
		return []byte{}, err
	}

	return sig, nil
}

// isValidSignature returns if sig is a valid packfile signature.
func (s *Scanner) isValidSignature(sig []byte) bool {
	return bytes.Equal(sig, []byte{'P', 'A', 'C', 'K'})
}

// readVersion reads and returns the version field of a packfile.
func (s *Scanner) readVersion() (uint32, error) {
	return s.readInt32()
}

// isSupportedVersion returns whether version v is supported by the parser.
// The current supported version is VersionSupported, defined above.
func (s *Scanner) isSupportedVersion(v uint32) bool {
	return v == VersionSupported
}

// readCount reads and returns the count of objects field of a packfile.
func (s *Scanner) readCount() (uint32, error) {
	return s.readInt32()
}

// ReadInt32 reads 4 bytes and returns them as a Big Endian int32.
func (s *Scanner) readInt32() (uint32, error) {
	var v uint32
	if err := binary.Read(s.r, binary.BigEndian, &v); err != nil {
		return 0, err
	}

	return v, nil
}

// NextObjectHeader returns the ObjectHeader for the next object in the reader
func (s *Scanner) NextObjectHeader() (*ObjectHeader, error) {
	if err := s.doPending(); err != nil {
		return nil, err
	}

	s.crc.Reset()

	h := &ObjectHeader{}
	s.pendingObject = h

	var err error
	h.Offset, err = s.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	h.Type, h.Length, err = s.readObjectTypeAndLength()
	if err != nil {
		return nil, err
	}

	switch h.Type {
	case core.OFSDeltaObject:
		no, err := s.readNegativeOffset()
		if err != nil {
			return nil, err
		}

		h.OffsetReference = h.Offset + no
	case core.REFDeltaObject:
		var err error
		h.Reference, err = s.readHash()
		if err != nil {
			return nil, err
		}
	}

	return h, nil
}

func (s *Scanner) doPending() error {
	if s.version == 0 {
		var err error
		s.version, s.objects, err = s.Header()
		if err != nil {
			return err
		}
	}

	return s.discardObjectIfNeeded()
}

func (s *Scanner) discardObjectIfNeeded() error {
	if s.pendingObject == nil {
		return nil
	}

	h := s.pendingObject
	n, _, err := s.NextObject(ioutil.Discard)
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
func (s *Scanner) readObjectTypeAndLength() (core.ObjectType, int64, error) {
	t, c, err := s.readType()
	if err != nil {
		return t, 0, err
	}

	l, err := s.readLength(c)

	return t, l, err
}

func (s *Scanner) readType() (core.ObjectType, byte, error) {
	var c byte
	var err error
	if c, err = s.readByte(); err != nil {
		return core.ObjectType(0), 0, err
	}

	typ := parseType(c)

	return typ, c, nil
}

// the length is codified in the last 4 bits of the first byte and in
// the last 7 bits of subsequent bytes.  Last byte has a 0 MSB.
func (s *Scanner) readLength(first byte) (int64, error) {
	length := int64(first & maskFirstLength)

	c := first
	shift := firstLengthBits
	var err error
	for moreBytesInLength(c) {
		if c, err = s.readByte(); err != nil {
			return 0, err
		}

		length += int64(c&maskLength) << shift
		shift += lengthBits
	}

	return length, nil
}

// NextObject writes the content of the next object into the reader, returns
// the number of bytes written, the CRC32 of the content and an error, if any
func (s *Scanner) NextObject(w io.Writer) (written int64, crc32 uint32, err error) {
	defer s.crc.Reset()

	s.pendingObject = nil
	written, err = s.copyObject(w)
	crc32 = s.crc.Sum32()
	return
}

// ReadRegularObject reads and write a non-deltified object
// from it zlib stream in an object entry in the packfile.
func (s *Scanner) copyObject(w io.Writer) (int64, error) {
	zr, err := zlib.NewReader(s.r)
	if err != nil {
		return -1, fmt.Errorf("zlib reading error: %s", err)
	}

	defer func() {
		closeErr := zr.Close()
		if err == nil {
			err = closeErr
		}
	}()

	return io.Copy(w, zr)
}

// Seek sets a new offset from start, returns the old position before the change
func (s *Scanner) Seek(offset int64) (previous int64, err error) {
	// if seeking we asume that you are not interested on the header
	if s.version == 0 {
		s.version = VersionSupported
	}

	previous, err = s.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return -1, err
	}

	_, err = s.r.Seek(offset, io.SeekStart)
	return previous, err
}

// Checksum returns the checksum of the packfile
func (s *Scanner) Checksum() (core.Hash, error) {
	err := s.discardObjectIfNeeded()
	if err != nil {
		return core.ZeroHash, err
	}

	return s.readHash()
}

// ReadHash reads a hash.
func (s *Scanner) readHash() (core.Hash, error) {
	var h core.Hash
	if _, err := io.ReadFull(s.r, h[:]); err != nil {
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
func (s *Scanner) readNegativeOffset() (int64, error) {
	var c byte
	var err error

	if c, err = s.readByte(); err != nil {
		return 0, err
	}

	var offset = int64(c & maskLength)
	for moreBytesInLength(c) {
		offset++
		if c, err = s.readByte(); err != nil {
			return 0, err
		}
		offset = (offset << lengthBits) + int64(c&maskLength)
	}

	return -offset, nil
}

func (s *Scanner) readByte() (byte, error) {
	b, err := s.r.ReadByte()
	if err != nil {
		return 0, err
	}

	return b, err
}

// Close reads the reader until io.EOF
func (s *Scanner) Close() error {
	_, err := io.Copy(ioutil.Discard, s.r)
	return err
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
	count int64
	io.Reader
}

// Read reads up to len(p) bytes into p.
func (r *trackableReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.count += int64(n)

	return
}

// Seek only supports io.SeekCurrent, any other operation fails
func (r *trackableReader) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekCurrent {
		return -1, ErrSeekNotSupported
	}

	return r.count, nil
}

func newByteReadSeeker(r io.ReadSeeker) *bufferedSeeker {
	return &bufferedSeeker{
		r:      r,
		Reader: *bufio.NewReader(r),
	}
}

type bufferedSeeker struct {
	r io.ReadSeeker
	bufio.Reader
}

func (r *bufferedSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		current, err := r.r.Seek(offset, whence)
		if err != nil {
			return current, err
		}

		return current - int64(r.Buffered()), nil
	}

	defer r.Reader.Reset(r.r)
	return r.r.Seek(offset, whence)
}

type reader interface {
	io.Reader
	io.ByteReader
	io.Seeker
}

type teeReader struct {
	reader
	w hash.Hash32
}

func (r *teeReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		if n, err := r.w.Write(p[:n]); err != nil {
			return n, err
		}
	}
	return
}

func (r *teeReader) ReadByte() (b byte, err error) {
	b, err = r.reader.ReadByte()
	if err == nil {
		_, err := r.w.Write([]byte{b})
		if err != nil {
			return 0, err
		}
	}

	return
}
