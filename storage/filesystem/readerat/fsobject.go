package readerat

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	gosync "sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
	"github.com/go-git/go-git/v6/utils/binary"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

const (
	maskContinue = 0x80 // 1000 0000
)

type ondemandObject struct {
	hash        plumbing.Hash
	offset      int64
	size        int64
	typ         plumbing.ObjectType
	scanner     *PackScanner
	diskType    plumbing.ObjectType // The type stored on disk (may be delta)
	autoResolve bool

	m gosync.RWMutex
}

// newOndemandObject creates a new object representation that is linked to a
// PackScanner, which is used to fetch its content on demand.
func newOndemandObject(
	hash plumbing.Hash,
	typ plumbing.ObjectType,
	offset int64,
	size int64,
	scanner *PackScanner,
	autoResolve bool,
) *ondemandObject {
	obj := &ondemandObject{
		hash:        hash,
		offset:      offset,
		size:        size,
		typ:         typ,
		diskType:    typ,
		scanner:     scanner,
		autoResolve: autoResolve,
	}

	if typ.IsDelta() && autoResolve {
		_ = obj.resolveMetadata()
	}

	return obj
}

// Resolve resolves a deltified object, updating size, offset and type based
// on the real object.
//
// For non-delta objects this is a no-op.
func (o *ondemandObject) Resolve() error {
	o.m.RLock()
	if o.diskType.IsDelta() && o.typ.IsDelta() {
		o.m.RUnlock()
		return o.resolveMetadata()
	}
	o.m.RUnlock()

	return nil
}

// Reader implements the plumbing.EncodedObject interface.
func (o *ondemandObject) Reader() (io.ReadCloser, error) {
	o.m.Lock()
	defer o.m.Unlock()

	if o.diskType.IsDelta() && o.autoResolve {
		return o.resolveDelta()
	}

	start := o.toDataOffset(o.offset)

	if o.diskType.IsDelta() {
		dataBuf := make([]byte, o.size)
		n, err := o.scanner.packReader.ReadAt(dataBuf, start)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read delta data: %w", err)
		}

		br := sync.GetBufioReader(bytes.NewReader(dataBuf[:n]))
		rc := ioutil.NewReadCloser(br, ioutil.CloserFunc(func() error {
			sync.PutBufioReader(br)
			return nil
		}))

		return rc, nil
	}

	sr := io.NewSectionReader(o.scanner.packReader, start, o.scanner.packSize-start)
	br := sync.GetBufioReader(sr)
	zr, err := sync.GetZlibReader(br)
	if err != nil {
		sync.PutBufioReader(br)
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}

	return &zlibReadCloser{r: zr, rbuf: br}, nil
}

// Hash holds the object's ID.
func (o *ondemandObject) Hash() plumbing.Hash {
	return o.hash
}

// Size holds the object's size.
func (o *ondemandObject) Size() int64 {
	o.m.RLock()
	defer o.m.RUnlock()

	return o.size
}

// Type holds the object's ObjectType.
func (o *ondemandObject) Type() plumbing.ObjectType {
	o.m.RLock()
	defer o.m.RUnlock()

	return o.typ
}

// TODO: Create a read-only EncodedObject interface, to avoid the LSP
// violation as per methods below.

// SetSize only exists to implement the plumbing.EncodedObject interface.
// This method has no effect to the underlying, as it is a no-op.
func (o *ondemandObject) SetSize(int64) {}

// SetType only exists to implement the plumbing.EncodedObject interface.
// This method has no effect to the underlying, as it is a no-op.
func (o *ondemandObject) SetType(plumbing.ObjectType) {}

// Writer only exists to implement the plumbing.EncodedObject interface.
// This method always returns a nil writer.
func (o *ondemandObject) Writer() (io.WriteCloser, error) {
	return nil, nil
}

// resolveMetadata resolves the type and size for the delta object without
// fully materializing it.
//
// Calling it on a non-delta object is a no-op. Subsequent calls on a delta
// object will also become a no-op.
func (o *ondemandObject) resolveMetadata() error {
	o.m.RLock()
	if !o.typ.IsDelta() {
		o.m.RUnlock()
		return nil
	}
	o.m.RUnlock()

	o.m.Lock()
	defer o.m.Unlock()

	o.autoResolve = true

	pos := o.toDataOffset(o.offset)

	var base plumbing.EncodedObject
	var err error
	if o.diskType == plumbing.OFSDeltaObject {
		offsetBuf := make([]byte, 16)
		n, readErr := o.scanner.packReader.ReadAt(offsetBuf, pos)
		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("failed to read OFS delta offset: %w", readErr)
		}

		reader := bytes.NewReader(offsetBuf[:n])
		negativeOffset, err := binary.ReadVariableWidthInt(reader)
		if err != nil {
			return fmt.Errorf("failed to parse OFS delta offset: %w", err)
		}
		baseOffset := uint64(o.offset) - uint64(negativeOffset)
		consumed := n - reader.Len()
		pos += int64(consumed)

		//nolint:staticcheck
		base, err = o.scanner.GetByOffset(baseOffset) //nolint:ineffassign
	} else {
		hashSize := o.scanner.hashSize
		hashBuf := make([]byte, hashSize)
		n, readErr := o.scanner.packReader.ReadAt(hashBuf, pos)
		if readErr != nil {
			return fmt.Errorf("failed to read REF delta hash: %w", readErr)
		}
		if n != hashSize {
			return fmt.Errorf("short read for REF delta hash: got %d, expected %d", n, hashSize)
		}

		baseHash, _ := plumbing.FromBytes(hashBuf)
		pos += int64(hashSize)

		base, err = o.scanner.Get(baseHash)
	}
	if err != nil {
		return fmt.Errorf("failed to get base object: %w", err)
	}

	// Now read the delta header to get the target size.
	headerBuf := make([]byte, 512)
	n, err := o.scanner.packReader.ReadAt(headerBuf, pos)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read delta header: %w", err)
	}

	deltaReader := bytes.NewReader(headerBuf[:n])
	br := bufio.NewReader(deltaReader)
	zr, err := sync.GetZlibReader(br)
	if err != nil {
		return fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer sync.PutZlibReader(zr)

	// Wrap the zlib reader in a bufio.Reader for ByteReader interface.
	zlibBuf := bufio.NewReader(zr)

	_, err = packutil.DecodeLEB128FromReader(zlibBuf)
	if err != nil {
		return fmt.Errorf("failed to read source size from delta: %w", err)
	}

	// Read target size (inflated size).
	targetSize, err := packutil.DecodeLEB128FromReader(zlibBuf)
	if err != nil {
		return fmt.Errorf("failed to read target size from delta: %w", err)
	}

	o.typ = base.Type()
	o.size = int64(targetSize)

	return nil
}

