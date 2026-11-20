package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

type shallowWalkingStorer struct {
	storage.Storer
	revListCalls  int
	shallowsCalls [][]plumbing.Hash
	// objects holds what the last call to RevListObjectsWithShallows
	// returned.
	objects []plumbing.Hash
}

func (s *shallowWalkingStorer) RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	s.revListCalls++
	return revlist.Objects(s.Storer, wants, haves)
}

func (s *shallowWalkingStorer) RevListObjectsWithShallows(wants, haves, shallows []plumbing.Hash) ([]plumbing.Hash, error) {
	s.shallowsCalls = append(s.shallowsCalls, append([]plumbing.Hash(nil), shallows...))
	objs, err := revlist.ObjectsWithShallows(s.Storer, wants, haves, shallows)
	s.objects = objs
	return objs, err
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
	// One walk is grafted at both the client's existing boundary and the
	// new one.
	require.Len(t, st.shallowsCalls, 1)
	require.ElementsMatch(t, []plumbing.Hash{head.Hash(), parent}, st.shallowsCalls[0])
	require.Zero(t, st.revListCalls)
	// The client's have reaches the walk: the commit it has is not sent
	// again, while its parent, newly inside the boundary, is.
	require.NotContains(t, st.objects, head.Hash())
	require.Contains(t, st.objects, parent)
}

// TestUploadPackV1DeepenUsesStorerWalk checks that v0/v1 shallow fetches,
// which reach the object walk on a path of their own, go through the storer's
// shallow walk as v2 does, with the client's haves.
func TestUploadPackV1DeepenUsesStorerWalk(t *testing.T) {
	t.Parallel()

	for _, existing := range []bool{false, true} {
		name := "new shallow clone"
		if existing {
			name = "deepen existing shallow clone"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
			head, err := storer.ResolveReference(st, plumbing.HEAD)
			require.NoError(t, err)
			c, err := object.GetCommit(st, head.Hash())
			require.NoError(t, err)
			require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
			parent := c.ParentHashes[0]

			upreq := &packp.UploadRequest{Wants: []plumbing.Hash{head.Hash()}}
			upreq.Capabilities.Add("shallow")
			upreq.Depth.Deepen = 1
			var haves []plumbing.Hash
			if existing {
				upreq.Shallows = []plumbing.Hash{head.Hash()}
				upreq.Depth.Deepen = 2
				haves = []plumbing.Hash{head.Hash()}
			}
			var out bytes.Buffer
			require.NoError(t, UploadPack(t.Context(), st, io.NopCloser(bytes.NewReader(encodeV1Request(t, upreq, haves...))),
				ioutil.WriteNopCloser(&out), &UploadPackRequest{GitProtocol: "version=1", StatelessRPC: true}))

			require.Contains(t, out.String(), "PACK")
			require.Zero(t, st.revListCalls)
			require.Len(t, st.shallowsCalls, 1)
			if !existing {
				require.Equal(t, []plumbing.Hash{head.Hash()}, st.shallowsCalls[0])
				require.Contains(t, st.objects, head.Hash())
				require.NotContains(t, st.objects, parent)
				return
			}
			require.ElementsMatch(t, []plumbing.Hash{head.Hash(), parent}, st.shallowsCalls[0])
			require.NotContains(t, st.objects, head.Hash())
			require.Contains(t, st.objects, parent)
		})
	}
}

// TestUploadPackV2FetchDeepenPastHistoryUsesRevListObjects checks that a
// client that is not shallow, deepening past the start of history, gets the
// plain walk: there is no boundary to graft.
func TestUploadPackV2FetchDeepenPastHistoryUsesRevListObjects(t *testing.T) {
	t.Parallel()
	st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen 1000",
		"done",
	}))

	require.Contains(t, out, "packfile")
	require.Positive(t, st.revListCalls)
	require.Empty(t, st.shallowsCalls)
}

// TestUploadPackV2FetchUnshallowUsesStorerWalk checks that a shallow client
// deepening to its full history still goes through the storer's shallow walk:
// the new boundary is empty, but the walk must still stop at the client's
// own shallow commit, from whose parents the rest of the history is sent.
func TestUploadPackV2FetchUnshallowUsesStorerWalk(t *testing.T) {
	t.Parallel()

	for _, deepen := range []string{"deepen 2147483647", "deepen 1000"} {
		t.Run(deepen, func(t *testing.T) {
			t.Parallel()
			st := &shallowWalkingStorer{Storer: basicV2Storage(t)}
			head, err := storer.ResolveReference(st, plumbing.HEAD)
			require.NoError(t, err)

			out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
				"want " + head.Hash().String(),
				"have " + head.Hash().String(),
				"shallow " + head.Hash().String(),
				deepen,
				"done",
			}))

			require.Contains(t, out, "packfile")
			require.Equal(t, [][]plumbing.Hash{{head.Hash()}}, st.shallowsCalls)
			require.Zero(t, st.revListCalls)

			// Every commit below the client's shallow commit is sent.
			history, err := revlist.Objects(st.Storer, []plumbing.Hash{head.Hash()}, nil)
			require.NoError(t, err)
			sent := make(map[plumbing.Hash]struct{}, len(st.objects))
			for _, h := range st.objects {
				sent[h] = struct{}{}
			}
			var below int
			for _, h := range history {
				c, err := object.GetCommit(st.Storer, h)
				if err != nil || c.Hash == head.Hash() {
					continue
				}
				below++
				require.Contains(t, sent, h, "commit %s below the shallow commit was not sent", h)
			}
			require.Positive(t, below)
		})
	}
}

// revListOnlyStorer has a custom walker that cannot see the shallow boundary.
type revListOnlyStorer struct {
	storage.Storer
	revListCalls int
}

func (s *revListOnlyStorer) RevListObjects(wants, haves []plumbing.Hash) ([]plumbing.Hash, error) {
	s.revListCalls++
	return revlist.Objects(s.Storer, wants, haves)
}

func TestUploadPackV2FetchShallowRevListOnlyStorerUsesBuiltinWalk(t *testing.T) {
	t.Parallel()
	st := &revListOnlyStorer{Storer: basicV2Storage(t)}
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen 1",
		"done",
	}))

	require.Contains(t, out, "packfile")
	require.Zero(t, st.revListCalls)
}
