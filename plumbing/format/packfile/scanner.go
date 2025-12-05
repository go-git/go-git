package packfile

import (
	"bufio"
	"bytes"
	"crypto"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
	gogithash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/binary"
	"github.com/go-git/go-git/v6/utils/ioutil"
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
	// objects holds the quantity of objects within the packfile.
	objects uint32
	// objIndex is the current index when going through the packfile objects.
	objIndex int
	// hasher is used to hash non-delta objects.
	hasher plumbing.Hasher
	// hasher256 is optional and used to hash the non-delta objects using SHA256.
	hasher256 *plumbing.Hasher
	// crc is used to generate the CRC-32 checksum of each object's content.
	crc hash.Hash32
	// packhash hashes the pack contents so that at the end it is able to
	// validate the packfile's footer checksum against the calculated hash.
	packhash gogithash.Hash
	// objectIdSize holds the object ID size.
	objectIDSize int

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
	rbuf *bufio.Reader

	lowMemoryMode bool
}

// NewScanner creates a new instance of Scanner.
func NewScanner(rs io.Reader, opts ...ScannerOption) *Scanner {
	crc := crc32.NewIEEE()
	packhash := gogithash.New(crypto.SHA1)

	r := &Scanner{
		objIndex: -1,
		hasher:   plumbing.NewHasher(format.SHA1, plumbing.AnyObject, 0),
		crc:      crc,
		packhash: packhash,
		nextFn:   packHeaderSignature,
		// Set the default size, which can be overridden by opts.
		objectIDSize: packhash.Size(),
	}

	for _, opt := range opts {
		opt(r)
	}

	r.scannerReader = newScannerReader(rs, io.MultiWriter(crc, packhash), r.rbuf)

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
	r.Flush()
	r.Seek(0, io.SeekStart)
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

// SeekFromStart seeks to the given offset from the start of the packfile.
func (r *Scanner) SeekFromStart(offset int64) error {
	r.Reset()

	if !r.Scan() {
		return fmt.Errorf("failed to reset and read header")
	}

	_, err := r.Seek(offset, io.SeekStart)
	return err
}

// WriteObject writes the content of the given ObjectHeader to the provided writer.
func (r *Scanner) WriteObject(oh *ObjectHeader, writer io.Writer) error {
	if oh.content != nil && oh.content.Len() > 0 {
		_, err := ioutil.CopyBufferPool(writer, oh.content)
		return err
	}

	// If the oh is not an external ref and we don't have the
	// content offset, we won't be able to inflate via seeking through
	// the packfile.
	if oh.externalRef && oh.ContentOffset == 0 {
		return plumbing.ErrObjectNotFound
	}

	// Not a seeker data source.
	if r.seeker == nil {
		return plumbing.ErrObjectNotFound
	}

	err := r.inflateContent(oh.ContentOffset, writer)
	if err != nil {
		return ErrReferenceDeltaNotFound
	}

	return nil
}

func (r *Scanner) inflateContent(contentOffset int64, writer io.Writer) error {
	_, err := r.Seek(contentOffset, io.SeekStart)
	if err != nil {
		return err
	}

	zr, err := gogitsync.GetZlibReader(r.scannerReader)
	if err != nil {
		return fmt.Errorf("zlib reset error: %s", err)
	}
	defer gogitsync.PutZlibReader(zr)

	_, err = ioutil.CopyBufferPool(writer, zr)
	return err
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

	offset := r.offset

	r.Flush()
	r.crc.Reset()

	b := []byte{0}
	_, err := r.Read(b)
	if err != nil {
		return nil, err
	}

	typ := packutil.ObjectType(b[0])
	if !typ.Valid() {
		return nil, fmt.Errorf("%w: invalid object type: %v", ErrMalformedPackfile, b[0])
	}

	size, err := packutil.VariableLengthSize(b[0], r)
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
			oh.Reference.ResetBySize(r.objectIDSize)
			_, err := oh.Reference.ReadFrom(r.scannerReader)
			if err != nil {
				return nil, err
			}
		}
	}

	oh.ContentOffset = r.offset

	zr, err := gogitsync.GetZlibReader(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("zlib reset error: %s", err)
	}
	defer gogitsync.PutZlibReader(zr)

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

		// If the reader isn't seekable, and low memory mode
		// isn't supported, keep the contents of the objects in
		// memory.
		if !r.lowMemoryMode && r.seeker == nil {
			oh.content = gogitsync.GetBytesBuffer()
			mw = io.MultiWriter(mw, oh.content)
		}
		if r.hasher256 != nil {
			r.hasher256.Reset(oh.Type, oh.Size)
			mw = io.MultiWriter(mw, r.hasher256)
		}

		// For non delta objects, simply calculate the hash of each object.
		_, err = ioutil.CopyBufferPool(mw, zr)
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
		if !r.lowMemoryMode {
			oh.content = gogitsync.GetBytesBuffer()
			_, err = oh.content.ReadFrom(zr)
			if err != nil {
				return nil, err
			}
		} else {
			// We don't know the compressed length, so we can't seek to
			// the next object, we must discard the data instead.
			_, err = ioutil.CopyBufferPool(io.Discard, zr)
			if err != nil {
				return nil, err
			}
		}
	}
	r.Flush()
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
	r.Flush()

	actual := r.packhash.Sum(nil)

	var checksum plumbing.Hash
	_, err := checksum.ReadFrom(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("cannot read PACK checksum: %w", ErrMalformedPackfile)
	}

	if checksum.Compare(actual) != 0 {
		return nil, fmt.Errorf("checksum mismatch expected %q but found %q: %w",
			hex.EncodeToString(actual), checksum, ErrMalformedPackfile)
	}

	r.packData.Section = FooterSection
	r.packData.checksum = checksum
	r.nextFn = nil

	return nil, nil
}
