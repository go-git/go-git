package transport

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

type shallowWalkingStorer struct {
	storage.Storer
	revListCalls  int
	shallowsCalls [][]plumbing.Hash
}

func (s *shallowWalkingStorer) RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	s.revListCalls++
	return revlist.Objects(s.Storer, wants, haves)
}

func (s *shallowWalkingStorer) RevListObjectsWithShallows(wants, haves, shallows []plumbing.Hash) ([]plumbing.Hash, error) {
	s.shallowsCalls = append(s.shallowsCalls, append([]plumbing.Hash(nil), shallows...))
	return revlist.Objects(&shallowBoundaryStorer{Storer: s.Storer, boundary: shallows}, wants, haves)
}

func TestUploadPackV2FetchNonShallowUsesRevListObjects(t *testing.T) {
	t.Parallel()
	st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"done",
	}))

	require.Contains(t, out, "packfile")
	require.Positive(t, st.revListCalls)
	require.Empty(t, st.shallowsCalls)
}

func TestUploadPackV2FetchShallowUsesStorerWalk(t *testing.T) {
	t.Parallel()
	st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen 1",
		"done",
	}))

	require.Contains(t, out, "packfile")
	require.Equal(t, [][]plumbing.Hash{{head.Hash()}}, st.shallowsCalls)
	// RevListObjects cannot see the per-request boundary, so using it here
	// would ship the full history to a depth-1 client.
	require.Zero(t, st.revListCalls)
}

func TestUploadPackV2FetchDeepenExistingShallowUsesStorerWalk(t *testing.T) {
	t.Parallel()
	st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + head.Hash().String(),
		"shallow " + head.Hash().String(),
		"deepen 2",
		"done",
	}))

	require.Contains(t, out, "packfile")
	// The deepened view is grafted at the new boundary, the client's view at
	// its existing one.
	require.ElementsMatch(t, [][]plumbing.Hash{{parent}, {head.Hash()}}, st.shallowsCalls)
	require.Zero(t, st.revListCalls)
}
