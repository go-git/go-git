package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
	gogitbinary "github.com/go-git/go-git/v6/utils/binary"
)

func TestParserRejectsDeepDeltaChain(t *testing.T) {
	t.Parallel()

	objects := make([]testPackObject, 0, maxDeltaChainDepth+2)
	content := []byte{0, 0}
	parentHash := testObjectHash(plumbing.BlobObject, content)
	objects = append(objects, testPackObject{
		typ:     plumbing.BlobObject,
		content: content,
	})

	for i := range maxDeltaChainDepth + 1 {
		content = []byte{byte(i + 1), byte((i + 1) >> 8)}
		delta := buildDelta(2, 2, insertOp(content))
		objects = append(objects, testPackObject{
			typ:       plumbing.REFDeltaObject,
			content:   delta,
			reference: parentHash,
		})
		parentHash = testObjectHash(plumbing.BlobObject, content)
	}

	pack, _ := buildTestPack(t, objects...)
	parser := NewParser(bytes.NewReader(pack))

	_, err := parser.Parse(t.Context())
	require.ErrorIs(t, err, ErrMalformedPackfile)
	require.ErrorContains(t, err, "delta chain depth")
}

func TestObjectIterReturnsObjectNotFoundForMissingDeltaBase(t *testing.T) {
	t.Parallel()

	delta := buildDelta(1, 1, insertOp([]byte("b")))
	missingReference := plumbing.NewHash("1111111111111111111111111111111111111111")

	for _, test := range []struct {
		name string
		obj  testPackObject
	}{
		{
			name: "ref delta",
			obj: testPackObject{
				typ:       plumbing.REFDeltaObject,
				content:   delta,
				reference: missingReference,
			},
		},
		{
			name: "ofs delta",
			obj: testPackObject{
				typ:                 plumbing.OFSDeltaObject,
				content:             delta,
				offsetDeltaDistance: 1,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pack, offsets := buildTestPack(t, test.obj)
			file := writeTestPackFile(t, pack)
			entry := &idxfile.Entry{
				Hash:   testObjectHash(plumbing.BlobObject, []byte("unused")),
				Offset: uint64(offsets[0]),
			}
			p := NewPackfile(file, WithIdx(&singleEntryIndex{entry: entry}))
			defer p.Close()

			iter, err := p.GetByType(plumbing.BlobObject)
			require.NoError(t, err)
			defer iter.Close()

			obj, err := iter.Next(t.Context())
			require.Nil(t, obj)
			require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
		})
	}
}

type singleEntryIndex struct {
	entry *idxfile.Entry
}

func (idx *singleEntryIndex) Contains(h plumbing.Hash) (bool, error) {
	return idx.entry.Hash.Equal(h), nil
}

func (idx *singleEntryIndex) FindOffset(h plumbing.Hash) (int64, error) {
	if idx.entry.Hash.Equal(h) {
		return int64(idx.entry.Offset), nil
	}
	return 0, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) FindCRC32(h plumbing.Hash) (uint32, error) {
	if idx.entry.Hash.Equal(h) {
		return idx.entry.CRC32, nil
	}
	return 0, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) FindHash(offset int64) (plumbing.Hash, error) {
	if idx.entry.Offset == uint64(offset) {
		return idx.entry.Hash, nil
	}
	return plumbing.ZeroHash, plumbing.ErrObjectNotFound
}

func (idx *singleEntryIndex) Count() (int64, error) {
	return 1, nil
}

func (idx *singleEntryIndex) Entries() (idxfile.EntryIter, error) {
	return &singleEntryIter{entry: idx.entry}, nil
}

func (idx *singleEntryIndex) EntriesByOffset() (idxfile.EntryIter, error) {
	return &singleEntryIter{entry: idx.entry}, nil
}

func (idx *singleEntryIndex) EntriesWithPrefix(prefix []byte) (idxfile.EntryIter, error) {
	if len(prefix) == 0 || idx.entry.Hash.HasPrefix(prefix) {
		return &singleEntryIter{entry: idx.entry}, nil
	}
	return &singleEntryIter{done: true}, nil
}

func (idx *singleEntryIndex) MayContain(h plumbing.Hash) bool {
	return idx.entry.Hash.Equal(h)
}

func (idx *singleEntryIndex) Close() error { return nil }

type singleEntryIter struct {
	entry *idxfile.Entry
	done  bool
}

func (iter *singleEntryIter) Next() (*idxfile.Entry, error) {
	if iter.done {
		return nil, io.EOF
	}
	iter.done = true
	return iter.entry, nil
}

func (iter *singleEntryIter) Close() error {
	iter.done = true
	return nil
}

type testPackObject struct {
	typ                 plumbing.ObjectType
	declaredSize        int64
	content             []byte
	reference           plumbing.Hash
	offsetDeltaDistance int64
}

