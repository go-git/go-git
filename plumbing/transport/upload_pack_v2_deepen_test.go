package transport

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func TestUploadPackV2FetchDeepenSince(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)

	// since == HEAD's committer time: only the tip qualifies; its older parents
	// fall outside the boundary, so the tip is shallow.
	since := c.Committer.When.Unix()
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen-since " + strconv.FormatInt(since, 10),
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "shallow "+head.Hash().String())
	require.Less(t, strings.Index(out, "shallow-info"), strings.Index(out, "packfile"),
		"shallow-info must precede the packfile section")
}

func TestUploadPackV2FetchDeepenNot(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	// Excluding the parent leaves only the tip reachable, so the tip is the
	// shallow boundary (its parent is excluded).
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen-not " + parent.String(),
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "shallow "+head.Hash().String())
}

func TestUploadPackV2FetchDeepenRelative(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	// deepen-relative with depth 1 on a fresh fetch (no haves) is the same as
	// deepen 1: the tip is the shallow boundary.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen 1",
		"deepen-relative",
		"done",
	}))

	require.Contains(t, out, "shallow "+head.Hash().String())
}

func TestUploadPackV2FetchDeepenAndSinceConflict(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	var out bytes.Buffer
	err = UploadPack(context.TODO(), st,
		v2Request(t, "fetch", nil, []string{
			"want " + head.Hash().String(),
			"deepen 1",
			"deepen-since 1",
			"done",
		}),
		ioutil.WriteNopCloser(&out),
		&UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be used together")
}

func TestGetShallowCommitsByRevListInvariants(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	since := c.Committer.When

	var upd packp.ShallowUpdate
	require.NoError(t, getShallowCommitsByRevList(t.Context(), st, []plumbing.Hash{head.Hash()}, since, nil, &upd))

	require.NotEmpty(t, upd.Shallows)
	for _, h := range upd.Shallows {
		sc, err := object.GetCommit(t.Context(), st, h)
		require.NoError(t, err)
		require.False(t, sc.Committer.When.Before(since),
			"a shallow commit must satisfy the deepen-since cutoff")
		olderParent := false
		for _, p := range sc.ParentHashes {
			if pc, err := object.GetCommit(t.Context(), st, p); err == nil && pc.Committer.When.Before(since) {
				olderParent = true
			}
		}
		require.True(t, olderParent, "a shallow boundary commit must have a parent beyond the cutoff")
	}
}

func TestIncludeReachableTags(t *testing.T) {
	t.Parallel()
	st := v2StorageFromFixture(t, fixtures.ByTag("tags").One())

	var tagHash, target plumbing.Hash
	iter, err := st.IterReferences(t.Context())
	require.NoError(t, err)
	_ = iter.ForEach(t.Context(), func(ref *plumbing.Reference) error {
		if !tagHash.IsZero() || ref.Type() != plumbing.HashReference || !ref.Name().IsTag() {
			return nil
		}
		if _, err := object.GetTag(t.Context(), st, ref.Hash()); err != nil {
			return nil // lightweight tag
		}
		if peeled, ok := peelToNonTag(t.Context(), st, ref.Hash()); ok {
			tagHash, target = ref.Hash(), peeled
		}
		return nil
	})
	iter.Close()
	require.False(t, tagHash.IsZero(), "tags fixture must contain an annotated tag")

	// Target in the pack: the annotated tag is auto-included.
	withTag, err := includeReachableTags(t.Context(), st, []plumbing.Hash{target})
	require.NoError(t, err)
	require.Contains(t, withTag, tagHash,
		"annotated tag whose target is packed must be included")

	// Target absent: the tag is not added.
	without, err := includeReachableTags(t.Context(), st, nil)
	require.NoError(t, err)
	require.NotContains(t, without, tagHash,
		"annotated tag whose target is not packed must be omitted")
}

func TestUploadPackV2FetchDeepenExistingShallow(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	// The client has a depth-1 shallow clone (boundary = tip) and deepens to 2:
	// it offers the tip as want and have, reports its shallow boundary, and asks
	// for deepen 2. The server must extend the boundary to the parent and mark
	// the tip unshallow, rather than ignoring the deepen.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + head.Hash().String(),
		"shallow " + head.Hash().String(),
		"deepen 2",
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "shallow "+parent.String(),
		"the deepened boundary (depth 2) must be the parent")
	require.Contains(t, out, "unshallow "+head.Hash().String(),
		"the previously-shallow tip is now interior and must be unshallowed")
}

func TestUploadPackV2FetchDeepenRelativeExistingShallow(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	// The client has a depth-1 shallow clone (boundary = tip) and asks to deepen
	// by one more commit relative to that boundary. deepen-relative counts the
	// depth from the client's existing shallow, so this must extend the boundary
	// to the parent and unshallow the tip, not no-op as an absolute deepen 1
	// (which would just re-report the tip) would.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + head.Hash().String(),
		"shallow " + head.Hash().String(),
		"deepen 1",
		"deepen-relative",
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "shallow "+parent.String(),
		"deepen-relative 1 from a depth-1 clone must extend the boundary to the parent")
	require.Contains(t, out, "unshallow "+head.Hash().String(),
		"the previously-shallow tip is now interior and must be unshallowed")
}

func TestUploadPackV2FetchDeepenExistingShallowToFullHistory(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")

	// A depth-1 shallow clone deepens past the whole history. The deepen reaches
	// full history, so the resulting boundary is empty; the server must still
	// emit shallow-info with the unshallow line rather than silently dropping the
	// transition and keeping the client shallow.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + head.Hash().String(),
		"shallow " + head.Hash().String(),
		"deepen 1000000",
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "unshallow "+head.Hash().String(),
		"an unshallowing deepen must emit the unshallow line for the old boundary")
}

func TestUploadPackV2FetchNoProgressEmitsNoProgressBand(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, UploadPack(context.TODO(), st,
		v2Request(t, "fetch", nil, []string{
			"want " + head.Hash().String(),
			"no-progress",
			"done",
		}),
		ioutil.WriteNopCloser(&buf),
		&UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true},
	))

	// Advance to the packfile section header.
	rd := bufio.NewReader(&buf)
	for {
		_, line, err := pktline.ReadLine(rd)
		require.NoError(t, err)
		if strings.TrimRight(string(line), "\n") == "packfile" {
			break
		}
	}
	// Every sideband packet that follows must be on the data band (1), never the
	// progress band (2): the v2 server emits no progress, so no-progress holds.
	for {
		l, line, err := pktline.ReadLine(rd)
		if l == pktline.Flush {
			break
		}
		require.NoError(t, err)
		require.NotEmpty(t, line)
		require.NotEqual(t, byte(sideband.ProgressMessage), line[0],
			"upload-pack must not write a sideband progress band")
	}
}
