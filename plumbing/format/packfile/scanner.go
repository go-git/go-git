package packfile

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"errors"
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
	ErrBadSignature = NewError("bad signature")
	// ErrMalformedPackfile is returned when the packfile format is incorrect.
	ErrMalformedPackfile = NewError("malformed pack file")
	// ErrUnsupportedVersion is returned by ReadHeader when the packfile version is
	// different than VersionSupported.
	ErrUnsupportedVersion = NewError("unsupported packfile version")
	// ErrSeekNotSupported returned if seek is not support.
	ErrSeekNotSupported = NewError("not seek support")
	// ErrInflatedSizeMismatch is returned when a packfile object inflates to
	// more bytes than the size declared in its object header. A well-formed
	// packfile never produces more data than the declared size; exceeding it
	// indicates a structurally invalid entry.
	ErrInflatedSizeMismatch = errors.New("packfile: inflated object exceeds declared size")
)

// boundedWriter passes writes through to w up to limit bytes total, then
// returns ErrInflatedSizeMismatch. It is used to enforce that a packfile
// object's inflated length does not exceed the size declared in its header.
type boundedWriter struct {
	w     io.Writer
	limit int64
	n     int64
}

// Write forwards p to the underlying writer while keeping the running total
// at or below limit. On overrun it forwards the legal prefix and reports
// the number of bytes actually consumed alongside ErrInflatedSizeMismatch,
// matching the contract in io.Writer. A write error from the underlying
// writer during overrun-handling is joined with ErrInflatedSizeMismatch so
// it is not silently dropped.
func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.n+int64(len(p)) > b.limit {
		remain := int(b.limit - b.n)
		err := error(ErrInflatedSizeMismatch)
		if remain > 0 {
			n, werr := b.w.Write(p[:remain])
			b.n += int64(n)
			if werr != nil {
				err = errors.Join(ErrInflatedSizeMismatch, werr)
			}
			return n, err
		}
		return 0, err
	}
	n, err := b.w.Write(p)
	b.n += int64(n)
	return n, err
}

// BoundedReadCloser wraps a ReadCloser and reports ErrInflatedSizeMismatch
// once more than limit bytes have been read. It is used by the on-demand
// object readers to enforce the same bound that the scanner applies during
// a forward scan, so a lazy Read of a packfile object cannot stream past
// its declared inflated size.
//
// The implementation builds on io.LimitedReader with the standard
// overrun-detection trick: request limit+1 bytes from the underlying so
// that the moment the sentinel byte materializes (LimitedReader.N drops
// to zero) we know the source produced more than limit bytes.
type BoundedReadCloser struct {
	lr      io.LimitedReader
	closer  io.Closer
	overrun bool
}

// NewBoundedReadCloser wraps rc so that the cumulative bytes returned from
// Read never exceed limit. The first call that would have returned a byte
// past limit instead returns ErrInflatedSizeMismatch; subsequent calls
// keep returning the same error. A negative limit is treated as zero, so
// the first byte produced by rc surfaces ErrInflatedSizeMismatch.
func NewBoundedReadCloser(rc io.ReadCloser, limit int64) *BoundedReadCloser {
	if limit < 0 {
		limit = 0
	}
	return &BoundedReadCloser{
		lr:     io.LimitedReader{R: rc, N: limit + 1},
		closer: rc,
	}
}

// Read forwards Read up to the configured byte limit. When the underlying
// stream produces the limit+1 sentinel byte, the legal prefix is returned
// alongside ErrInflatedSizeMismatch; on subsequent calls only the error
// is returned.
func (b *BoundedReadCloser) Read(p []byte) (int, error) {
	if b.overrun {
		return 0, ErrInflatedSizeMismatch
	}
	n, err := b.lr.Read(p)
	if b.lr.N == 0 {
		b.overrun = true
		return n - 1, ErrInflatedSizeMismatch
	}
	return n, err
}

// Close closes the underlying ReadCloser.
func (b *BoundedReadCloser) Close() error { return b.closer.Close() }

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
	// ctx is the context of the in-progress scan operation. Scan is a
	// state machine driven step-by-step by callers (io.Closer-style, no
	// per-call context parameter), so the operation context is carried
	// here; Parser.Parse sets it for the duration of the parse.
	ctx context.Context

	*scannerReader
	rbuf *bufio.Reader

	lowMemoryMode bool
}

// NewScanner creates a new instance of Scanner.
func NewScanner(rs io.Reader, opts ...ScannerOption) *Scanner {
	crc := crc32.NewIEEE()
	packhash := gogithash.New(crypto.SHA1)

	r := &Scanner{
		ctx:      context.Background(),
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

	r.scannerReader = newScannerReader(rs, io.MultiWriter(crc, r.packhash), r.rbuf)

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
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			err = fmt.Errorf("%w: %w", ErrMalformedPackfile, err)
		}

		r.err = err
		return false
	}

	return true
}

