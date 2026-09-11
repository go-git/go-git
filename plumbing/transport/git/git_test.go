package git

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func freePort(t *testing.T) int {
	t.Helper()
	port, err := test.FreePort()
	require.NoError(t, err)
	return port
}

// daemonShutdown is how long the daemon is given to act on the interrupt
// below before exec kills it instead, and so the longest a test can be held
// at waitForShutdown. A daemon that stops when it is asked to, which is the
// ordinary case, is not waited on for any of it.
const daemonShutdown = 5 * time.Second

// waitForShutdown reaps cmd once the test that started it ends.
//
// A command built by exec.CommandContext watches its context in a goroutine
// of its own, and that goroutine hands its result to Wait. With no Wait to
// receive it, the watcher blocks on the send for good (os/exec/exec.go, the
// last line of watchCtx), and the process it killed is never collected: it
// stays a zombie until the test binary exits, taking a goroutine with it.
// Killing is not reaping, and WaitDelay does not stand in for a Wait.
//
// So Wait runs in the background from the moment the command starts, and the
// cleanup joins it. Joining is what keeps the test binary alive long enough
// for the escalation above to happen at all, and the wait is bounded by it:
// WaitDelay is handed to Wait, which kills the process when it elapses.
func waitForShutdown(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { <-waited })
}

func startDaemon(t *testing.T, base string, port int) {
	t.Helper()
	// Bound to the test's own context, which the testing package cancels
	// before the test's cleanups run, so the daemon ends with the test that
	// started it rather than through a cleanup remembering to end it.
	daemon := gitenv.CommandContext(t.Context(), "git", "daemon",
		fmt.Sprintf("--base-path=%s", base),
		"--export-all", "--enable=receive-pack", "--enable=upload-archive", "--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		"--max-connections=1", "--listen=127.0.0.1",
	)
	// Interrupt in place of the Kill exec would use, which leaves the daemon's
	// git-upload-pack and git-receive-pack children orphaned. WaitDelay bounds
	// the gentler ending: a daemon that does not act on the interrupt — the
	// signal is unsupported on Windows, and a shutdown can stall anywhere — is
	// killed rather than left running, which sending the signal and returning
	// had no answer for.
	daemon.Cancel = func() error { return daemon.Process.Signal(os.Interrupt) }
	daemon.WaitDelay = daemonShutdown
	require.NoError(t, daemon.Start())
	waitForShutdown(t, daemon)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
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
		{"ReceivePack", "git-receive-pack"},
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

			// A daemon that refuses the command still answers, with an
			// ERR pkt-line. Reading the first line as a pkt-line is what
			// tells acceptance from refusal: ReadLine reports an ERR
			// payload as *pktline.ErrorLine, where counting bytes off the
			// reader cannot distinguish the two.
			_, line, err := pktline.ReadLine(sess.Reader())
			require.NoError(t, err)
			assert.NotEmpty(t, line, "server should advertise refs")

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

func TestGitTransport_Archive(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	port := freePort(t)
	base := filepath.Join(t.TempDir(), fmt.Sprintf("git-proto-%d", port))
	_ = test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	startDaemon(t, base, port)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "git",
			Host:   fmt.Sprintf("localhost:%d", port),
			Path:   "/basic.git",
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
