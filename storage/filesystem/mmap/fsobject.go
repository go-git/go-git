//go:build darwin || linux

package mmap

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

	// If this is a delta object and autoResolve is enabled,
	// resolve metadata eagerly.
	if typ.IsDelta() && autoResolve {
		obj.resolveMetadata()
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

	// If this is a delta object and auto resolve is enabled,
	// return the contents of the resolved object.
	if o.diskType.IsDelta() && o.autoResolve {
		return o.resolveDelta()
	}

	start := o.toDataOffset(o.offset)

	// If type is delta, and we are not resolving it, we return
	// the raw deltified object.
	if o.diskType.IsDelta() {
		data := o.scanner.packMmap[start : start+o.size]
		br := sync.GetBufioReader(bytes.NewReader(data))
		rc := ioutil.NewReadCloser(br, ioutil.CloserFunc(func() error {
			sync.PutBufioReader(br)
			return nil
		}))

		return rc, nil
	}

	data := o.scanner.packMmap[start:]
	br := sync.GetBufioReader(bytes.NewReader(data))
	zr, err := sync.GetZlibReader(br)
	if err != nil {
		return nil, err
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

	// enable Reader() to get the resolved content, as opposed to delta's.
	o.autoResolve = true

	pos := o.toDataOffset(o.offset)

	// Get the base object to inherit its type.
	var base plumbing.EncodedObject
	var err error
	if o.diskType == plumbing.OFSDeltaObject {
		reader := bytes.NewReader(o.scanner.packMmap[pos:])
		negativeOffset, err := binary.ReadVariableWidthInt(reader)
		if err != nil {
			return fmt.Errorf("failed to read OFS delta offset: %w", err)
		}
		baseOffset := uint64(o.offset) - uint64(negativeOffset)
		consumed := len(o.scanner.packMmap[pos:]) - reader.Len()
		pos += int64(consumed)

		//nolint:staticcheck
		base, err = o.scanner.GetByOffset(baseOffset) //nolint:ineffassign
	} else {
		hashSize := o.scanner.hashSize
		end := pos + int64(hashSize)
		if end > int64(len(o.scanner.packMmap)) {
			return fmt.Errorf("invalid REF delta: hash out of bounds")
		}
		baseHash, _ := plumbing.FromBytes(o.scanner.packMmap[pos:end])
		pos = end

		base, err = o.scanner.Get(baseHash)
	}
	if err != nil {
		return fmt.Errorf("failed to get base object: %w", err)
	}

	// Now read the delta header to get the target size.
	deltaReader := bytes.NewReader(o.scanner.packMmap[pos:])
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
	first := o.scanner.packMmap[offset] // Skip type byte.
	offset++

	// Skip the size bytes (variable length encoding).
	for first&maskContinue != 0 {
		first = o.scanner.packMmap[offset]
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
		reader := bytes.NewReader(o.scanner.packMmap[pos:])
		negativeOffset, err := binary.ReadVariableWidthInt(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read OFS delta offset: %w", err)
		}
		baseOffset = uint64(o.offset) - uint64(negativeOffset)

		consumed := len(o.scanner.packMmap[pos:]) - reader.Len()
		pos += int64(consumed)
	} else {
		hashSize := o.scanner.hashSize
		if pos+int64(hashSize) > int64(len(o.scanner.packMmap)) {
			return nil, fmt.Errorf("invalid REF delta: hash out of bounds")
		}

		baseHash, _ = plumbing.FromBytes(o.scanner.packMmap[pos : pos+int64(hashSize)])
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
	defer baseReader.Close()

	baseBuf := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(baseBuf)
	_, err = baseBuf.ReadFrom(baseReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read base content: %w", err)
	}

	deltaReader := bytes.NewReader(o.scanner.packMmap[pos:])
	br := bufio.NewReader(deltaReader)
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
