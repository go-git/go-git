package packfile

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestScan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		packfile     billy.File
		sha256       bool
		want         []ObjectHeader
		wantCrc      []uint32
		wantChecksum string
	}{
		{
			name:         "ofs",
			packfile:     fixtures.Basic().One().Packfile(),
			want:         expectedHeadersOFS256,
			wantCrc:      expectedCRCOFS,
			wantChecksum: "a3fed42da1e8189a077c0e6846c040dcf73fc9dd",
		},
		{
			name:     "ofs sha256",
			packfile: fixtures.ByTag("packfile-sha256").One().Packfile(),
			sha256:   true,
			want: []ObjectHeader{
				{Hash: plumbing.NewHash("233fbe36fbc685c391d6e48049c1e6558a6742dba527281d02896bcba43a8950"), Offset: 12, Size: 685, Type: plumbing.CommitObject},
				{Hash: plumbing.NewHash("0000000000000000000000000000000000000000000000000000000000000000"), Offset: 459, Size: 227, Type: plumbing.OFSDeltaObject, OffsetReference: 12},
				{Hash: plumbing.NewHash("757ba6c738cdd774ea77094c52350acb8de989889a63f90972702ff6c5df69d4"), Offset: 687, Size: 47, Type: plumbing.BlobObject},
				{Hash: plumbing.NewHash("a3490718a0b0e8564981306fcfb3c8e5e5b8dd4c00d477d635350c92c542e15c"), Offset: 737, Size: 49, Type: plumbing.TreeObject},
				{Hash: plumbing.NewHash("fc90aec557362385e83d1f2046e2f8c2d52fdaeb5ba570a5f82b403e12340370"), Offset: 797, Size: 49, Type: plumbing.TreeObject},
				{Hash: plumbing.NewHash("1f307724f91af43be1570b77aeef69c5010e8136e50bef83c28de2918a08f494"), Offset: 857, Size: 9, Type: plumbing.BlobObject},
			},
			wantCrc:      []uint32{0x6f83ea11, 0x83e66670, 0xd3753b86, 0x69640927, 0xe11ef7d6, 0xcd987848},
			wantChecksum: "407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2",
		},
		{
			name:         "refs",
			packfile:     fixtures.Basic().ByTag("ref-delta").One().Packfile(),
			want:         expectedHeadersREF,
			wantCrc:      expectedCRCREF,
			wantChecksum: "c544593473465e6315ad4182d04d366c4592b829",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var opts []ScannerOption

			if tc.sha256 {
				opts = append(opts, WithSHA256())
			}

			s := NewScanner(tc.packfile, opts...)
			i := 0

			for s.Scan() {
				data := s.Data()
				v := data.Value()

				switch data.Section {
				case HeaderSection:
					gotHeader := v.(Header)
					assert.Equal(t, 0, i, "wrong index")
					assert.Equal(t, Version(2), gotHeader.Version)
					assert.Equal(t, uint32(len(tc.want)), gotHeader.ObjectsQty)
				case ObjectSection:
					index := i - 1

					oh := v.(ObjectHeader)
					oo := tc.want[index]
					assert.Equal(t, oo.Type, oh.Type, "type mismatch index: %d", index)
					assert.Equal(t, oo.Offset, oh.Offset, "offset mismatch index: %d", index)
					assert.Equal(t, oo.Size, oh.Size, "size mismatch index: %d", index)
					assert.Equal(t, oo.Reference, oh.Reference, "reference mismatch index: %d", index)
					assert.Equal(t, oo.OffsetReference, oh.OffsetReference, "offset reference mismatch index: %d", index)
					assert.Equal(t, oo.Hash.String(), oh.Hash.String(), "hash mismatch index: %d", index)
					assert.Equal(t, tc.wantCrc[index], oh.Crc32, "crc mismatch index: %d", index)
				case FooterSection:
					checksum := v.(plumbing.Hash)
					assert.Equal(t, tc.wantChecksum, checksum.String(), "pack hash mismatch")
				}
				i++
			}

			err := s.Error()
			assert.NoError(t, err)

			// wanted objects + header + footer
			assert.Equal(t, len(tc.want)+2, i)
		})
	}
}

