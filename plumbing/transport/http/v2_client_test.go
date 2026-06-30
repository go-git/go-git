package http

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestBackend_HTTP_V2_GoGitClient exercises the go-git HTTP transport speaking
// Protocol v2 (handshake, ls-refs, and fetch) against go-git's own v2 server.
func TestBackend_HTTP_V2_GoGitClient(t *testing.T) {
	t.Parallel()
	requireGitV2(t)

	base, name := setupGoGitBackendServer(t)
	pu, err := url.Parse(fmt.Sprintf("http://u:p@%s/%s", strings.TrimPrefix(base, "http://"), name))
	require.NoError(t, err)

	tr := NewTransport(Options{})
	sess, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      pu,
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	sps, ok := sess.(*smartPackSession)
	require.True(t, ok)
	require.Equal(t, protocol.V2, sps.version)

	refs, err := sess.GetRemoteRefs(context.Background(), nil)
	require.NoError(t, err)

	var want plumbing.Hash
	var sawHead bool
	for _, r := range refs.References {
		if r.Name() == "refs/heads/main" {
			want = r.Hash()
		}
		if r.Name() == plumbing.HEAD {
			sawHead = true
			require.Equal(t, plumbing.ReferenceName("refs/heads/main"), r.Target())
		}
	}
	require.False(t, want.IsZero(), "expected refs/heads/main from v2 ls-refs")
	require.True(t, sawHead, "expected symref HEAD from v2 ls-refs")

	st := memory.NewStorage()
	require.NoError(t, sess.Fetch(context.Background(), st, &transport.FetchRequest{
		Wants: []plumbing.Hash{want},
	}))
	cobj, err := st.EncodedObject(plumbing.CommitObject, want)
	require.NoError(t, err)
	require.Equal(t, want, cobj.Hash())

	refsHeads, err := sess.GetRemoteRefs(context.Background(), &transport.GetRemoteRefsOptions{
		RefPrefixes: []string{"refs/heads/"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, refsHeads.References)
	for _, r := range refsHeads.References {
		require.True(t, strings.HasPrefix(r.Name().String(), "refs/heads/"),
			"ref-prefix filter should drop %s", r.Name())
	}
}
