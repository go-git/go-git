package revfile

import (
	"bufio"
	"bytes"
	"crypto"
	"io"
	"sync"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeSHA256(t *testing.T) {
	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	count, err := idx.Count()
	require.NoError(t, err)
	d := NewDecoder(bufio.NewReader(revf), count, idx.PackfileChecksum)

	idxPos := make(chan uint32)

	want := []uint32{2, 0, 3, 4, 5, 1}
	got := []uint32{}

	go func() {
		err = d.Decode(idxPos)
	}()

	for pos := range idxPos {
		got = append(got, pos)
	}

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestDecode(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile-sha256").One()

	tests := []struct {
		name         string
		revFile      *bufio.Reader
		objCount     int64
		packChecksum plumbing.ObjectID
		ch           chan uint32
		want         string
	}{
		{
			name: "nil rev file",
			want: "malformed rev file: nil reader",
		},
		{
			name:     "nil chan",
			revFile:  bufio.NewReader(fixture.Rev()),
			objCount: 6,
			want:     "nil channel",
		},
		{
			name:     "closed chan",
			revFile:  bufio.NewReader(fixture.Rev()),
			objCount: 6,
			ch: func() chan uint32 {
				ch := make(chan uint32)
				close(ch)
				return ch
			}(),
			want: "close of closed channel",
		},
		{
			name:         "shorter obj count",
			revFile:      bufio.NewReader(fixture.Rev()),
			objCount:     5,
			packChecksum: plumbing.NewHash("00000001407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8"),
			ch:           make(chan uint32),
			want:         "malformed rev file: rev file checksum mismatch wanted \"c5d5a04b0e120302b2defa9ad5192aa0f027acbc840c6ba7273e34d0d02cbfcd\" got \"f55d0ee2392bf9821bce02a75a7657e19b11b9a12ac8c96cdd5ad182bcc16528\"",
		},
		{
			name:         "longer obj count",
			revFile:      bufio.NewReader(fixture.Rev()),
			objCount:     50,
			packChecksum: plumbing.NewHash("00"),
			ch:           make(chan uint32),
			want:         "EOF",
		},
		{
			name:         "wrong pack checksum",
			revFile:      bufio.NewReader(fixture.Rev()),
			objCount:     6,
			packChecksum: plumbing.NewHash("aa7497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2"),
			ch:           make(chan uint32),
			want:         "malformed rev file: packfile hash mismatch wanted \"aa7497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2\" got \"407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2\"",
		},
		{
			name: "longer rev file",
			revFile: bufio.NewReader(io.MultiReader(
				fixture.Rev(),
				bytes.NewReader([]byte{0xFF}),
			)),
			objCount:     6,
			packChecksum: plumbing.NewHash("407497645643e18a7ba56c6132603f167fe9c51c00361ee0c81d74a8f55d0ee2"),
			ch:           make(chan uint32),
			want:         "malformed rev file: expected EOF",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDecoder(tc.revFile, tc.objCount, tc.packChecksum)

			var err error
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				err = d.Decode(tc.ch)
				wg.Done()
			}()

			if tc.ch != nil {
				for range tc.ch {
				}
			}

			wg.Wait()
			if tc.want != "" {
				assert.EqualError(t, err, tc.want)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