func BenchmarkScannerBasic(b *testing.B) {
	f := fixtures.Basic().One().Packfile()
	scanner := NewScanner(f)
	for b.Loop() {
		if err := scanner.Reset(); err != nil {
			b.Fatal(err)
		}

		for scanner.Scan() {
		}

		err := scanner.Error()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestPackHeaderSignature(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		scanner   *Scanner
		nextState stateFn
		wantErr   error
	}{
		{
			name: "valid signature",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader([]byte("PACK")), nil, nil),
			},
			nextState: packVersion,
		},
		{
			name: "invalid signature",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader([]byte("FOOBAR")), nil, nil),
			},
			wantErr: ErrBadSignature,
		},
		{
			name: "invalid signature - too small",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader([]byte("FOO")), nil, nil),
			},
			wantErr: ErrBadSignature,
		},
		{
			name: "empty packfile: io.EOF",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: io.EOF,
		},
		{
			name: "empty packfile: ErrBadSignature",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: ErrBadSignature,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next, err := packHeaderSignature(tc.scanner)

			if tc.wantErr == nil {
				assert.Equal(t,
					runtime.FuncForPC(reflect.ValueOf(tc.nextState).Pointer()).Name(),
					runtime.FuncForPC(reflect.ValueOf(next).Pointer()).Name())

				assert.NoError(t, err)
			} else {
				assert.Nil(t, next)
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestPackVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		scanner   *Scanner
		version   Version
		nextState stateFn
		wantErr   error
	}{
		{
			name:    "Version 2",
			version: Version(2),
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 4))
					binary.Write(buf, binary.BigEndian, uint32(2))
					return newScannerReader(buf, nil, nil)
				}(),
			},
			nextState: packObjectsQty,
		},
		{
			name: "Version -1",
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 4))
					binary.Write(buf, binary.BigEndian, -1) //nolint:staticcheck // intentionally testing invalid input
					return newScannerReader(buf, nil, nil)
				}(),
			},
			wantErr: ErrMalformedPackfile,
		},
		{
			name: "Unsupported version",
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 4))
					binary.Write(buf, binary.BigEndian, uint32(3))
					return newScannerReader(buf, nil, nil)
				}(),
			},
			wantErr: ErrUnsupportedVersion,
		},
		{
			name: "empty packfile: ErrMalformedPackfile",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: ErrMalformedPackfile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next, err := packVersion(tc.scanner)

			if tc.wantErr == nil {
				assert.Equal(t,
					runtime.FuncForPC(reflect.ValueOf(tc.nextState).Pointer()).Name(),
					runtime.FuncForPC(reflect.ValueOf(next).Pointer()).Name())

				assert.Equal(t, tc.version, tc.scanner.version)
				assert.NoError(t, err)
			} else {
				assert.Nil(t, next)
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestPackObjectQty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		scanner   *Scanner
		objects   uint32
		nextState stateFn
		wantErr   error
	}{
		{
			name: "Zero",
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 4))
					binary.Write(buf, binary.BigEndian, uint32(0))
					return newScannerReader(buf, nil, nil)
				}(),
			},
			nextState: packFooter, // if there are no objects, skip to footer.
		},
		{
			name: "Valid number",
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 4))
					binary.Write(buf, binary.BigEndian, uint32(7))
					return newScannerReader(buf, nil, nil)
				}(),
			},
			objects:   7,
			nextState: nil,
		},
		{
			name: "less than 2 bytes on source",
			scanner: &Scanner{
				scannerReader: func() *scannerReader {
					buf := bytes.NewBuffer(make([]byte, 0, 2))
					return newScannerReader(buf, nil, nil)
				}(),
			},
			wantErr: ErrMalformedPackfile,
		},
		{
			name: "empty packfile: ErrMalformedPackfile",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: ErrMalformedPackfile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next, err := packObjectsQty(tc.scanner)

			if tc.wantErr == nil {
				assert.Equal(t,
					runtime.FuncForPC(reflect.ValueOf(tc.nextState).Pointer()).Name(),
					runtime.FuncForPC(reflect.ValueOf(next).Pointer()).Name())

				assert.Equal(t, tc.objects, tc.scanner.objects)
				assert.NoError(t, err)
			} else {
				assert.Nil(t, next)
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

var expectedHeadersOFS256 = []ObjectHeader{
	{
		Type: plumbing.CommitObject, Offset: 12, Size: 254,
		Hash: plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	},
	{Type: plumbing.OFSDeltaObject, Offset: 186, Size: 93, OffsetReference: 12},
	{
		Type: plumbing.CommitObject, Offset: 286, Size: 242,
		Hash: plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	},
	{
		Type: plumbing.CommitObject, Offset: 449, Size: 242,
		Hash: plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
	},
	{
		Type: plumbing.CommitObject, Offset: 615, Size: 333,
		Hash: plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
	},
	{
		Type: plumbing.CommitObject, Offset: 838, Size: 332,
		Hash: plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
	},
	{
		Type: plumbing.CommitObject, Offset: 1063, Size: 244,
		Hash: plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	},
	{
		Type: plumbing.CommitObject, Offset: 1230, Size: 243,
		Hash: plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	},
	{
		Type: plumbing.CommitObject, Offset: 1392, Size: 187,
		Hash: plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	},
	{
		Type: plumbing.BlobObject, Offset: 1524, Size: 189,
		Hash: plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"),
	},
	{
		Type: plumbing.BlobObject, Offset: 1685, Size: 18,
		Hash: plumbing.NewHash("d3ff53e0564a9f87d8e84b6e28e5060e517008aa"),
	},
	{
		Type: plumbing.BlobObject, Offset: 1713, Size: 1072,
		Hash: plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"),
	},
	{
		Type: plumbing.BlobObject, Offset: 2351, Size: 76110,
		Hash: plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d"),
	},
	{
		Type: plumbing.BlobObject, Offset: 78050, Size: 2780,
		Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"),
	},
	{
		Type: plumbing.BlobObject, Offset: 78882, Size: 217848,
		Hash: plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9"),
	},
	{
		Type: plumbing.BlobObject, Offset: 80725, Size: 706,
		Hash: plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956"),
	},
	{
		Type: plumbing.BlobObject, Offset: 80998, Size: 11488,
		Hash: plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"),
	},
	{
		Type: plumbing.BlobObject, Offset: 84032, Size: 78,
		Hash: plumbing.NewHash("9dea2395f5403188298c1dabe8bdafe562c491e3"),
	},
	{
		Type: plumbing.TreeObject, Offset: 84115, Size: 272,
		Hash: plumbing.NewHash("dbd3641b371024f44d0e469a9c8f5457b0660de1"),
	},
	{Type: plumbing.OFSDeltaObject, Offset: 84375, Size: 43, OffsetReference: 84115},
	{
		Type: plumbing.TreeObject, Offset: 84430, Size: 38,
		Hash: plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db"),
	},
	{
		Type: plumbing.TreeObject, Offset: 84479, Size: 75,
		Hash: plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda"),
	},
	{
		Type: plumbing.TreeObject, Offset: 84559, Size: 38,
		Hash: plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa"),
	},
	{
		Type: plumbing.TreeObject, Offset: 84608, Size: 34,
		Hash: plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b"),
	},
	{
		Type: plumbing.BlobObject, Offset: 84653, Size: 9,
		Hash: plumbing.NewHash("7e59600739c96546163833214c36459e324bad0a"),
	},
	{Type: plumbing.OFSDeltaObject, Offset: 84671, Size: 6, OffsetReference: 84375},
	{Type: plumbing.OFSDeltaObject, Offset: 84688, Size: 9, OffsetReference: 84375},
	{Type: plumbing.OFSDeltaObject, Offset: 84708, Size: 6, OffsetReference: 84375},
	{Type: plumbing.OFSDeltaObject, Offset: 84725, Size: 5, OffsetReference: 84115},
	{Type: plumbing.OFSDeltaObject, Offset: 84741, Size: 8, OffsetReference: 84375},
	{Type: plumbing.OFSDeltaObject, Offset: 84760, Size: 4, OffsetReference: 84741},
}

var expectedCRCOFS = []uint32{
	0xaa07ba4b,
	0xf706df58,
	0x12438846,
	0x2905a38c,
	0xd9429436,
	0xbecfde4e,
	0x780e4b3e,
	0xdc18344f,
	0xcf4e4280,
	0x1f08118a,
	0xafded7b8,
	0xcc1428ed,
	0x1631d22f,
	0xbfff5850,
	0xd108e1d8,
	0x8e97ba25,
	0x7316ff70,
	0xdb4fce56,
	0x901cce2c,
	0xec4552b0,
	0x847905bf,
	0x3689459a,
	0xe67af94a,
	0xc2314a2e,
	0xcd987848,
	0x8a853a6d,
	0x70c6518,
	0x4f4108e2,
	0xd6fe09e9,
	0xf07a2804,
	0x1d75d6be,
}

var expectedHeadersREF = []ObjectHeader{
	{Type: plumbing.CommitObject, Offset: 12, Size: 254, Hash: plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")},
	{
		Type: plumbing.REFDeltaObject, Offset: 186, Size: 93,
		Reference: plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	},
	{Type: plumbing.CommitObject, Offset: 304, Size: 242, Hash: plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294")},
	{Type: plumbing.CommitObject, Offset: 467, Size: 242, Hash: plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")},
	{Type: plumbing.CommitObject, Offset: 633, Size: 333, Hash: plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")},
	{Type: plumbing.CommitObject, Offset: 856, Size: 332, Hash: plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69")},
	{Type: plumbing.CommitObject, Offset: 1081, Size: 243, Hash: plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")},
	{Type: plumbing.CommitObject, Offset: 1243, Size: 244, Hash: plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9")},
	{Type: plumbing.CommitObject, Offset: 1410, Size: 187, Hash: plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")},
	{Type: plumbing.BlobObject, Offset: 1542, Size: 189, Hash: plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")},
	{Type: plumbing.BlobObject, Offset: 1703, Size: 18, Hash: plumbing.NewHash("d3ff53e0564a9f87d8e84b6e28e5060e517008aa")},
	{Type: plumbing.BlobObject, Offset: 1731, Size: 1072, Hash: plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f")},
	{Type: plumbing.BlobObject, Offset: 2369, Size: 76110, Hash: plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d")},
	{Type: plumbing.TreeObject, Offset: 78068, Size: 38, Hash: plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db")},
	{Type: plumbing.BlobObject, Offset: 78117, Size: 2780, Hash: plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198")},
	{Type: plumbing.TreeObject, Offset: 79049, Size: 75, Hash: plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda")},
	{Type: plumbing.BlobObject, Offset: 79129, Size: 217848, Hash: plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9")},
	{Type: plumbing.BlobObject, Offset: 80972, Size: 706, Hash: plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956")},
	{Type: plumbing.TreeObject, Offset: 81265, Size: 38, Hash: plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa")},
	{Type: plumbing.BlobObject, Offset: 81314, Size: 11488, Hash: plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")},
	{Type: plumbing.TreeObject, Offset: 84752, Size: 34, Hash: plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b")},
	{Type: plumbing.BlobObject, Offset: 84797, Size: 78, Hash: plumbing.NewHash("9dea2395f5403188298c1dabe8bdafe562c491e3")},
	{Type: plumbing.TreeObject, Offset: 84880, Size: 271, Hash: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")},
	{
		Type: plumbing.REFDeltaObject, Offset: 85141, Size: 6,
		Reference: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"),
	},
	{
		Type: plumbing.REFDeltaObject, Offset: 85176, Size: 37,
		Reference: plumbing.NewHash("fb72698cab7617ac416264415f13224dfd7a165e"),
	},
	{Type: plumbing.BlobObject, Offset: 85244, Size: 9, Hash: plumbing.NewHash("7e59600739c96546163833214c36459e324bad0a")},
	{
		Type: plumbing.REFDeltaObject, Offset: 85262, Size: 9,
		Reference: plumbing.NewHash("fb72698cab7617ac416264415f13224dfd7a165e"),
	},
	{
		Type: plumbing.REFDeltaObject, Offset: 85300, Size: 6,
		Reference: plumbing.NewHash("fb72698cab7617ac416264415f13224dfd7a165e"),
	},
	{Type: plumbing.TreeObject, Offset: 85335, Size: 110, Hash: plumbing.NewHash("c2d30fa8ef288618f65f6eed6e168e0d514886f4")},
	{
		Type: plumbing.REFDeltaObject, Offset: 85448, Size: 8,
		Reference: plumbing.NewHash("eba74343e2f15d62adedfd8c883ee0262b5c8021"),
	},
	{Type: plumbing.TreeObject, Offset: 85485, Size: 73, Hash: plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725")},
}

var expectedCRCREF = []uint32{
	0xaa07ba4b,
	0xfb4725a4,
	0x12438846,
	0x2905a38c,
	0xd9429436,
	0xbecfde4e,
	0xdc18344f,
	0x780e4b3e,
	0xcf4e4280,
	0x1f08118a,
	0xafded7b8,
	0xcc1428ed,
	0x1631d22f,
	0x847905bf,
	0x3e20f31d,
	0x3689459a,
	0xd108e1d8,
	0x71143d4a,
	0xe67af94a,
	0x739fb89f,
	0xc2314a2e,
	0x87864926,
	0x415d752f,
	0xf72fb182,
	0x3ffa37d4,
	0xcd987848,
	0x2f20ac8f,
	0xf2f0575,
	0x7d8726e1,
	0x740bf39,
	0x26af4735,
}
