package git

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func newEndpoint(t testing.TB, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("git://localhost:%d/%s", port, name))
	require.NoError(t, err)
	return ep
}

// suiteSetup holds the common setup values for git transport test suites.
type suiteSetup struct {
	Endpoint            *transport.Endpoint
	EmptyEndpoint       *transport.Endpoint
	NonExistentEndpoint *transport.Endpoint
	Storer              *filesystem.Storage
	EmptyStorer         *filesystem.Storage
	Client              transport.Transport
	Daemon              *exec.Cmd
}

// setupSuite creates the common test setup for git transport tests.
func setupSuite(t testing.TB) *suiteSetup {
	base, port := setupTest(t)

	basic := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(t, fixtures.ByTag("empty").One(), base, "empty.git")

	return &suiteSetup{
		Endpoint:            newEndpoint(t, port, "basic.git"),
		EmptyEndpoint:       newEndpoint(t, port, "empty.git"),
		NonExistentEndpoint: newEndpoint(t, port, "non-existent.git"),
		Storer:              filesystem.NewStorage(basic, nil),
		EmptyStorer:         filesystem.NewStorage(empty, nil),
		Client:              DefaultClient,
		Daemon:              startDaemon(t, base, port),
	}
}

func setupTest(t testing.TB) (base string, port int) {
	var err error
	port, err = test.FreePort()
	require.NoError(t, err)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-protocol-%d", port))
	return base, port
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
		"--listen=127.0.0.1",
	)

	// Environment must be inherited in order to acknowledge GIT_EXEC_PATH if set.
	daemon.Env = os.Environ()

	require.NoError(t, daemon.Start())

	// Wait until daemon is ready.
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()

	assert.NoError(t, waitForPort(ctx, port))

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
	cmd.Process.Signal(os.Interrupt)
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
