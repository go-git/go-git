package test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

// daemonShutdown is how long the daemon is given to act on the interrupt
// below before exec kills it instead, and so the longest a test can be held
// at waitForShutdown. A daemon that stops when it is asked to, which is the
// ordinary case, is not waited on for any of it.
const daemonShutdown = 5 * time.Second

// daemonStartup bounds the wait for the daemon to accept connections.
const daemonStartup = time.Second

// StartGitDaemon runs git daemon for the rest of t, serving every repository
// under base on 127.0.0.1:port, and returns once the port accepts
// connections. extraArgs are appended to the daemon's command line, for
// example to enable services such as receive-pack.
//
// The test is skipped when git daemon is not installed, as distributions
// that package it apart from git leave it out.
func StartGitDaemon(t testing.TB, base string, port int, extraArgs ...string) {
	t.Helper()
	requireGitDaemon(t)
	args := append([]string{
		"daemon",
		fmt.Sprintf("--base-path=%s", base),
		"--export-all", "--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		"--listen=127.0.0.1",
	}, extraArgs...)

	// Bound to the test's own context, which the testing package cancels
	// before the test's cleanups run, so the daemon ends with the test that
	// started it rather than through a cleanup remembering to end it.
	daemon := gitenv.CommandContext(t.Context(), "git", args...)
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

	ctx, cancel := context.WithTimeout(t.Context(), daemonStartup)
	defer cancel()
	require.NoError(t, waitForPort(ctx, port))
}

// requireGitDaemon skips t unless git can run git daemon. git runs it as the
// git-daemon program, looked up in its exec path and then in PATH, as git's
// setup_path puts the exec path in front of PATH.
func requireGitDaemon(t testing.TB) {
	t.Helper()
	name := "git-daemon"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if out, err := gitenv.Command("git", "--exec-path").Output(); err == nil {
		if _, err := os.Stat(filepath.Join(strings.TrimSpace(string(out)), name)); err == nil {
			return
		}
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("git daemon is not installed: %v", err)
	}
}

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
func waitForShutdown(t testing.TB, cmd *exec.Cmd) {
	t.Helper()

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { <-waited })
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
