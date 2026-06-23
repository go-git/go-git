package http

import (
	"context"
	"fmt"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/gitcli"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func v2Session(t *testing.T) (transport.Session, func()) {
	t.Helper()
	gitcli.SkipIfProtocolV2Unsupported(t)

	base, addr := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:      httpEndpoint(addr, "basic.git"),
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	require.NoError(t, err)

	require.Contains(t, fmt.Sprintf("%T", session), "v2Session",
		"smart HTTP should negotiate protocol v2")

	return session, func() { _ = session.Close() }
}

func TestHTTPv2LsRefs(t *testing.T) {
	t.Parallel()

	session, cleanup := v2Session(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refs, err := session.GetRemoteRefs(ctx, nil)
	require.NoError(t, err)

	byName := map[string]*plumbing.Reference{}
	for _, r := range refs {
		byName[r.Name().String()] = r
	}
	require.Contains(t, byName, "refs/heads/master")
	require.Contains(t, byName, "HEAD")
}

func TestHTTPv2Fetch(t *testing.T) {
	t.Parallel()

	session, cleanup := v2Session(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refs, err := session.GetRemoteRefs(ctx, nil)
	require.NoError(t, err)

	var want plumbing.Hash
	for _, r := range refs {
		if r.Name().String() == "refs/heads/master" {
			want = r.Hash()
		}
	}
	require.False(t, want.IsZero())

	st := memory.NewStorage()
	err = session.Fetch(ctx, st, &transport.FetchRequest{Wants: []plumbing.Hash{want}})
	require.NoError(t, err)

	obj, err := st.EncodedObject(plumbing.CommitObject, want)
	require.NoError(t, err)
	require.Equal(t, want, obj.Hash())
}
