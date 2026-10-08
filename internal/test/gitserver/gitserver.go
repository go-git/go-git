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

	daemon := gitenv.CommandContext(t.Context(), "git", "daemon",
		fmt.Sprintf("--base-path=%s", basePath),
		"--export-all", "--enable=receive-pack", "--enable=upload-archive", "--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		"--max-connections=1", "--listen=127.0.0.1",
	)
	daemon.Cancel = func() error { return daemon.Process.Signal(os.Interrupt) }
	daemon.WaitDelay = daemonShutdown
	require.NoError(t, daemon.Start())

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
