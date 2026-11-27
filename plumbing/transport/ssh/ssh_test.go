package ssh

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stdssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/testdata"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func startSSHServer(t *testing.T, opts ...ssh.Option) *net.TCPAddr {
	t.Helper()

	l := test.ListenTCP(t)
	server := &ssh.Server{Handler: handlerSSH}
	server.SetOption(ssh.HostKeyPEM(testdata.PEMBytes["ed25519"]))
	for _, opt := range opts {
		require.NoError(t, server.SetOption(opt))
	}

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

	cmd := gitenv.Command(args[0], args[1:]...)
	// Forward the SSH session environment (notably GIT_PROTOCOL, which the
	// client sets via Setenv) so git speaks the requested wire version.
	cmd.Env = append(cmd.Env, s.Environ()...)
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

func TestBuildCommand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		command string
		path    string
		args    []string
		want    string
	}{
		{
			name:    "plain path",
			command: "git-upload-pack",
			path:    "/repo.git",
			want:    "git-upload-pack '/repo.git'",
		},
		{
			name:    "path with single-quote injection payload",
			command: "git-upload-pack",
			path:    "/repo.git'; touch /tmp/x ; #",
			want:    `git-upload-pack '/repo.git'\''; touch /tmp/x ; #'`,
		},
		{
			name:    "arg with single-quote injection payload",
			command: "git-lfs-authenticate",
			path:    "/repo.git",
			args:    []string{`download'; rm -rf / ; #`},
			want:    `git-lfs-authenticate '/repo.git' 'download'\''; rm -rf / ; #'`,
		},
		{
			name:    "empty args has no trailing space",
			command: "git-upload-pack",
			path:    "/repo.git",
			args:    []string{},
			want:    "git-upload-pack '/repo.git'",
		},
		{
			name:    "bang is escaped for csh history expansion",
			command: "git-upload-pack",
			path:    "/repo!.git",
			want:    `git-upload-pack '/repo'\!'.git'`,
		},
		{
			name:    "mixed quote and bang",
			command: "git-upload-pack",
			path:    "/a'b!c",
			want:    `git-upload-pack '/a'\''b'\!'c'`,
		},
		{
			name:    "inert shell metacharacters pass through",
			command: "git-upload-pack",
			path:    "/a\\b\"c$d`e",
			want:    "git-upload-pack '/a\\b\"c$d`e'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := &transport.Request{
				Command: tc.command,
				URL:     &url.URL{Path: tc.path},
				Args:    tc.args,
			}
			assert.Equal(t, tc.want, buildCommand(req))
		})
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

func TestSSHTransport_Archive(t *testing.T) {
	t.Parallel()

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
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	defer session.Close()

	a, ok := session.(transport.Archiver)
	require.True(t, ok, "session should implement Archiver")

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Greater(t, len(data), 0)

	tarR := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tarR.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}

func TestSSHTransport_CustomHostKeyCallbackWithoutKnownHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(home, "missing_known_hosts"))

	addr := startSSHServer(t)
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), t.TempDir(), "basic.git")

	auth := &Password{
		User: "git",
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
		},
	}
	tr := NewTransport(Options{ClientConfig: auth.ClientConfig})

	sess, err := tr.Connect(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   formatSSHHost(addr),
			Path:   filepath.ToSlash(repoFS.Root()),
		},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	require.NoError(t, sess.Close())
}

func TestSSHTransport_HostKeyAlgorithms(t *testing.T) {
	t.Parallel()

	// x/crypto/ssh prefers ECDSA over Ed25519 host keys by default, so
	// without HostKeyAlgorithms the server's ECDSA key would be negotiated
	// and rejected by FixedHostKey.
	addr := startSSHServer(t, ssh.HostKeyPEM(testdata.PEMBytes["ecdsa"]))
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), t.TempDir(), "basic.git")

	signer, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	require.NoError(t, err)
	fixed := stdssh.FixedHostKey(signer.PublicKey())

	var gotKeyType string
	auth := &Password{
		User: "git",
		HostKeyCallbackHelper: HostKeyCallbackHelper{
			HostKeyCallback: func(hostname string, remote net.Addr, key stdssh.PublicKey) error {
				gotKeyType = key.Type()
				return fixed(hostname, remote, key)
			},
			HostKeyAlgorithms: []string{stdssh.KeyAlgoED25519},
		},
	}
	tr := NewTransport(Options{ClientConfig: auth.ClientConfig})

	sess, err := tr.Connect(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   formatSSHHost(addr),
			Path:   filepath.ToSlash(repoFS.Root()),
		},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	require.NoError(t, sess.Close())
	assert.Equal(t, stdssh.KeyAlgoED25519, gotKeyType)
}

func TestSSHTransport_KnownHostsSelectsHostKeyAlgorithms(t *testing.T) {
	addr := startSSHServer(t, ssh.HostKeyPEM(testdata.PEMBytes["ecdsa"]))
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), t.TempDir(), "basic.git")

	signer, err := stdssh.ParsePrivateKey(testdata.PEMBytes["ed25519"])
	require.NoError(t, err)

	// x/crypto/ssh prefers ECDSA by default, so the client must restrict
	// negotiation to the key types known_hosts has for this host, or
	// verification fails with a key mismatch.
	// See https://github.com/go-git/go-git/issues/411.
	home := t.TempDir()
	knownHosts := filepath.Join(home, "known_hosts")
	line := knownhosts.Line([]string{formatSSHHost(addr)}, signer.PublicKey())
	require.NoError(t, os.WriteFile(knownHosts, []byte(line+"\n"), 0o600))
	t.Setenv("HOME", home)
	t.Setenv("SSH_KNOWN_HOSTS", knownHosts)

	auth := &Password{User: "git"}
	tr := NewTransport(Options{ClientConfig: auth.ClientConfig})

	sess, err := tr.Connect(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "ssh",
			User:   url.User("git"),
			Host:   formatSSHHost(addr),
			Path:   filepath.ToSlash(repoFS.Root()),
		},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	require.NoError(t, sess.Close())
}
