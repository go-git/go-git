package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stdssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func startSSHServer(t *testing.T) *net.TCPAddr {
	t.Helper()

	l := test.ListenTCP(t)
	server := &ssh.Server{Handler: handlerSSH}
	server.SetOption(ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]))

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, server.Serve(l), net.ErrClosed)
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-done
	})
	return l.Addr().(*net.TCPAddr)
}

func handlerSSH(s ssh.Session) {
	args := s.Command()
	if len(args) < 2 {
		_, _ = fmt.Fprintln(s.Stderr(), "invalid command")
		_ = s.Exit(1)
		return
	}

	cmd := exec.Command(args[0], args[1:]...)
	stdout, _ := cmd.StdoutPipe()
	stdin, _ := cmd.StdinPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintln(s.Stderr(), err)
		_ = s.Exit(1)
		return
	}

	go func() {
		defer stdin.Close()
		io.Copy(stdin, s)
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(s.Stderr(), stderr) }()
	go func() { defer wg.Done(); io.Copy(s, stdout) }()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		_ = s.Exit(1)
		return
	}
	_ = s.Exit(0)
}

func sshClientOptions() Options {
	return Options{
		ClientConfig: func(_ context.Context, _ *transport.Request) (*stdssh.ClientConfig, error) {
			return &stdssh.ClientConfig{
				User:            "git",
				Auth:            []stdssh.AuthMethod{stdssh.Password("")},
				HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
			}, nil
		},
	}
}

func TestSSHTransport_Connect(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		command string
	}{
		{"Open", "git-upload-pack"},
		{"Connect", "git-upload-pack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			addr := startSSHServer(t)
			base := t.TempDir()
			repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
			repoPath := filepath.ToSlash(repoFS.Root())

			tr := NewTransport(sshClientOptions())

			req := &transport.Request{
				URL: &url.URL{
					Scheme: "ssh",
					User:   url.User("git"),
					Host:   fmt.Sprintf("localhost:%d", addr.Port),
					Path:   repoPath,
				},
				Command:  tc.command,
				Protocol: protocol.V0,
			}

			sess, err := tr.Connect(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, sess)

			buf := make([]byte, 4)
			n, err := sess.Reader().Read(buf)
			require.NoError(t, err)
			assert.Greater(t, n, 0, "should read pkt-line data from server")

			require.NoError(t, sess.Close())
		})
	}
}

func TestSSHTransport_NoConfig(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			Host:   "localhost:22",
			Path:   "/repo.git",
		},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}
