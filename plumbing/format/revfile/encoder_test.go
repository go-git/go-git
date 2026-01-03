package revfile

import (
	"bytes"
	"crypto"
	"io"
	"io/fs"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

// encoderMockRevFile wraps a bytes.Reader to satisfy the RevFile interface for testing.
type encoderMockRevFile struct {
	*bytes.Reader
	size int64
}

func newEncoderMockRevFile(data []byte) *encoderMockRevFile {
	return &encoderMockRevFile{
		Reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}
}

func (m *encoderMockRevFile) Stat() (fs.FileInfo, error) {
	return &encoderMockFileInfo{size: m.size}, nil
}

func (m *encoderMockRevFile) Close() error {
	return nil
}

type encoderMockFileInfo struct {
	size int64
}

func (m *encoderMockFileInfo) Name() string       { return "test.rev" }
func (m *encoderMockFileInfo) Size() int64        { return m.size }
func (m *encoderMockFileInfo) Mode() fs.FileMode  { return 0o644 }
func (m *encoderMockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *encoderMockFileInfo) IsDir() bool        { return false }
func (m *encoderMockFileInfo) Sys() any           { return nil }

func TestEncode(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf, hash.New(crypto.SHA256))
	err := idec.Decode(idx)
	require.NoError(t, err)

	tests := []struct {
		name   string
		writer *bytes.Buffer
		idx    *idxfile.MemoryIndex
		want   string
	}{
		{
			name:   "nil writer",
			writer: nil,
			idx:    idx,
			want:   "nil writer",
		},
		{
			name:   "nil index",
			writer: &bytes.Buffer{},
			idx:    nil,
			want:   "nil index",
		},
		{
			name:   "valid encoding",
			writer: &bytes.Buffer{},
			idx:    idx,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := hash.New(crypto.SHA256)

			err := Encode(tc.writer, h, tc.idx)
			if tc.want != "" {
				assert.EqualError(t, err, tc.want)
			} else {
				require.NoError(t, err)

				content, err := io.ReadAll(fixture.Rev())
				require.NoError(t, err)

				// Ensure the produced rev file is byte-identical to
				// the one in the fixture.
				assert.Equal(t, content, tc.writer.Bytes())
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture *fixtures.Fixture
		hasher  crypto.Hash
	}{
		{
			name:    "sha256 packfile",
			fixture: fixtures.ByTag("packfile-sha256").One(),
			hasher:  crypto.SHA256,
		},
		{
			name:    "basic packfile",
			fixture: fixtures.Basic().One(),
			hasher:  crypto.SHA1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			idxf := tc.fixture.Idx()
			require.NotNil(t, idxf)

			idx := idxfile.NewMemoryIndex(tc.hasher.Size())
			idec := idxfile.NewDecoder(idxf, hash.New(tc.hasher))
			err := idec.Decode(idx)
			require.NoError(t, err)

			var buf bytes.Buffer
			h := hash.New(tc.hasher)

			err = Encode(&buf, h, idx)
			require.NoError(t, err)

			// Form expected entries based on the index so that they can
			// be cross-related to the generated rev file.
			count, err := idx.Count()
			require.NoError(t, err)

			offsetToPos := make(map[uint64]uint32)
			idxEntries, err := idx.Entries()
			require.NoError(t, err)

			var pos uint32
			for {
				entry, err := idxEntries.Next()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				offsetToPos[entry.Offset] = pos
				pos++
			}
			idxEntries.Close()

			entriesByOffset, err := idx.EntriesByOffset()
			require.NoError(t, err)

			var want []uint32
			for {
				entry, err := entriesByOffset.Next()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				want = append(want, offsetToPos[entry.Offset])
			}
			entriesByOffset.Close()

			// Use ReaderAtRevIndex to verify the generated rev file.
			revIdx, err := NewReaderAtRevIndex(newEncoderMockRevFile(buf.Bytes()), tc.hasher.Size(), count)
			require.NoError(t, err)
			defer revIdx.Close()

			var got []uint32
			all, finish := revIdx.All()
			for _, idxPos := range all {
				got = append(got, uint32(idxPos))
			}
			assert.NoError(t, finish())
			assert.Equal(t, want, got)
		})
	}
}
