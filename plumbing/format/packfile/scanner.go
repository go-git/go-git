package packfile

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	gogithash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/binary"
	gogitsync "github.com/go-git/go-git/v6/utils/sync"
)

var (
	// ErrEmptyPackfile is returned by ReadHeader when no data is found in the packfile.
	ErrEmptyPackfile = NewError("empty packfile")
	// ErrBadSignature is returned by ReadHeader when the signature in the packfile is incorrect.
	ErrBadSignature = NewError("malformed pack file signature")
	// ErrMalformedPackfile is returned when the packfile format is incorrect.
	ErrMalformedPackfile = NewError("malformed pack file")
	// ErrUnsupportedVersion is returned by ReadHeader when the packfile version is
	// different than VersionSupported.
	ErrUnsupportedVersion = NewError("unsupported packfile version")
	// ErrSeekNotSupported returned if seek is not support.
	ErrSeekNotSupported = NewError("not seek support")
)

// Scanner provides sequential access to the data stored in a Git packfile.
//
// A Git packfile is a compressed binary format that stores multiple Git objects,
// such as commits, trees, delta objects and blobs. These packfiles are used to
// reduce the size of data when transferring or storing Git repositories.
//
// A Git packfile is structured as follows:
//
//	+----------------------------------------------------+
//	|                 PACK File Header                   |
//	+----------------------------------------------------+
//	| "PACK"  | Version Number | Number of Objects       |
//	| (4 bytes)  |  (4 bytes)   |    (4 bytes)           |
//	+----------------------------------------------------+
//	|                  Object Entry #1                   |
//	+----------------------------------------------------+
//	|  Object Header  |  Compressed Object Data / Delta  |
//	| (type + size)   |  (var-length, zlib compressed)   |
//	+----------------------------------------------------+
//	|                         ...                        |
//	+----------------------------------------------------+
//	|                  PACK File Footer                  |
//	+----------------------------------------------------+
//	|                SHA-1 Checksum (20 bytes)           |
//	+----------------------------------------------------+
//
// For upstream docs, refer to https://git-scm.com/docs/gitformat-pack.
type Scanner struct {
	// version holds the packfile version.
	version Version
	// objects holds the quantiy of objects within the packfile.
	objects uint32
	// objIndex is the current index when going through the packfile objects.
	objIndex int
	// hasher is used to hash non-delta objects.
	hasher plumbing.Hasher
	// hasher256 is optional and used to hash the non-delta objects using SHA256.
	hasher256 *plumbing.Hasher256
	// crc is used to generate the CRC-32 checksum of each object's content.
	crc hash.Hash32
	// packhash hashes the pack contents so that at the end it is able to
	// validate the packfile's footer checksum against the calculated hash.
	packhash gogithash.Hash

	// next holds what state function should be executed on the next
	// call to Scan().
	nextFn stateFn
	// packData holds the data for the last successful call to Scan().
	packData PackData
	// err holds the first error that occurred.
	err error

	m sync.Mutex

	// storage is optional, and when set is used to store full objects found.
	// Note that delta objects are not stored.
	storage storer.EncodedObjectStorer

	*scannerReader
	zr  gogitsync.ZLibReader
	buf bytes.Buffer
}