// toDataOffset gets the object offset and returns the data offset.
func (o *ondemandObject) toDataOffset(offset int64) int64 {
	buf := make([]byte, 1)
	n, err := o.scanner.packReader.ReadAt(buf, offset)
	if err != nil || n != 1 {
		return offset + 1 // Fallback, should not happen
	}
	first := buf[0]
	offset++

	// Skip the size bytes (variable length encoding).
	for first&maskContinue != 0 {
		n, err := o.scanner.packReader.ReadAt(buf, offset)
		if err != nil || n != 1 {
			break
		}
		first = buf[0]
		offset++
	}

	return offset
}

// resolveDelta resolves a delta object by getting the base and applying the patch.
func (o *ondemandObject) resolveDelta() (io.ReadCloser, error) {
	var baseOffset uint64
	var baseHash plumbing.Hash
	var err error

	pos := o.toDataOffset(o.offset)
	if o.diskType == plumbing.OFSDeltaObject {
		// Read the negative offset (variable width int)
		offsetBuf := make([]byte, 16)
		n, readErr := o.scanner.packReader.ReadAt(offsetBuf, pos)
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("failed to read OFS delta offset: %w", readErr)
		}

		reader := bytes.NewReader(offsetBuf[:n])
		negativeOffset, err := binary.ReadVariableWidthInt(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to parse OFS delta offset: %w", err)
		}
		baseOffset = uint64(o.offset) - uint64(negativeOffset)

		consumed := n - reader.Len()
		pos += int64(consumed)
	} else {
		hashSize := o.scanner.hashSize
		hashBuf := make([]byte, hashSize)
		n, readErr := o.scanner.packReader.ReadAt(hashBuf, pos)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read REF delta hash: %w", readErr)
		}
		if n != hashSize {
			return nil, fmt.Errorf("short read for REF delta hash: got %d, expected %d", n, hashSize)
		}

		baseHash, _ = plumbing.FromBytes(hashBuf)
		pos += int64(hashSize)
	}

	// Delta objects always have a base object, where they derive most
	// of their content from.
	var base plumbing.EncodedObject
	if o.diskType == plumbing.OFSDeltaObject {
		base, err = o.scanner.GetByOffset(baseOffset)
	} else {
		base, err = o.scanner.Get(baseHash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get base object: %w", err)
	}

	baseReader, err := base.Reader()
	if err != nil {
		return nil, fmt.Errorf("failed to read base object: %w", err)
	}
	defer func() { _ = baseReader.Close() }()

	baseBuf := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(baseBuf)
	_, err = baseBuf.ReadFrom(baseReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read base content: %w", err)
	}

	// Read delta data starting from current position
	// Create a section reader for the delta data
	sr := io.NewSectionReader(o.scanner.packReader, pos, o.scanner.packSize-pos)
	br := bufio.NewReader(sr)
	zr, err := sync.GetZlibReader(br)
	if err != nil {
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer sync.PutZlibReader(zr)

	deltaBuf := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(deltaBuf)
	_, err = deltaBuf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("failed to read delta data: %w", err)
	}

	// TODO: Consider using the internal stream patch to avoid loading the
	// entire objects into memory while resolving a delta.
	result, err := packfile.PatchDelta(baseBuf.Bytes(), deltaBuf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to apply delta patch: %w", err)
	}

	return io.NopCloser(bytes.NewReader(result)), nil
}

type zlibReadCloser struct {
	r        *sync.ZLibReader
	rbuf     *bufio.Reader
	once     gosync.Once
	closeErr error
}

// Read reads up to len(p) bytes into p from the data.
func (r *zlibReadCloser) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r *zlibReadCloser) Close() (err error) {
	r.once.Do(func() {
		r.closeErr = r.r.Close()
		sync.PutZlibReader(r.r)
		sync.PutBufioReader(r.rbuf)
	})

	return r.closeErr
}
