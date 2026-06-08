package revfile

import (
	"bytes"
	"crypto"
	"errors"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

// errorAfterReader wraps a reader and returns an error after n bytes have been read.
type errorAfterReader struct {
	r         io.Reader
	bytesLeft int
	err       error
}

func (e *errorAfterReader) Read(p []byte) (int, error) {
	if e.bytesLeft <= 0 {
		return 0, e.err
	}
	if len(p) > e.bytesLeft {
		p = p[:e.bytesLeft]
	}
	n, err := e.r.Read(p)
	e.bytesLeft -= n
	if err != nil {
		return n, err
	}
	if e.bytesLeft <= 0 {
		return n, e.err
	}
	return n, nil
}

func mustRev(t testing.TB, f *fixtures.Fixture) io.Reader {
	t.Helper()
	r, err := f.Rev()
	require.NoError(t, err)
	return r
}

func TestDecodeSHA256(t *testing.T) {
	t.Parallel()
	fixture := fixtures.ByTag("packfile").ByObjectFormat("sha256").One()
	revf, err := fixture.Rev()
	require.NoError(t, err)
	require.NotNil(t, revf)

	idxf, err := fixture.Idx()
	require.NoError(t, err)
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf, hash.New(crypto.SHA256))
	err = idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)

	idxPos := make(chan uint32)

	want := []uint32{2, 0, 3, 4, 5, 1}
	got := []uint32{}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Decode(revf, count, idx.PackfileChecksum, idxPos)
	}()

	for pos := range idxPos {
		got = append(got, pos)
	}

	require.NoError(t, <-errCh)
	assert.Equal(t, want, got)
}

func TestDecode(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile").ByObjectFormat("sha256").One()

	allPositions := []uint32{2, 0, 3, 4, 5, 1}

	tests := []struct {
		name          string
		revFile       io.Reader
		objCount      int64
		packChecksum  plumbing.ObjectID
		ch            chan uint32
		wantErr       string
		wantPositions []uint32
	}{
		{
			name:    "nil rev file",
			wantErr: "malformed rev file: nil reader",
		},
		{
			name:     "nil chan",
			revFile:  mustRev(t, fixture),
			objCount: 6,
			wantErr:  "nil channel",
		},
		{
			name:          "shorter obj count",
			revFile:       mustRev(t, fixture),
			objCount:      5,
			packChecksum:  plumbing.NewHash("00000001407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8"),
			ch:            make(chan uint32),
			wantErr:       "malformed rev file: rev file checksum mismatch wanted \"c5d5a04b0e120302b2defa9ad5192aa0f027acbc840c6ba7273e34d0d02cbfcd\" got \"f55d0ee2392bf9821bce02a75a7657e19b11b9a12ac8c96cdd5ad182bcc16528\"",
			wantPositions: allPositions[:5],
		},
		{
			name:         "longer obj count",
			revFile:      mustRev(t, fixture),
			objCount:     50,
			packChecksum: plumbing.NewHash("00"),
			ch:           make(chan uint32),
			wantErr:      "malformed rev file: unexpected EOF at object 22",
		},
		{
			name:          "wrong pack checksum",
			revFile:       mustRev(t, fixture),
			objCount:      6,
			packChecksum:  plumbing.NewHash("aa7497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2"),
			ch:            make(chan uint32),
			wantErr:       "malformed rev file: packfile hash mismatch wanted \"aa7497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2\" got \"407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2\"",
			wantPositions: allPositions,
		},
		{
			name: "longer rev file",
			revFile: io.MultiReader(
				mustRev(t, fixture),
				bytes.NewReader([]byte{0xFF}),
			),
			objCount:      6,
			packChecksum:  plumbing.NewHash("407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2"),
			ch:            make(chan uint32),
			wantErr:       "malformed rev file: expected EOF",
			wantPositions: allPositions,
		},
		{
			name: "read error at EOF check",
			revFile: &errorAfterReader{
				r:         mustRev(t, fixture),
				bytesLeft: 100, // rev file is 100 bytes (header + entries + checksums)
				err:       errors.New("network error"),
			},
			objCount:      6,
			packChecksum:  plumbing.NewHash("407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2"),
			ch:            make(chan uint32),
			wantErr:       "network error",
			wantPositions: allPositions,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errCh := make(chan error, 1)

			go func() {
				errCh <- Decode(tc.revFile, tc.objCount, tc.packChecksum, tc.ch)
			}()

			var got []uint32
			if tc.ch != nil {
				for pos := range tc.ch {
					got = append(got, pos)
				}
			}

			err := <-errCh
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}

			if tc.wantPositions != nil {
				assert.Equal(t, tc.wantPositions, got)
			}
		})
	}
}
