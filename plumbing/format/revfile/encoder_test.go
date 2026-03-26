package revfile

import (
	"bytes"
	"crypto"
	"errors"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

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
		writer io.Writer
		idx    idxfile.Index
		packCS plumbing.Hash // explicit packChecksum for Encode
		want   string
	}{
		{
			name:   "nil writer",
			writer: nil,
			idx:    idx,
			packCS: idx.PackfileChecksum,
			want:   "nil writer",
		},
		{
			name:   "nil index",
			writer: &bytes.Buffer{},
			idx:    nil,
			// packCS zero-valued: Encode rejects the nil index first.
			want: "nil index",
		},
		{
			name:   "valid encoding",
			writer: &bytes.Buffer{},
			idx:    idx,
			packCS: idx.PackfileChecksum,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := hash.New(crypto.SHA256)

			err := Encode(tc.writer, h, tc.idx, tc.packCS)
			if tc.want != "" {
				assert.EqualError(t, err, tc.want)
			} else {
				require.NoError(t, err)

				content, err := io.ReadAll(fixture.Rev())
				require.NoError(t, err)

				// Ensure the produced rev file is byte-identical to
				// the one in the fixture.
				assert.Equal(t, content, tc.writer.(*bytes.Buffer).Bytes())
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

			err = Encode(&buf, h, idx, idx.PackfileChecksum)
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
				if errors.Is(err, io.EOF) {
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
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				want = append(want, offsetToPos[entry.Offset])
			}
			entriesByOffset.Close()

			// Decode the generated rev file so that the entries can be checked.
			idxPos := make(chan uint32)
			got := []uint32{}

			errCh := make(chan error, 1)
			go func() {
				errCh <- Decode(&buf, count, idx.PackfileChecksum, idxPos)
			}()

			for p := range idxPos {
				got = append(got, p)
			}

			require.NoError(t, <-errCh)
			assert.Equal(t, want, got)
		})
	}
}

// TestEncodeTypedNilWriter documents that passing a typed-nil *bytes.Buffer
// (satisfying io.Writer but with a nil pointer) causes a panic. This is a
// known limitation — callers must ensure the writer is non-nil.
func TestEncodeTypedNilWriter(t *testing.T) {
	t.Parallel()

	fixture := fixtures.Basic().One()
	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA1.Size())
	idec := idxfile.NewDecoder(idxf, hash.New(crypto.SHA1))
	require.NoError(t, idec.Decode(idx))

	assert.Panics(t, func() {
		_ = Encode((*bytes.Buffer)(nil), hash.New(crypto.SHA1), idx, idx.PackfileChecksum)
	}, "typed-nil *bytes.Buffer writer should panic")
}
