package ssh

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// TestSSHTransport_ExplicitCallbackNoKnownHosts verifies that a caller-supplied
// HostKeyCallback with no HostKeyAlgorithms does not require a known_hosts file:
// the transport must negotiate default algorithms instead of failing. The
// caller has taken ownership of host verification via the callback.
func TestSSHTransport_ExplicitCallbackNoKnownHosts(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "missing_known_hosts"))

	addr := startSSHServer(t)
	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath := filepath.ToSlash(repoFS.Root())

	tr := NewTransport(sshClientOptions())
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   fmt.Sprintf("localhost:%d", addr.Port),
			Path:   repoPath,
		},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	refs, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, refs)
}