// Reset resets the current scanner, enabling it to be used to scan the
// same Packfile again.
func (r *Scanner) Reset() error {
	if err := r.Flush(); err != nil {
		return err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	r.packhash.Reset()

	r.objIndex = -1
	r.version = 0
	r.objects = 0
	r.packData = PackData{}
	r.err = nil
	r.nextFn = packHeaderSignature
	return nil
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
	if err := r.Reset(); err != nil {
		return err
	}

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

	err := r.inflateContent(oh.ContentOffset, writer, oh.Size)
	if err != nil {
		return ErrReferenceDeltaNotFound
	}

	return nil
}

func (r *Scanner) inflateContent(contentOffset int64, writer io.Writer, declaredSize int64) error {
	bounded := &boundedWriter{w: writer, limit: declaredSize}
	_, err := r.Seek(contentOffset, io.SeekStart)
	if err != nil {
		return err
	}

	zr, err := gogitsync.GetZlibReader(r.scannerReader)
	if err != nil {
		return fmt.Errorf("zlib reset error: %w", err)
	}
	defer gogitsync.PutZlibReader(zr)

	_, err = ioutil.CopyBufferPool(bounded, zr)
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
	n, err := r.Read(start)
	if err != nil {
		if n == 0 && err == io.EOF {
			return nil, ErrEmptyPackfile
		}
		return nil, fmt.Errorf("read signature: %w", err)
	}

	if bytes.Equal(start, signature) {
		return packVersion, nil
	}

	return nil, fmt.Errorf("%w: %w", ErrMalformedPackfile, ErrBadSignature)
}

// packVersion parses the packfile version. It returns [ErrMalformedPackfile]
// when the version cannot be parsed. If a valid version is parsed, but it is
// not currently supported, it returns [ErrUnsupportedVersion] instead.
func packVersion(r *Scanner) (stateFn, error) {
	version, err := binary.ReadUint32(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("read version: %w", err)
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
		return nil, fmt.Errorf("read number of objects: %w", err)
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

	if err := r.Flush(); err != nil {
		return nil, err
	}
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
		if errors.Is(err, packutil.ErrLengthOverflow) {
			return nil, fmt.Errorf("%w: %w", ErrMalformedPackfile, err)
		}
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
		oh.Hash.ResetBySize(r.objectIDSize)

		// For delta objects, we need to skip the base reference
		if oh.Type == plumbing.OFSDeltaObject {
			no, err := binary.ReadVariableWidthInt(r.scannerReader)
			if err != nil {
				return nil, err
			}
			if err := ValidateOFSDeltaBase(oh.Offset, no); err != nil {
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
		return nil, fmt.Errorf("zlib reset error: %w", err)
	}
	defer gogitsync.PutZlibReader(zr)

	mw := io.Discard
	if !oh.Type.IsDelta() {
		r.hasher.Reset(oh.Type, oh.Size)
		mw = r.hasher
		if r.storage != nil {
			w, err := r.storage.RawObjectWriter(r.ctx, oh.Type, oh.Size)
			if err != nil {
				return nil, err
			}

			defer func() { _ = w.Close() }()
			mw = io.MultiWriter(r.hasher, w)
		}
	}

	// If low memory mode isn't supported, and either the reader
	// isn't seekable or this is a delta object, keep the contents
	// of the objects in memory.
	if !r.lowMemoryMode && (oh.Type.IsDelta() || r.seeker == nil) {
		oh.content = gogitsync.GetBytesBuffer()
		mw = io.MultiWriter(mw, oh.content)
	}

	// Bind the inflated stream by the size declared in the object header.
	// A well-formed packfile never produces more inflated bytes than that
	// value, so any overrun signals a malformed entry. For delta entries
	// the declared size is the size of the delta instruction stream, not
	// the resolved object.
	mw = &boundedWriter{w: mw, limit: oh.Size}

	_, err = ioutil.CopyBufferPool(mw, zr)
	if err != nil {
		return nil, err
	}

	if err := r.Flush(); err != nil {
		return nil, err
	}

	oh.Crc32 = r.crc.Sum32()
	if !oh.Type.IsDelta() {
		oh.Hash = r.hasher.Sum()
	}

	r.packData.Section = ObjectSection
	r.packData.objectHeader = oh

	return nil, nil
}

// packFooter parses the packfile checksum.
// If the checksum cannot be parsed, or it does not match the checksum
// calculated during the scanning process, an [ErrMalformedPackfile] is
// returned.
func packFooter(r *Scanner) (stateFn, error) {
	if err := r.Flush(); err != nil {
		return nil, err
	}

	actual := r.packhash.Sum(nil)

	var checksum plumbing.Hash
	checksum.ResetBySize(r.objectIDSize)
	_, err := checksum.ReadFrom(r.scannerReader)
	if err != nil {
		return nil, fmt.Errorf("read pack checksum: %w", err)
	}

	if checksum.Compare(actual) != 0 {
		return nil, fmt.Errorf("%w: checksum mismatch: %q instead of %q",
			ErrMalformedPackfile, hex.EncodeToString(actual), checksum)
	}

	r.packData.Section = FooterSection
	r.packData.checksum = checksum
	r.nextFn = nil

	return nil, nil
}
