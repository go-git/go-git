package packfile

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	gogitbinary "github.com/go-git/go-git/v6/utils/binary"
)

func FuzzParser(f *testing.F) {
	if pf, err := fixtures.Basic().One().Packfile(); err == nil {
		if data, rerr := io.ReadAll(pf); rerr == nil {
			f.Add(data)
		}
	}

	var overflow bytes.Buffer
	overflow.WriteString("PACK")
	_ = binary.Write(&overflow, binary.BigEndian, uint32(2))
	_ = binary.Write(&overflow, binary.BigEndian, uint32(1))
	overflow.WriteByte(0x90)
	overflow.Write(bytes.Repeat([]byte{0x80}, 9))
	sum := sha1.Sum(overflow.Bytes())
	overflow.Write(sum[:])
	f.Add(overflow.Bytes())

	// REF-delta whose base is an in-pack OFS-delta — exercises the
	// depth-first delta walk. Built as a 3-entry pack of the shape
	// [base blob, OFS-delta(base=blob), REF-delta(base=OFS-delta-hash)].
	// Inlined here because OSS-Fuzz's build strips non-Fuzz top-level
	// functions from this file before compiling.
	{
		base := []byte("a stable base payload used by the OFS-delta entry")
		mid := []byte("a stable base payload modified by the OFS-delta entry")
		leaf := []byte("a stable base payload modified twice for the REF-delta")

		midHasher := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, int64(len(mid)))
		_, _ = midHasher.Write(mid)
		midHash := midHasher.Sum()

		var pack bytes.Buffer
		h := sha1.New()
		w := io.MultiWriter(&pack, h)

		_, _ = w.Write([]byte("PACK"))
		_ = binary.Write(w, binary.BigEndian, uint32(2))
		_ = binary.Write(w, binary.BigEndian, uint32(3))

		writeHeader := func(typ plumbing.ObjectType, size int64) {
			c := byte((int64(typ) << firstLengthBits) | (size & int64(maskFirstLength)))
			size >>= firstLengthBits
			for size != 0 {
				_, _ = w.Write([]byte{c | maskContinue})
				c = byte(size & int64(maskLength))
				size >>= lengthBits
			}
			_, _ = w.Write([]byte{c})
		}
		writeZlib := func(payload []byte) {
			zw := zlib.NewWriter(w)
			_, _ = zw.Write(payload)
			_ = zw.Close()
		}

		obj1Offset := int64(pack.Len())
		writeHeader(plumbing.BlobObject, int64(len(base)))
		writeZlib(base)

		obj2Offset := int64(pack.Len())
		delta12 := DiffDelta(base, mid)
		writeHeader(plumbing.OFSDeltaObject, int64(len(delta12)))
		_ = gogitbinary.WriteVariableWidthInt(w, obj2Offset-obj1Offset)
		writeZlib(delta12)

		delta23 := DiffDelta(mid, leaf)
		writeHeader(plumbing.REFDeltaObject, int64(len(delta23)))
		_, _ = midHash.WriteTo(w)
		writeZlib(delta23)

		_, _ = pack.Write(h.Sum(nil))
		f.Add(pack.Bytes())
	}

	f.Fuzz(func(_ *testing.T, data []byte) {
		p := NewParser(bytes.NewReader(data))
		// context.Background instead of t.Context: OSS-Fuzz builds fuzz
		// targets with a shim testing.T that has no Context method.
		_, _ = p.Parse(context.Background())
	})
}
