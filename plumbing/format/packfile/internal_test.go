package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/plumbing"
	gogitbinary "github.com/go-git/go-git/v5/utils/binary"
)

func TestParserRejectsDeepDeltaChain(t *testing.T) {
	t.Parallel()

	pack := buildLinearDeltaChainPack(t, maxDeltaChainDepth+1)
	scanner := NewScanner(bytes.NewReader(pack))
	parser, err := NewParser(scanner)
	require.NoError(t, err)

	_, err = parser.Parse()
	require.ErrorIs(t, err, ErrMalformedPackFile)
	require.ErrorContains(t, err, "delta chain depth")
}

func TestParserAcceptsMaxDepthDeltaChain(t *testing.T) {
	t.Parallel()

	pack := buildLinearDeltaChainPack(t, maxDeltaChainDepth)
	scanner := NewScanner(bytes.NewReader(pack))
	parser, err := NewParser(scanner)
	require.NoError(t, err)

	_, err = parser.Parse()
	require.NoError(t, err)
}

// buildLinearDeltaChainPack returns a pack containing one base blob followed
// by deltaCount OFS deltas, each referencing the immediately preceding object.
// The resulting chain depth (counting only delta links) equals deltaCount.
func buildLinearDeltaChainPack(t *testing.T, deltaCount int) []byte {
	t.Helper()

	objects := make([]testPackObject, 0, deltaCount+1)
	objects = append(objects, testPackObject{
		typ:     plumbing.BlobObject,
		content: []byte{0, 0},
	})
	for i := range deltaCount {
		content := []byte{byte(i + 1), byte((i + 1) >> 8)}
		delta := buildDelta(2, 2, insertOp(content))
		objects = append(objects, testPackObject{
			typ:                 plumbing.OFSDeltaObject,
			content:             delta,
			offsetDeltaDistance: -1,
		})
	}
	return buildTestPack(t, objects...)
}

type testPackObject struct {
	typ                 plumbing.ObjectType
	declaredSize        int64
	content             []byte
	reference           plumbing.Hash
	offsetDeltaDistance int64 // for OFS deltas; -1 means "previous object"
}

// buildTestPack assembles a pack with the given objects, returning the raw
// pack bytes. Object offsets are not exposed because the only caller does
// not need them.
func buildTestPack(t *testing.T, objects ...testPackObject) []byte {
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
			body.Write(obj.reference[:])
		case plumbing.OFSDeltaObject:
			distance := obj.offsetDeltaDistance
			if distance == -1 {
				// Reference the immediately preceding object.
				distance = offsets[len(offsets)-1] - offsets[len(offsets)-2]
			}
			require.NoError(t, gogitbinary.WriteVariableWidthInt(&body, distance))
		}
		body.Write(zlibCompress(t, obj.content))
	}

	sum := sha1.Sum(body.Bytes())
	body.Write(sum[:])
	return body.Bytes()
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