// NewScanner creates a new instance of Scanner.
func NewScanner(rs io.Reader, opts ...ScannerOption) *Scanner {
	dict := make([]byte, 16*1024)
	crc := crc32.NewIEEE()
	packhash := gogithash.New(gogithash.CryptoType)

	r := &Scanner{
		scannerReader: newScannerReader(rs, io.MultiWriter(crc, packhash)),
		zr:            gogitsync.NewZlibReader(&dict),
		objIndex:      -1,
		hasher:        plumbing.NewHasher(plumbing.AnyObject, 0),
		crc:           crc,
		packhash:      packhash,
		nextFn:        packHeaderSignature,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Scan scans a Packfile sequently. Each call will navigate from a section
// to the next, until the entire file is read.
//
// The section data can be accessed via calls to Data(). Example:
//
//	for scanner.Scan() {
//	    v := scanner.Data().Value()
//
//		switch scanner.Data().Section {
//		case HeaderSection:
//			header := v.(Header)
//			fmt.Println("[Header] Objects Qty:", header.ObjectsQty)
//		case ObjectSection:
//			oh := v.(ObjectHeader)
//			fmt.Println("[Object] Object Type:", oh.Type)
//		case FooterSection:
//			checksum := v.(plumbing.Hash)
//			fmt.Println("[Footer] Checksum:", checksum)
//		}
//	}
func (r *Scanner) Scan() bool {
	r.m.Lock()
	defer r.m.Unlock()

	if r.err != nil || r.nextFn == nil {
		return false
	}

	if err := scan(r); err != nil {
		r.err = err
		return false
	}

	return true
}

// Reset resets the current scanner, enabling it to be used to scan the
// same Packfile again.
func (r *Scanner) Reset() {
	r.scannerReader.Flush()
	r.scannerReader.Seek(0, io.SeekStart)
	r.packhash.Reset()

	r.objIndex = -1
	r.version = 0
	r.objects = 0
	r.packData = PackData{}
	r.err = nil
	r.nextFn = packHeaderSignature
}

// Data returns the pack data based on the last call to Scan().
func (r *Scanner) Data() PackData {
	return r.packData
}

// Data returns the first error that occurred on the last call to Scan().
// Once an error occurs, calls to Scan() becomes a no-op.
func (r *Scanner) Error() error {
	return r.err
}

func (r *Scanner) SeekFromStart(offset int64) error {
	r.Reset()

	if !r.Scan() {
		return fmt.Errorf("failed to reset and read header")
	}

	_, err := r.scannerReader.Seek(offset, io.SeekStart)
	return err
}

func (s *Scanner) WriteObject(oh *ObjectHeader, writer io.Writer) error {
	if oh.content.Len() > 0 {
		_, err := io.Copy(writer, bytes.NewReader(oh.content.Bytes()))
		return err
	}

	// If the oh is not an external ref and we don't have the
	// content offset, we won't be able to inflate via seeking through
	// the packfile.
	if oh.externalRef && oh.ContentOffset == 0 {
		return plumbing.ErrObjectNotFound
	}

	// Not a seeker data source.
	if s.seeker == nil {
		return plumbing.ErrObjectNotFound
	}

	err := s.inflateContent(oh.ContentOffset, writer)
	if err != nil {
		return ErrReferenceDeltaNotFound
	}

	return nil
}

func (s *Scanner) inflateContent(contentOffset int64, writer io.Writer) error {
	_, err := s.scannerReader.Seek(contentOffset, io.SeekStart)
	if err != nil {
		return err
	}

	err = s.zr.Reset(s.scannerReader)
	if err != nil {
		return fmt.Errorf("zlib reset error: %s", err)
	}

	_, err = io.Copy(writer, s.zr.Reader)
	if err != nil {
		return err
	}

	return nil
}

// scan goes through the next stateFn.
//
// State functions are chained by returning a non-nil value for stateFn.
// In such cases, the returned stateFn will be called immediately after
// the current func.
func scan(r *Scanner) error {
	var err error
	for state := r.nextFn; state != nil; {
		state, err = state(r)
		if err != nil {
			return err
		}
	}
	return nil
}

// stateFn defines each individual state within the state machine that
// represents a packfile.
type stateFn func(*Scanner) (stateFn, error)

// packHeaderSignature validates the packfile's header signature and
// returns [ErrBadSignature] if the value provided is invalid.
//
// This is always the first state of a packfile and starts the chain
// that handles the entire packfile header.
func packHeaderSignature(r *Scanner) (stateFn, error) {
	start := make([]byte, 4)
	_, err := r.Read(start)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadSignature, err)
	}

	if bytes.Equal(start, signature) {
		return packVersion, nil
	}

	return nil, ErrBadSignature
}

// packVersion parses the packfile version. It returns [ErrMalformedPackfile]
// when the version cannot be parsed. If a valid version is parsed, but it is
// not currently supported, it returns [ErrUnsupportedVersion] instead.
func packVersion(r *Scanner) (stateFn, error) {
	version, err := binary.ReadUint32(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read version", ErrMalformedPackfile)
	}

	v := Version(version)
	if !v.Supported() {
		return nil, ErrUnsupportedVersion
	}

	r.version = v
	return packObjectsQty, nil
}

// packObjectsQty parses the quantity of objects that the packfile contains.
// If the value cannot be parsed, [ErrMalformedPackfile] is returned.
//
// This state ends the packfile header chain.
func packObjectsQty(r *Scanner) (stateFn, error) {
	qty, err := binary.ReadUint32(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read number of objects", ErrMalformedPackfile)
	}
	if qty == 0 {
		return packFooter, nil
	}

	r.objects = qty
	r.packData = PackData{
		Section: HeaderSection,
		header:  Header{Version: r.version, ObjectsQty: r.objects},
	}
	r.nextFn = objectEntry

	return nil, nil
}

