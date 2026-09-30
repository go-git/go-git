package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFetchV2ShallowInfo(t *testing.T) {
	t.Parallel()

	f, err := fixtures.Basic().One().Packfile()
	require.NoError(t, err)
	pack, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	master := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	older := plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294")
	undelivered := plumbing.NewHash("0000000000000000000000000000000000000001")

	// The server answers with the whole fixture pack and names two shallow
	// roots whether or not the client asked to deepen: one commit the pack
	// carries and one it does not, as a shallow server or a hostile one may.
	sent := &packp.ShallowInfo{Shallows: []plumbing.Hash{master, undelivered}}
	round := func(_ *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		var buf bytes.Buffer
		mux := sideband.NewMuxer(sideband.Sideband64k, &buf)
		if _, err := mux.Write(pack); err != nil {
			return nil, nil, err
		}
		if err := pktline.WriteFlush(&buf); err != nil {
			return nil, nil, err
		}
		return &packp.FetchOutput{ShallowInfo: sent, Packfile: true}, &buf, nil
	}

	tests := []struct {
		name        string
		depth       int
		existingRef *plumbing.Reference
		wantShallow []plumbing.Hash
	}{
		{
			name:        "deepen request records every root",
			depth:       1,
			wantShallow: []plumbing.Hash{master, undelivered},
		},
		{
			name:        "clone records the delivered roots",
			wantShallow: []plumbing.Hash{master},
		},
		{
			name:        "fetch into existing repository records nothing",
			existingRef: plumbing.NewHashReference("refs/heads/master", older),
			wantShallow: []plumbing.Hash{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage()
			if tc.existingRef != nil {
				require.NoError(t, st.SetReference(tc.existingRef))
			}

			req := &FetchRequest{Wants: []plumbing.Hash{master}, Depth: tc.depth}
			require.NoError(t, FetchV2(context.Background(), st, req, round))
			require.NoError(t, st.HasEncodedObject(master))

			shallows, err := st.Shallow()
			require.NoError(t, err)
			require.Equal(t, tc.wantShallow, shallows)
		})
	}
}