func buildTestPack(t *testing.T, objects ...testPackObject) ([]byte, []int64) {
	t.Helper()

	var body bytes.Buffer
	body.WriteString("PACK")
	require.NoError(t, binary.Write(&body, binary.BigEndian, uint32(2)))
	require.NoError(t, binary.Write(&body, binary.BigEndian, uint32(len(objects))))

	offsets := make([]int64, 0, len(objects))
	for _, obj := range objects {
		offsets = append(offsets, int64(body.Len()))
		declaredSize := obj.declaredSize
		if declaredSize == 0 && len(obj.content) > 0 {
			declaredSize = int64(len(obj.content))
		}

		writeTestObjectHeader(&body, obj.typ, declaredSize)
		switch obj.typ {
		case plumbing.REFDeltaObject:
			body.Write(obj.reference.Bytes())
		case plumbing.OFSDeltaObject:
			require.NoError(t, gogitbinary.WriteVariableWidthInt(&body, obj.offsetDeltaDistance))
		}
		body.Write(zlibCompress(t, obj.content))
	}

	sum := sha1.Sum(body.Bytes())
	body.Write(sum[:])
	return body.Bytes(), offsets
}

func writeTestObjectHeader(w io.Writer, typ plumbing.ObjectType, size int64) {
	first := byte(typ)<<4 | byte(size&0x0f)
	rest := uint(size >> 4)
	if rest != 0 {
		first |= 0x80
	}
	_, _ = w.Write([]byte{first})
	if rest != 0 {
		_ = packutil.EncodeLEB128ToWriter(w, rest)
	}
}

func zlibCompress(t *testing.T, content []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, err := zw.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return compressed.Bytes()
}

func testObjectHash(typ plumbing.ObjectType, content []byte) plumbing.Hash {
	h := plumbing.NewHasher(format.SHA1, typ, int64(len(content)))
	_, _ = h.Write(content)
	return h.Sum()
}

func writeTestPackFile(t *testing.T, pack []byte) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "malformed-*.pack")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	_, err = file.Write(pack)
	require.NoError(t, err)
	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)
	return file
}

// TestNewPackfile_CloseReturnsScannerError verifies that
// Packfile.Close surfaces the scanner cursor's close error in
// resolver mode. The resolver-owned handle is not closed by
// Packfile.Close (its lifetime belongs to the resolver), so the
// scanner cursor is the only error source.
func TestNewPackfile_CloseReturnsScannerError(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("scan-fail")

	cases := []struct {
		name    string
		scanErr error
		wantNil bool
	}{
		{name: "scan succeeds", scanErr: nil, wantNil: true},
		{name: "scan fails", scanErr: scanErr, wantNil: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := &Packfile{}
			p.handle = stubPackHandle{}
			p.scanReader = stubCloser{err: tc.scanErr}

			err := p.Close()
			if tc.wantNil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, scanErr)
		})
	}
}

// stubCloser is a minimal io.ReadSeekCloser that returns a
// configured error from Close.
type stubCloser struct{ err error }

func (s stubCloser) Read([]byte) (int, error)       { return 0, io.EOF }
func (s stubCloser) Seek(int64, int) (int64, error) { return 0, nil }
func (s stubCloser) Close() error                   { return s.err }

// stubPackHandle is a minimal PackHandle for tests that don't
// exercise its methods.
type stubPackHandle struct{}

func (stubPackHandle) OpenPackReader() (io.ReadSeekCloser, error) { panic("unused") }
func (stubPackHandle) OpenRandomReader() (RandomReader, error)    { panic("unused") }
func (stubPackHandle) PackHash() (plumbing.Hash, error)           { panic("unused") }

// TestNewPackfile_ExternalResolverNotClosed verifies that
// Packfile.Close does not invoke any close path against a handle
// supplied via WithPackHandle. The resolver-owned handle must
// outlive the Packfile that consumed it; only the scanner cursor
// (held in p.scanReader) is closed.
func TestNewPackfile_ExternalResolverNotClosed(t *testing.T) {
	t.Parallel()

	// Track invocations through the resolver. The resolver
	// itself is invoked exactly once during init (by the
	// scanner-init path), but here we close before init runs so
	// it should NOT be invoked at all.
	var resolverCalls atomic.Int32
	stub := stubPackHandle{}

	resolver := PackHandleResolver(func() (PackHandle, error) {
		resolverCalls.Add(1)
		return stub, nil
	})

	p := NewPackfile(nil, WithPackHandle(resolver))
	// Close without init — exercises the WithPackHandle path
	// directly. p.handle is still nil (init hasn't run);
	// p.scanReader is nil. Close must be a no-op.
	require.NoError(t, p.Close())
	assert.Zero(t, resolverCalls.Load(),
		"resolver must not be invoked during Close on an unused Packfile")

	// Second close is idempotent.
	require.NoError(t, p.Close())
}