// objectEntry handles the object entries within a packfile. This is generally
// split between object headers and their contents.
//
// The object header contains the object type and size. If the type cannot be parsed,
// [ErrMalformedPackfile] is returned.
//
// When SHA256 is enabled, the scanner will also calculate the SHA256 for each object.
func objectEntry(r *Scanner) (stateFn, error) {
	if r.objIndex+1 >= int(r.objects) {
		return packFooter, nil
	}
	r.objIndex++

	offset := r.scannerReader.offset

	r.scannerReader.Flush()
	r.crc.Reset()

	b := []byte{0}
	_, err := r.Read(b)
	if err != nil {
		return nil, err
	}

	typ := parseType(b[0])
	if !typ.Valid() {
		return nil, fmt.Errorf("%w: invalid object type: %v", ErrMalformedPackfile, b[0])
	}

	size, err := readVariableLengthSize(b[0], r)
	if err != nil {
		return nil, err
	}

	oh := ObjectHeader{
		Offset:   offset,
		Type:     typ,
		diskType: typ,
		Size:     int64(size),
	}

	switch oh.Type {
	case plumbing.OFSDeltaObject, plumbing.REFDeltaObject:
		// For delta objects, we need to skip the base reference
		if oh.Type == plumbing.OFSDeltaObject {
			no, err := binary.ReadVariableWidthInt(r.scannerReader)
			if err != nil {
				return nil, err
			}
			oh.OffsetReference = oh.Offset - no
		} else {
			ref, err := binary.ReadHash(r.scannerReader)
			if err != nil {
				return nil, err
			}
			oh.Reference = ref
		}
	}

	oh.ContentOffset = r.scannerReader.offset
	err = r.zr.Reset(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("zlib reset error: %s", err)
	}

	if !oh.Type.IsDelta() {
		r.hasher.Reset(oh.Type, oh.Size)

		var mw io.Writer = r.hasher
		if r.storage != nil {
			w, err := r.storage.RawObjectWriter(oh.Type, oh.Size)
			if err != nil {
				return nil, err
			}

			defer w.Close()
			mw = io.MultiWriter(r.hasher, w)
		}

		if r.hasher256 != nil {
			r.hasher256.Reset(oh.Type, oh.Size)
			mw = io.MultiWriter(mw, r.hasher256)
		}

		// For non delta objects, simply calculate the hash of each object.
		_, err = io.CopyBuffer(mw, r.zr.Reader, r.buf.Bytes())
		if err != nil {
			return nil, err
		}

		oh.Hash = r.hasher.Sum()
		if r.hasher256 != nil {
			h := r.hasher256.Sum()
			oh.Hash256 = &h
		}
	} else {
		// If data source is not io.Seeker, keep the content
		// in the cache, so that it can be accessed by the Parser.
		if r.scannerReader.seeker == nil {
			_, err = oh.content.ReadFrom(r.zr.Reader)
			if err != nil {
				return nil, err
			}
		} else {
			// We don't know the compressed length, so we can't seek to
			// the next object, we must discard the data instead.
			_, err = io.Copy(io.Discard, r.zr.Reader)
			if err != nil {
				return nil, err
			}
		}
	}
	r.scannerReader.Flush()
	oh.Crc32 = r.crc.Sum32()

	r.packData.Section = ObjectSection
	r.packData.objectHeader = oh

	return nil, nil
}

// packFooter parses the packfile checksum.
// If the checksum cannot be parsed, or it does not match the checksum
// calculated during the scanning process, an [ErrMalformedPackfile] is
// returned.
func packFooter(r *Scanner) (stateFn, error) {
	r.scannerReader.Flush()
	actual := r.packhash.Sum(nil)

	checksum, err := binary.ReadHash(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("cannot read PACK checksum: %w", ErrMalformedPackfile)
	}

	if !bytes.Equal(actual, checksum[:]) {
		return nil, fmt.Errorf("checksum mismatch expected %q but found %q: %w",
			hex.EncodeToString(actual), checksum, ErrMalformedPackfile)
	}

	r.packData.Section = FooterSection
	r.packData.checksum = checksum
	r.nextFn = nil

	return nil, nil
}

func readVariableLengthSize(first byte, reader io.ByteReader) (uint64, error) {
	// Extract the first part of the size (last 3 bits of the first byte).
	size := uint64(first & 0x0F)

	// |  001xxxx | xxxxxxxx | xxxxxxxx | ...
	//
	//	 ^^^       ^^^^^^^^   ^^^^^^^^
	//	Type      Size Part 1   Size Part 2
	//
	// Check if more bytes are needed to fully determine the size.
	if first&maskContinue != 0 {
		shift := uint(4)

		for {
			b, err := reader.ReadByte()
			if err != nil {
				return 0, err
			}

			// Add the next 7 bits to the size.
			size |= uint64(b&0x7F) << shift

			// Check if the continuation bit is set.
			if b&maskContinue == 0 {
				break
			}

			// Prepare for the next byte.
			shift += 7
		}
	}
	return size, nil
}

func parseType(b byte) plumbing.ObjectType {
	return plumbing.ObjectType((b & maskType) >> firstLengthBits)
}
