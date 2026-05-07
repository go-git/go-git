package packfile_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
)

func FuzzParser(f *testing.F) {
	if pf := fixtures.Basic().One().Packfile(); pf != nil {
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

	f.Fuzz(func(_ *testing.T, data []byte) {
		scanner := packfile.NewScanner(bytes.NewReader(data))
		parser, err := packfile.NewParser(scanner)
		if err != nil {
			return
		}
		_, _ = parser.Parse()
	})
}
