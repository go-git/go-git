package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

func FuzzScanner(f *testing.F) {
	// Seed from a real packfile when available.
	if pf, err := fixtures.Basic().One().Packfile(); err == nil {
		if data, err := io.ReadAll(pf); err == nil {
			f.Add(data)
		}
	}

	// Minimal valid pack: PACK + version(2) + object count(0) + SHA1 trailer.
	var minimal bytes.Buffer
	minimal.WriteString("PACK")
	_ = binary.Write(&minimal, binary.BigEndian, uint32(2))
	_ = binary.Write(&minimal, binary.BigEndian, uint32(0))
	sum := sha1.Sum(minimal.Bytes())
	minimal.Write(sum[:])
	f.Add(minimal.Bytes())

	// Self-referencing OFS-delta whose encoded negative offset equals
	// its own pack offset; rejected per packfile.c:1289-1290.
	var ofsSelfRef bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&ofsSelfRef, h)
	_, _ = w.Write([]byte("PACK"))
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(1))
	_, _ = w.Write([]byte{byte(plumbing.OFSDeltaObject) << firstLengthBits})
	_, _ = w.Write([]byte{0x0C}) // negative offset = 12, the entry's own offset
	zw := zlib.NewWriter(w)
	_ = zw.Close()
	_, _ = ofsSelfRef.Write(h.Sum(nil))
	f.Add(ofsSelfRef.Bytes())

	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		s := NewScanner(bytes.NewReader(data))
		for s.Scan() {
			d := s.Data()
			_ = d.Section
			_ = d.Value()
		}
		_ = s.Error()
	})
}
