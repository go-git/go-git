package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
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

	_, err := parser.Parse()
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

			obj, err := iter.Next()
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

func writeTestObjectHeader(w io.ByteWriter, typ plumbing.ObjectType, size int64) {
	remaining := uint64(size)
	first := byte(typ)<<4 | byte(remaining&0x0f)
	remaining >>= 4
	if remaining > 0 {
		first |= 0x80
	}
	_ = w.WriteByte(first)

	for remaining > 0 {
		next := byte(remaining & 0x7f)
		remaining >>= 7
		if remaining > 0 {
			next |= 0x80
		}
		_ = w.WriteByte(next)
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
