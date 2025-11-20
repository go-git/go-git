package revfile

import (
	"bufio"
	"bytes"
	"crypto"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	plumbinghash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()
	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	tests := []struct {
		name   string
		writer io.Writer
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
			h := plumbinghash.New(crypto.SHA256)
			enc := NewEncoder(tc.writer, h)

			err := enc.Encode(tc.idx)
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
			idxf := tc.fixture.Idx()
			require.NotNil(t, idxf)

			idx := idxfile.NewMemoryIndex(tc.hasher.Size())
			idec := idxfile.NewDecoder(idxf)
			err := idec.Decode(idx)
			require.NoError(t, err)

			var buf bytes.Buffer
			h := plumbinghash.New(tc.hasher)
			enc := NewEncoder(&buf, h)

			err = enc.Encode(idx)
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

			// Decode the generated rev file so that the entries can be checked.
			d := NewDecoder(bufio.NewReader(&buf), count, idx.PackfileChecksum)

			idxPos := make(chan uint32)
			got := []uint32{}

			go func() {
				err = d.Decode(idxPos)
			}()

			for p := range idxPos {
				got = append(got, p)
			}

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}
