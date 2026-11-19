package idxfile_test

import (
	"bytes"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

func TestEncode(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile").One()
	idxf, err := fixture.Idx()
	require.NoError(t, err)
	require.NotNil(t, idxf)

	expected, err := io.ReadAll(idxf)
	require.NoError(t, err)

	validIdxFn := func() *MemoryIndex {
		idx := NewMemoryIndex(hash.SHA1)
		d := NewDecoder(FromBytes(expected))
		err := d.Decode(idx)
		require.NoError(t, err)
		return idx
	}

	tests := []struct {
		name   string
		writer io.Writer
		idx    func() *MemoryIndex
		want   string
	}{
		{
			name:   "nil writer",
			writer: nil,
			idx:    validIdxFn,
			want:   "nil writer",
		},
		{
			name:   "nil index",
			writer: &bytes.Buffer{},
			idx:    func() *MemoryIndex { return nil },
			want:   "nil index",
		},
		{
			name:   "invalid fanout mapping",
			writer: &bytes.Buffer{},
			idx: func() *MemoryIndex {
				idx := validIdxFn()
				idx.FanoutMapping[3] = 6783216

				return idx
			},
			want: "malformed idx file: invalid position 6783216",
		},
		{
			name:   "invalid CRC32 position",
			writer: &bytes.Buffer{},
			idx: func() *MemoryIndex {
				idx := validIdxFn()
				idx.CRC32 = make([][]byte, 0)

				return idx
			},
			want: "malformed idx file: invalid CRC32 index 0",
		},
		{
			name:   "invalid offset position",
			writer: &bytes.Buffer{},
			idx: func() *MemoryIndex {
				idx := validIdxFn()
				idx.Offset32 = make([][]byte, 0)

				return idx
			},
			want: "malformed idx file: invalid offset32 index 0",
		},
		{
			name:   "unsupported version 3",
			writer: &bytes.Buffer{},
			idx: func() *MemoryIndex {
				idx := validIdxFn()
				idx.Version = 3

				return idx
			},
			want: "unsupported version",
		},
		{
			name:   "valid encoding",
			writer: &bytes.Buffer{},
			idx:    validIdxFn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := Encode(tc.writer, tc.idx())
			if tc.want != "" {
				assert.EqualError(t, err, tc.want)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture *fixtures.Fixture
		format  hash.ObjectFormat
	}{
		{
			name:    "sha256 packfile",
			fixture: fixtures.ByTag("packfile").ByObjectFormat("sha256").One(),
			format:  hash.SHA256,
		},
		{
			name:    "sha1 packfile",
			fixture: fixtures.Basic().ByObjectFormat("sha1").One(),
			format:  hash.SHA1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idxf, err := tc.fixture.Idx()
			require.NoError(t, err)
			require.NotNil(t, idxf)

			expected, err := io.ReadAll(idxf)
			require.NoError(t, err)

			idx := NewMemoryIndex(tc.format)
			d := NewDecoder(FromBytes(expected))
			err = d.Decode(idx)
			require.NoError(t, err)

			result := bytes.NewBuffer(nil)
			err = Encode(result, idx)
			require.NoError(t, err)

			assert.Len(t, expected, result.Len())
			assert.Equal(t, expected, result.Bytes())
		})
	}
}

func TestDecodeEncode(t *testing.T) {
	t.Parallel()
	for _, f := range fixtures.ByTag("packfile") {
		idxFile, err := f.Idx()
		require.NoError(t, err)
		expected, err := io.ReadAll(idxFile)
		require.NoError(t, err)

		format := hash.ObjectFormat(f.ObjectFormat)
		idx := NewMemoryIndex(format)
		d := NewDecoder(FromBytes(expected))
		err = d.Decode(idx)
		require.NoError(t, err)

		result := bytes.NewBuffer(nil)
		err = Encode(result, idx)
		require.NoError(t, err)

		assert.Len(t, expected, result.Len())
		assert.Equal(t, expected, result.Bytes())
	}
}
