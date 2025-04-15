package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/stretchr/testify/require"
)

func newEndpoint(t testing.TB, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("git://localhost:%d/%s", port, name))
	require.NoError(t, err)
	return ep
}

func setupTest(t testing.TB) (base string, port int) {
	var err error
	port, err = test.FreePort()
	require.NoError(t, err)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-protocol-%d", port))
	return
}

func startDaemon(t testing.TB, base string, port int) *exec.Cmd {
	t.Helper()
	daemon := exec.Command(
		"git",
		"daemon",
		fmt.Sprintf("--base-path=%s", base),
		"--export-all",
		"--enable=receive-pack",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		// Unless max-connections is limited to 1, a git-receive-pack
		// might not be seen by a subsequent operation.
		"--max-connections=1",
	)

	// Environment must be inherited in order to acknowledge GIT_EXEC_PATH if set.
	daemon.Env = os.Environ()

	require.NoError(t, daemon.Start())

	// Connections might be refused if we start sending request too early.
	time.Sleep(time.Millisecond * 500)

	return daemon
}

func stopDaemon(t testing.TB, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("daemon is nil")
		return
	}
	if cmd.Process == nil {
		t.Fatal("daemon process has not started")
		return
	}

	// XXX: We signal the process to terminate gracefully and kill any
	// remaining child processes.
	// Using [os.Process.Kill] won't work here because it won't terminate
	// the child processes.
	cmd.Process.Signal(os.Interrupt) //nolint:errcheck
}
