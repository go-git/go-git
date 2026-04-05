package git

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func freePort(t *testing.T) int {
	t.Helper()
	port, err := test.FreePort()
	require.NoError(t, err)
	return port
}

func startDaemon(t *testing.T, base string, port int) {
	t.Helper()
	daemon := exec.Command("git", "daemon",
		fmt.Sprintf("--base-path=%s", base),
		"--export-all", "--enable=receive-pack", "--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		"--max-connections=1", "--listen=127.0.0.1",
	)
	daemon.Env = os.Environ()
	require.NoError(t, daemon.Start())

	t.Cleanup(func() {
		if daemon.Process != nil {
			// Signal graceful shutdown; do not use Kill which leaves
			// child processes (git-upload-pack, git-receive-pack) orphaned.
			// Do not Wait — on Windows os.Interrupt is a no-op so Wait
			// would block forever.
			_ = daemon.Process.Signal(os.Interrupt)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, waitForPort(ctx, port))
}

func waitForPort(ctx context.Context, port int) error {
	for {
		select {
		case <-ctx.Done():
			return errors.New("context canceled before the port is connectable")
		case <-time.After(10 * time.Millisecond):
			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				return conn.Close()
			}
		}
	}
}

const windowsSkipMsg = `git for windows has issues with write operations through git:// protocol.
See https://github.com/git-for-windows/git/issues/907`

func TestGitTransport_Connect(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	for _, tc := range []struct {
		name    string
		command string
	}{
		{"UploadPack", "git-upload-pack"},
		{"ReceivePack", "git-upload-pack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			port := freePort(t)
			base := filepath.Join(t.TempDir(), fmt.Sprintf("git-proto-%d", port))
			_ = test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
			startDaemon(t, base, port)

			tr := NewTransport(Options{})

			req := &transport.Request{
				URL: &url.URL{
					Scheme: "git",
					Host:   fmt.Sprintf("localhost:%d", port),
					Path:   "/basic.git",
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

func TestGitTransport_ConnectFail(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL: &url.URL{
			Scheme: "git",
			Host:   "localhost:1",
			Path:   "/nonexistent.git",
		},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}
