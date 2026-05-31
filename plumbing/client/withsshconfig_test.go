package client_test

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
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

// TestWithSSHConfig_UploadPack is a black-box test of WithSSHConfig: it
// configures SSH through a typed Identity and HostConfig and performs a real
// upload-pack handshake against an SSH git server.
func TestWithSSHConfig_UploadPack(t *testing.T) {
	t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "missing_known_hosts"))

	addr := test.StartGitSSHServer(t)
	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath := filepath.ToSlash(repoFS.Root())

	host := &ssh.HostConfig{StrictHostKeyChecking: ssh.HostKeyCheckInsecureIgnore}
	cl := client.New(client.WithSSHConfig(&ssh.Identity{User: "git"}, host))

	session, err := cl.Handshake(context.Background(), &transport.Request{
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
