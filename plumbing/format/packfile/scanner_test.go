package packfile

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/fixtureutil"
	"github.com/go-git/go-git/v6/plumbing"
)

func TestScan(t *testing.T) {
	t.Parallel()

	packs := fixtures.ByTag("scanner-entries")
	require.GreaterOrEqual(t, len(packs), 2)

	packs.Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		entries := fixtureutil.ScannerEntries(f)
		require.NotEmpty(t, entries)

		var opts []ScannerOption
		if f.ObjectFormat == "sha256" {
			opts = append(opts, WithSHA256())
		}

		s := NewScanner(mustPackfile(t, f), opts...)
		i := 0

		for s.Scan() {
			data := s.Data()
			v := data.Value()

			switch data.Section {
			case HeaderSection:
				gotHeader := v.(Header)
				assert.Equal(t, 0, i, "wrong index")
				assert.Equal(t, Version(2), gotHeader.Version)
				assert.Equal(t, uint32(len(entries)), gotHeader.ObjectsQty)
			case ObjectSection:
				index := i - 1
				oo := entries[index]

				oh := v.(ObjectHeader)
				assert.Equal(t, oo.Type, oh.Type, "type mismatch index: %d", index)
				assert.Equal(t, oo.Offset, oh.Offset, "offset mismatch index: %d", index)
				assert.Equal(t, oo.Size, oh.Size, "size mismatch index: %d", index)
				assert.Equal(t, oo.Reference, oh.Reference, "reference mismatch index: %d", index)
				assert.Equal(t, oo.OffsetReference, oh.OffsetReference, "offset reference mismatch index: %d", index)
				if oo.Type != plumbing.OFSDeltaObject && oo.Type != plumbing.REFDeltaObject {
					assert.Equal(t, oo.Hash.String(), oh.Hash.String(), "hash mismatch index: %d", index)
				}
				assert.Equal(t, oo.CRC32, oh.Crc32, "crc mismatch index: %d", index)
			case FooterSection:
				checksum := v.(plumbing.Hash)
				assert.Equal(t, f.PackfileHash, checksum.String(), "pack hash mismatch")
			}
			i++
		}

		assert.NoError(t, s.Error())
		assert.Equal(t, len(entries)+2, i)
	})
}

func BenchmarkScannerBasic(b *testing.B) {
	f := mustPackfile(b, fixtures.Basic().One())
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
			wantErr: ErrMalformedPackfile,
		},
		{
			name: "empty packfile: ErrEmptyPackfile",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: ErrEmptyPackfile,
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
			wantErr: io.EOF,
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
			name: "empty packfile: EOF",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: io.EOF,
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
			wantErr: io.EOF,
		},
		{
			name: "empty packfile: EOF",
			scanner: &Scanner{
				scannerReader: newScannerReader(bytes.NewReader(nil), nil, nil),
			},
			wantErr: io.EOF,
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

func mustPackfile(tb testing.TB, f *fixtures.Fixture) billy.File {
	tb.Helper()
	pf, err := f.Packfile()
	if err != nil {
		tb.Fatal(err)
	}
	return pf
}
