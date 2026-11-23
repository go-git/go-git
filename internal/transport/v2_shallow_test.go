package transport

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

func shallowFeature() capability.List {
	var caps capability.List
	caps.Set(capability.FetchCmd, "shallow")
	return caps
}

func TestFetchV2AdvertisesShallowWithoutDepth(t *testing.T) {
	t.Parallel()

	boundary := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	want := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	st := memory.NewStorage()
	require.NoError(t, st.SetShallow([]plumbing.Hash{boundary}))

	var sent *packp.FetchArgs
	round := func(args *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		sent = args
		return nil, nil, io.ErrUnexpectedEOF
	}
	req := &FetchRequest{Wants: []plumbing.Hash{want}, Haves: []plumbing.Hash{boundary}}

	require.ErrorIs(t, FetchV2(context.Background(), st, shallowFeature(), req, round), io.ErrUnexpectedEOF)
	require.NotNil(t, sent)
	assert.Equal(t, []plumbing.Hash{boundary}, sent.Shallows)
	assert.Equal(t, []plumbing.Hash{boundary}, sent.Haves)
	assert.Zero(t, sent.Deepen)
}

func TestFetchV2ShallowNoOpNeedsNoShallowFeature(t *testing.T) {
	t.Parallel()

	boundary := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	st := memory.NewStorage()
	require.NoError(t, st.SetShallow([]plumbing.Hash{boundary}))

	round := func(*packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		t.Fatal("a no-op fetch must not reach the server")
		return nil, nil, nil
	}
	req := &FetchRequest{Wants: []plumbing.Hash{boundary}, Haves: []plumbing.Hash{boundary}}

	require.ErrorIs(t, FetchV2(context.Background(), st, capability.List{}, req, round), ErrNoChange)
}

func TestFetchV2ShallowClientNeedsShallowFeature(t *testing.T) {
	t.Parallel()

	boundary := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	want := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	st := memory.NewStorage()
	require.NoError(t, st.SetShallow([]plumbing.Hash{boundary}))

	round := func(*packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		t.Fatal("the fetch must fail before reaching the server")
		return nil, nil, nil
	}
	req := &FetchRequest{Wants: []plumbing.Hash{want}, Haves: []plumbing.Hash{boundary}}

	require.ErrorIs(t, FetchV2(context.Background(), st, capability.List{}, req, round), ErrShallowNotSupported)
}
