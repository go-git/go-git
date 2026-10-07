package transport

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestFetchV2AdvertisesShallowWithoutDepth pins the request shape: a shallow
// repository sends its boundary in FetchArgs.Shallows whether or not the
// fetch deepens, mirroring add_shallow_requests (fetch-pack.c:1280-1297).
func TestFetchV2AdvertisesShallowWithoutDepth(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	require.NoError(t, st.SetShallow([]plumbing.Hash{hash}))

	req := &FetchRequest{
		Wants: []plumbing.Hash{hash},
	}
	// No Depth, no Haves: previously this short-circuited to ErrNoChange
	// before any round trip; now the shallow boundary must reach the args.

	var got *packp.FetchArgs
	round := func(args *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		got = args
		return nil, nil, errStop
	}
	err := FetchV2(context.Background(), st, req, round)
	require.ErrorIs(t, err, errStop) // the mock refuses to serve; only the args matter
	require.NotNil(t, got)
	require.Equal(t, []plumbing.Hash{hash}, got.Shallows,
		"a shallow repository must advertise its boundary in v2 fetch args")
	require.Zero(t, got.Deepen, "no depth was requested")
}

var errStop = errors.New("stop")
