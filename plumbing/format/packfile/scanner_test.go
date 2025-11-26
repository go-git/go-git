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
			name:         "ofs sha256",
			packfile:     fixtures.Basic().One().Packfile(),
			sha256:       true,
			want:         expectedHeadersOFS256,
			wantCrc:      expectedCRCOFS,
			wantChecksum: "a3fed42da1e8189a077c0e6846c040dcf73fc9dd",
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
					if tc.sha256 && !oo.Type.IsDelta() {
						assert.Equal(t, oo.Hash256.String(), oh.Hash256.String(), "hash mismatch index: %d", index)
					}
					assert.Equal(t, tc.wantCrc[index], oh.Crc32, "crc mismatch index: %d", index)
				case FooterSection:
					checksum := v.(plumbing.Hash)
					assert.Equal(t, tc.wantChecksum, checksum.String())
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
		scanner.Reset()

		for scanner.Scan() {
		}

		err := scanner.Error()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestPackHeaderSignature(t *testing.T) {
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

func ptr[T any](value T) *T {
	return &value
}

var expectedHeadersOFS256 = []ObjectHeader{
	{
		Type: plumbing.CommitObject, Offset: 12, Size: 254,
		Hash:    plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		Hash256: ptr(plumbing.NewHash("751ee7d8e2736460ea9b6f1b88aeb050dad7d7641b0313d27f0bb9bedd1b3726")),
	},
	{Type: plumbing.OFSDeltaObject, Offset: 186, Size: 93, OffsetReference: 12},
	{
		Type: plumbing.CommitObject, Offset: 286, Size: 242,
		Hash:    plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		Hash256: ptr(plumbing.NewHash("a279e860c7074462629fefb6a96e77eecb240eba291791c163581f6afeaa7f12")),
	},
	{
		Type: plumbing.CommitObject, Offset: 449, Size: 242,
		Hash:    plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		Hash256: ptr(plumbing.NewHash("aa68eba21ad1796f88c16e470e0374bf6ed1376495ab3a367cd85698c3df766f")),
	},
	{
		Type: plumbing.CommitObject, Offset: 615, Size: 333,
		Hash:    plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		Hash256: ptr(plumbing.NewHash("4d00acb62a3ecb5f3f6871aa29c8ea670fc3d27042842277280c6b3e48a206f1")),
	},
	{
		Type: plumbing.CommitObject, Offset: 838, Size: 332,
		Hash:    plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		Hash256: ptr(plumbing.NewHash("627852504dc677ba7ac2ec7717d69b42f787c8d79bac9fe1370b8775d2312e94")),
	},
	{
		Type: plumbing.CommitObject, Offset: 1063, Size: 244,
		Hash:    plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		Hash256: ptr(plumbing.NewHash("00f0a27f127cffbb2a1089b772edd3ba7c82a6b69d666048b75d4bdcee24515d")),
	},
	{
		Type: plumbing.CommitObject, Offset: 1230, Size: 243,
		Hash:    plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
		Hash256: ptr(plumbing.NewHash("ef5441299e83e8707722706fefd89e77290a2a6e84be5202b980128eaa6decc2")),
	},
	{
		Type: plumbing.CommitObject, Offset: 1392, Size: 187,
		Hash:    plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		Hash256: ptr(plumbing.NewHash("809c0681b603794597ef162c71184b38dda79364a423c6c61d2e514a1d46efff")),
	},
	{
		Type: plumbing.BlobObject, Offset: 1524, Size: 189,
		Hash:    plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"),
		Hash256: ptr(plumbing.NewHash("40b7c05726c9da78c3d5a705c2a48a120261b36f521302ce06bad41916d000f7")),
	},
	{
		Type: plumbing.BlobObject, Offset: 1685, Size: 18,
		Hash:    plumbing.NewHash("d3ff53e0564a9f87d8e84b6e28e5060e517008aa"),
		Hash256: ptr(plumbing.NewHash("e6ee53c7eb0e33417ee04110b84b304ff2da5c1b856f320b61ad9f2ef56c6e4e")),
	},
	{
		Type: plumbing.BlobObject, Offset: 1713, Size: 1072,
		Hash:    plumbing.NewHash("c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"),
		Hash256: ptr(plumbing.NewHash("789c9f4220d167b66020b46bacddcad0ab5bb12f0f469576aa60bb59d98293dc")),
	},
	{
		Type: plumbing.BlobObject, Offset: 2351, Size: 76110,
		Hash:    plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d"),
		Hash256: ptr(plumbing.NewHash("665e33431d9b88280d7c1837680fdb66664c4cb4b394c9057cdbd07f3b4acff8")),
	},
	{
		Type: plumbing.BlobObject, Offset: 78050, Size: 2780,
		Hash:    plumbing.NewHash("880cd14280f4b9b6ed3986d6671f907d7cc2a198"),
		Hash256: ptr(plumbing.NewHash("33a5013ed4af64b6e54076c986a4733c2c11ce8ab27ede79f21366e8722ac5ed")),
	},
	{
		Type: plumbing.BlobObject, Offset: 78882, Size: 217848,
		Hash:    plumbing.NewHash("49c6bb89b17060d7b4deacb7b338fcc6ea2352a9"),
		Hash256: ptr(plumbing.NewHash("4c61794e77ff8c7ab7f07404cdb1bc0e989b27530e37a6be6d2ef73639aaff6d")),
	},
	{
		Type: plumbing.BlobObject, Offset: 80725, Size: 706,
		Hash:    plumbing.NewHash("c8f1d8c61f9da76f4cb49fd86322b6e685dba956"),
		Hash256: ptr(plumbing.NewHash("2a246d3eaea67b7c4ac36d96d1dc9dad2a4dc24486c4d67eb7cb73963f522481")),
	},
	{
		Type: plumbing.BlobObject, Offset: 80998, Size: 11488,
		Hash:    plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492"),
		Hash256: ptr(plumbing.NewHash("73660d98a4c6c8951f86bb8c4744a0b4837a6dd5f796c314064c1615781c400c")),
	},
	{
		Type: plumbing.BlobObject, Offset: 84032, Size: 78,
		Hash:    plumbing.NewHash("9dea2395f5403188298c1dabe8bdafe562c491e3"),
		Hash256: ptr(plumbing.NewHash("2a7543a59f760f7ca41784bc898057799ae960323733cab1175c21960a750f72")),
	},
	{
		Type: plumbing.TreeObject, Offset: 84115, Size: 272,
		Hash:    plumbing.NewHash("dbd3641b371024f44d0e469a9c8f5457b0660de1"),
		Hash256: ptr(plumbing.NewHash("773b6c73238a74067c97f193c06c1bf38a982e39ded04fdf9c833ebc34cedd3d")),
	},
	{Type: plumbing.OFSDeltaObject, Offset: 84375, Size: 43, OffsetReference: 84115},
	{
		Type: plumbing.TreeObject, Offset: 84430, Size: 38,
		Hash:    plumbing.NewHash("a39771a7651f97faf5c72e08224d857fc35133db"),
		Hash256: ptr(plumbing.NewHash("166e4d7c5b5771422259dda0819ea54e06a6e4f07cf927d9fc95f5c370fff28a")),
	},
	{
		Type: plumbing.TreeObject, Offset: 84479, Size: 75,
		Hash:    plumbing.NewHash("5a877e6a906a2743ad6e45d99c1793642aaf8eda"),
		Hash256: ptr(plumbing.NewHash("393e771684c98451b904457acffac4ca5bd5a736a1b9127cedf7b8fa1b6a9901")),
	},
	{
		Type: plumbing.TreeObject, Offset: 84559, Size: 38,
		Hash:    plumbing.NewHash("586af567d0bb5e771e49bdd9434f5e0fb76d25fa"),
		Hash256: ptr(plumbing.NewHash("3db5b7f8353ebe6e4d4bff0bd2953952e08d73e72040abe4a46d08e7c3593dcc")),
	},
	{
		Type: plumbing.TreeObject, Offset: 84608, Size: 34,
		Hash:    plumbing.NewHash("cf4aa3b38974fb7d81f367c0830f7d78d65ab86b"),
		Hash256: ptr(plumbing.NewHash("e39c8c3d47aa310861634c6cf44e54e847c02f99c34c8cb25246e16f40502a7e")),
	},
	{
		Type: plumbing.BlobObject, Offset: 84653, Size: 9,
		Hash:    plumbing.NewHash("7e59600739c96546163833214c36459e324bad0a"),
		Hash256: ptr(plumbing.NewHash("1f307724f91af43be1570b77aeef69c5010e8136e50bef83c28de2918a08f494")),
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
