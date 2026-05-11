package packfile

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
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

func TestScannerRejectsReservedObjectType(t *testing.T) {
	t.Parallel()

	pack, _ := buildTestPack(t, testPackObject{
		typ:     plumbing.ObjectType(5),
		content: nil,
	})
	scanner := NewScanner(bytes.NewReader(pack))

	for scanner.Scan() {
	}

	require.ErrorIs(t, scanner.Error(), ErrMalformedPackfile)
	require.ErrorContains(t, scanner.Error(), "invalid object type")
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

// buildMinimalPack writes a single-object packfile to a buffer.
// The object header advertises declaredSize as the uncompressed length,
// but the zlib payload supplied by compressedBody may inflate to more.
// The SHA-1 footer is computed over all preceding bytes.
func buildMinimalPack(tb testing.TB, typ plumbing.ObjectType, declaredSize int64, compressedBody []byte) []byte {
	tb.Helper()

	var buf bytes.Buffer
	h := sha1.New()
	w := io.MultiWriter(&buf, h)

	// Pack header: magic, version=2, count=1.
	_, _ = w.Write([]byte{'P', 'A', 'C', 'K'})
	_ = binary.Write(w, binary.BigEndian, uint32(2))
	_ = binary.Write(w, binary.BigEndian, uint32(1))

	// Object header: variable-length encoding of (type, declaredSize).
	// First byte: high nibble = type, low nibble = size[3:0]; MSB set
	// if more size bytes follow. Subsequent bytes carry 7 bits each.
	t := int64(typ)
	first := byte((t << firstLengthBits) | (declaredSize & int64(maskFirstLength)))
	sz := declaredSize >> firstLengthBits
	var hdrBytes []byte
	for sz != 0 {
		hdrBytes = append(hdrBytes, first|maskContinue)
		first = byte(sz & int64(maskLength))
		sz >>= lengthBits
	}
	hdrBytes = append(hdrBytes, first)
	_, _ = w.Write(hdrBytes)

	// Compressed payload.
	_, _ = w.Write(compressedBody)

	// SHA-1 trailer (20 bytes).
	_, _ = buf.Write(h.Sum(nil))

	return buf.Bytes()
}

// TestInflateContentRejectsOversizedInflate verifies that inflateContent
// stops reading and returns ErrInflatedSizeMismatch when an inflate exceeds
// the bound passed in by the caller. The packfile is well-formed (declared
// size matches the inflated payload) so the forward scan succeeds; the
// rejection is forced by passing a smaller bound to inflateContent.
func TestInflateContentRejectsOversizedInflate(t *testing.T) {
	t.Parallel()

	const size = 1 << 20 // 1 MiB

	var rawBuf bytes.Buffer
	zw := zlib.NewWriter(&rawBuf)
	_, err := zw.Write(make([]byte, size))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	pack := buildMinimalPack(t, plumbing.BlobObject, size, rawBuf.Bytes())

	s := NewScanner(bytes.NewReader(pack))

	require.True(t, s.Scan(), "expected header section")
	require.True(t, s.Scan(), "expected object section")
	require.NoError(t, s.Error())

	oh := s.Data().Value().(ObjectHeader)
	require.Equal(t, plumbing.BlobObject, oh.Type)

	var sink bytes.Buffer
	// Force the bound to fire by declaring a much smaller cap to
	// inflateContent than the actual inflated payload size.
	inflateErr := s.inflateContent(oh.ContentOffset, &sink, 4096)

	assert.ErrorIs(t, inflateErr, ErrInflatedSizeMismatch,
		"expected ErrInflatedSizeMismatch from oversized inflate")
	assert.LessOrEqual(t, int64(sink.Len()), int64(4096),
		"inflated %d bytes but declared bound was %d", sink.Len(), 4096)
}

// TestObjectEntryRejectsOversizedInflate verifies that the forward-scan
// path (objectEntry) refuses to stream more inflated bytes into the hasher
// and storer than the object header's declared size, by failing Scan with
// ErrInflatedSizeMismatch.
func TestObjectEntryRejectsOversizedInflate(t *testing.T) {
	t.Parallel()

	const realSize = 1 << 20  // 1 MiB actual content
	const declaredSize = 4096 // 4 KiB declared in header — deliberately a lie

	var rawBuf bytes.Buffer
	zw := zlib.NewWriter(&rawBuf)
	_, err := zw.Write(make([]byte, realSize))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	pack := buildMinimalPack(t, plumbing.BlobObject, declaredSize, rawBuf.Bytes())

	s := NewScanner(bytes.NewReader(pack))

	require.True(t, s.Scan(), "expected header section")
	assert.False(t, s.Scan(), "expected object section to fail")
	assert.ErrorIs(t, s.Error(), ErrInflatedSizeMismatch,
		"expected ErrInflatedSizeMismatch from oversized inflate during scan")
}

// TestBoundedReadCloser exercises the contract used by FSObject.Reader and
// ondemandObject.Reader: reads up to limit pass through, the byte just past
// the limit surfaces ErrInflatedSizeMismatch, and exact-fit streams cleanly
// reach io.EOF.
func TestBoundedReadCloser(t *testing.T) {
	t.Parallel()

	t.Run("exact fit reads to EOF", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello")))
		b := NewBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("overrun surfaces ErrInflatedSizeMismatch", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello world")))
		b := NewBoundedReadCloser(rc, 5)
		got, err := io.ReadAll(b)
		assert.ErrorIs(t, err, ErrInflatedSizeMismatch)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("Close forwards to underlying", func(t *testing.T) {
		t.Parallel()

		var closed bool
		rc := readCloserFn{
			Reader: bytes.NewReader(nil),
			closeFn: func() error {
				closed = true
				return nil
			},
		}
		b := NewBoundedReadCloser(rc, 0)
		require.NoError(t, b.Close())
		assert.True(t, closed)
	})

	t.Run("negative limit is treated as zero", func(t *testing.T) {
		t.Parallel()

		rc := io.NopCloser(bytes.NewReader([]byte("hello")))
		b := NewBoundedReadCloser(rc, -1)
		got, err := io.ReadAll(b)
		assert.ErrorIs(t, err, ErrInflatedSizeMismatch)
		assert.Empty(t, got)
	})
}

// TestBoundedWriterOverrunJoinsWriteError pins the contract that a write
// error from the underlying writer surfaced while writing the legal prefix
// on overrun is joined with ErrInflatedSizeMismatch rather than silently
// dropped.
func TestBoundedWriterOverrunJoinsWriteError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("downstream write failure")
	bw := &boundedWriter{w: errWriter{err: wantErr}, limit: 4}

	n, err := bw.Write([]byte("hello"))
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, ErrInflatedSizeMismatch)
	assert.ErrorIs(t, err, wantErr)
}

type errWriter struct{ err error }

func (e errWriter) Write(_ []byte) (int, error) { return 0, e.err }

type readCloserFn struct {
	io.Reader
	closeFn func() error
}

func (r readCloserFn) Close() error { return r.closeFn() }
