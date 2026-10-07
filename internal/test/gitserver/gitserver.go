// Package gitserver starts a real git daemon for tests: a real upload-pack
// speaking the real wire protocol, on the model of plumbing/transport/git's
// own tests. The daemon is bound to the test's context, so it ends with the
// test that started it.
package gitserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/internal/transport/test"
)

// daemonShutdown is how long the daemon is given to act on the interrupt
// below before exec kills it instead, and so the longest a test can be held
// at reaping. A daemon that stops when it is asked to, which is the ordinary
// case, is not waited on for any of it.
const daemonShutdown = 5 * time.Second

// Start starts a git daemon serving every repository below basePath, and
// returns the URL prefix clients use to reach them (git://127.0.0.1:<port>/).
func Start(t *testing.T, basePath string) string {
	t.Helper()
	port, err := test.FreePort()
	require.NoError(t, err)

	// Bound to the test's own context, which the testing package cancels
	// before the test's cleanups run, so the daemon ends with the test that
	// started it rather than through a cleanup remembering to end it.
	daemon := gitenv.CommandContext(t.Context(), "git", "daemon",
		fmt.Sprintf("--base-path=%s", basePath),
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

	// Wait runs in the background from the moment the command starts, and
	// the cleanup joins it: killing is not reaping, and without a Wait the
	// killed process stays a zombie until the test binary exits.
	waited := make(chan error, 1)
	go func() { waited <- daemon.Wait() }()
	t.Cleanup(func() { <-waited })

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, waitForPort(ctx, port))
	return fmt.Sprintf("git://127.0.0.1:%d/", port)
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
