package transport

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func basicV2Storage(t testing.TB) storage.Storer {
	t.Helper()
	return v2StorageFromFixture(t, fixtures.Basic().One())
}

func v2StorageFromFixture(t testing.TB, f *fixtures.Fixture) storage.Storer {
	t.Helper()
	dot, err := f.DotGit(fixtures.WithTargetDir(t.TempDir))
	require.NoError(t, err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// v2Request encodes a protocol-v2 command request: command line, optional
// header lines, a delim packet, the post-delim argument lines, and a flush.
func v2Request(t testing.TB, command string, headers, args []string) io.ReadCloser {
	t.Helper()
	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "command=%s\n", command)
	require.NoError(t, err)
	for _, h := range headers {
		_, err := pktline.Writef(&buf, "%s\n", h)
		require.NoError(t, err)
	}
	require.NoError(t, pktline.WriteDelim(&buf))
	for _, a := range args {
		_, err := pktline.Writef(&buf, "%s\n", a)
		require.NoError(t, err)
	}
	require.NoError(t, pktline.WriteFlush(&buf))
	return io.NopCloser(&buf)
}

func serveUploadPackV2Test(t testing.TB, st storage.Storer, r io.ReadCloser) string {
	t.Helper()
	var out bytes.Buffer
	err := UploadPack(context.TODO(), st, r, ioutil.WriteNopCloser(&out), &UploadPackRequest{
		GitProtocol:  "version=2",
		StatelessRPC: true,
	})
	require.NoError(t, err)
	return out.String()
}

func TestUploadPackV2AdvertisementCapabilities(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	var out bytes.Buffer
	err := UploadPack(context.TODO(), st, io.NopCloser(bytes.NewBuffer(nil)), ioutil.WriteNopCloser(&out), &UploadPackRequest{
		GitProtocol:   "version=2",
		AdvertiseRefs: true,
		StatelessRPC:  true,
	})
	require.NoError(t, err)
	adv := out.String()

	require.Contains(t, adv, "version 2")
	require.Contains(t, adv, "ls-refs")
	require.Contains(t, adv, "fetch=shallow")
	require.Contains(t, adv, "object-format=")

	// git omits the smart-HTTP "# service=..." line for v2 even over HTTP
	// (http-backend.c get_info_refs): the response starts with the version
	// packet. Emitting it would diverge from the reference server.
	require.NotContains(t, adv, "# service=")
	require.True(t, strings.HasPrefix(adv[4:], "version 2"),
		"v2 advertisement must start with the version packet, got %q", adv[:32])

	// Capabilities the server does not implement must not be advertised,
	// otherwise clients request features that are silently dropped or
	// produce malformed responses.
	require.NotContains(t, adv, "wait-for-done")
	require.NotContains(t, adv, "unborn")
	require.NotContains(t, adv, "server-option")
}

func TestUploadPackV2LsRefsPeeledInline(t *testing.T) {
	t.Parallel()
	st := v2StorageFromFixture(t, fixtures.ByTag("tags").One())

	out := serveUploadPackV2Test(t, st, v2Request(t, "ls-refs", nil, []string{"peel"}))

	// Protocol v2 ls-refs encodes the peeled value as a same-line attribute
	// "peeled:<oid>", not as a separate "<oid> <name>^{}" line (the v0/v1
	// advertisement format).
	require.Contains(t, out, "peeled:")
	require.NotContains(t, out, "^{}")
}

func TestUploadPackV2FetchShallowDeepen(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	// A depth-1 fetch: the tip is the shallow boundary. The response carries a
	// shallow-info section (shallow <tip>) before the packfile.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"deepen 1",
		"done",
	}))

	require.Contains(t, out, "shallow-info")
	require.Contains(t, out, "shallow "+head.Hash().String())
	require.Contains(t, out, "packfile")
	require.Less(t, strings.Index(out, "shallow-info"), strings.Index(out, "packfile"),
		"shallow-info must precede the packfile section")
}

func TestObjectsToUploadShallowBounded(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	full, err := objectsToUpload(t.Context(), st, []plumbing.Hash{head.Hash()}, nil)
	require.NoError(t, err)
	require.Contains(t, full, parent, "unbounded pack should include the parent commit")

	bounded, err := objectsToUpload(
		t.Context(),
		&shallowBoundaryStorer{Storer: st, boundary: []plumbing.Hash{head.Hash()}},
		[]plumbing.Hash{head.Hash()}, nil,
	)
	require.NoError(t, err)
	require.Contains(t, bounded, head.Hash(), "shallow boundary commit must be included")
	require.NotContains(t, bounded, parent, "depth-1 pack must exclude the boundary's parent")
	require.Less(t, len(bounded), len(full), "shallow pack must be smaller than the full pack")
}

