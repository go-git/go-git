package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/stretchr/testify/assert"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type CommonSuiteHelper struct {
	base   string
	port   int
	daemon *exec.Cmd
}

func (h *CommonSuiteHelper) Setup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}

	cmd := exec.Command("git", "daemon", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil && bytes.Contains(output, []byte("'daemon' is not a git command")) {
		t.Fatal("git daemon cannot be found")
	}

	h.port, err = freePort()
	assert.NoError(t, err)

	h.base, err = os.MkdirTemp(t.TempDir(), fmt.Sprintf("go-git-protocol-%d", h.port))
	assert.NoError(t, err)

	daemon, err := startGitDaemon(h.base, h.port)
	assert.NoError(t, err)
	h.daemon = daemon
}

func (h *CommonSuiteHelper) TearDown() {
	fixtures.Clean()

	if h.daemon != nil {
		_ = killDaemon(h.daemon)
		_ = h.daemon.Wait()
	}
}

func (h *CommonSuiteHelper) newEndpoint(t *testing.T, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("git://localhost:%d/%s", h.port, name))
	assert.NoError(t, err)

	return ep
}

func (h *CommonSuiteHelper) prepareRepository(t *testing.T, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	assert.NoError(t, err)

	path := filepath.Join(h.base, name)
	assert.NoError(t, os.Rename(fs.Root(), path))

	return h.newEndpoint(t, name)
}

func freePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}

	return l.Addr().(*net.TCPAddr).Port, l.Close()
}

func waitForPort(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return errors.New("context canceled before the port is connectable")
		case <-time.After(10 * time.Millisecond):
			conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
			if err == nil {
				return conn.Close()
			}
		}
	}
}