func TestUploadPackV2FetchNoDeepenNoShallowInfo(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"done",
	}))

	require.NotContains(t, out, "shallow-info")
	require.Contains(t, out, "packfile")
}

func TestUploadPackV2FetchCloneNoHaves(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"done",
	}))

	// No haves: clone-like, no acknowledgments section, packfile follows.
	require.NotContains(t, out, "acknowledgments")
	require.Contains(t, out, "packfile")
}

func TestUploadPackV2FetchReadyWithCommonHave(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + head.Hash().String(),
	}))

	// A common object is known: acknowledgments section with ACK + ready,
	// then the packfile. ready must precede packfile.
	require.Contains(t, out, "acknowledgments")
	require.Contains(t, out, "ACK "+head.Hash().String())
	require.Contains(t, out, "ready")
	require.Contains(t, out, "packfile")
	require.Less(t, strings.Index(out, "ready"), strings.Index(out, "packfile"),
		"ready must be emitted before the packfile section")
}

func TestUploadPackV2FetchCommonButNotReady(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)
	c, err := object.GetCommit(t.Context(), st, head.Hash())
	require.NoError(t, err)
	require.NotEmpty(t, c.ParentHashes, "HEAD must have a parent for this test")
	parent := c.ParentHashes[0]

	// want an ancestor, have its descendant (HEAD). HEAD is a known object so it
	// is ACK'd, but it is not an ancestor of the want, so no want is anchored:
	// the server must withhold "ready" and end with a flush (no packfile),
	// mirroring upstream ok_to_give_up.
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + parent.String(),
		"have " + head.Hash().String(),
	}))

	require.Contains(t, out, "acknowledgments")
	require.Contains(t, out, "ACK "+head.Hash().String())
	require.NotContains(t, out, "ready")
	require.NotContains(t, out, "packfile")
}

func TestUploadPackV2FetchNoWantsEmitsNothing(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	// No want lines: upstream emits no response at all (no stray flush).
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{"have " + plumbing.ZeroHash.String()}))
	require.Empty(t, out)
}

func TestUploadPackV2LsRefsHeadResolvedOID(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	out := serveUploadPackV2Test(t, st, v2Request(t, "ls-refs", nil, []string{"symrefs"}))

	// Upstream send_ref emits HEAD with its resolved object id and a
	// symref-target attribute, not a zero id. This is why the server encodes
	// ls-refs lines directly rather than through packp.LsRefsOutput, whose
	// symbolic references cannot carry a resolved hash.
	require.Contains(t, out, head.Hash().String()+" HEAD symref-target:"+head.Name().String())
	require.NotContains(t, out, plumbing.ZeroHash.String()+" HEAD")
}

func TestUploadPackV2LsRefsHeadFilteredByPrefix(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)

	// With a ref-prefix that excludes HEAD, upstream does not advertise HEAD
	// (send_ref applies ref_match to HEAD too). No ref ending in HEAD matches
	// "refs/heads/", so none should appear.
	out := serveUploadPackV2Test(t, st, v2Request(t, "ls-refs", nil, []string{"ref-prefix refs/heads/"}))

	require.Contains(t, out, "refs/heads/master")
	require.NotContains(t, out, "HEAD")
}

func TestUploadPackV2FetchNotReadyContinuesNegotiation(t *testing.T) {
	t.Parallel()
	st := basicV2Storage(t)
	head, err := storer.ResolveReference(t.Context(), st, plumbing.HEAD)
	require.NoError(t, err)

	// A have the server does not know about: no common base yet, so the
	// server must NAK and end the acknowledgments section with a flush,
	// without sending a packfile. Negotiation continues in a later request.
	unknown := "1111111111111111111111111111111111111111"
	out := serveUploadPackV2Test(t, st, v2Request(t, "fetch", nil, []string{
		"want " + head.Hash().String(),
		"have " + unknown,
	}))

	require.Contains(t, out, "acknowledgments")
	require.Contains(t, out, "NAK")
	require.NotContains(t, out, "ready")
	require.NotContains(t, out, "packfile")
}
