package git

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type BaseSuite struct {
	suite.Suite

	base   string
	port   int
	daemon *exec.Cmd
}

func (s *BaseSuite) TearDownSuite() {
	fixtures.Clean()
}

func (s *BaseSuite) SetupTest() {
	if runtime.GOOS == "windows" {
		s.T().Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}

	cmd := exec.Command("git", "daemon", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil && bytes.Contains(output, []byte("'daemon' is not a git command")) {
		s.T().Fatal("git daemon cannot be found")
	}

	s.port, err = freePort()
	s.NoError(err)

	s.base, err = os.MkdirTemp(s.T().TempDir(), fmt.Sprintf("go-git-protocol-%d", s.port))
	s.NoError(err)
}

func (s *BaseSuite) StartDaemon() {
	s.daemon = exec.Command(
		"git",
		"daemon",
		fmt.Sprintf("--base-path=%s", s.base),
		"--export-all",
		"--enable=receive-pack",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", s.port),
		// Unless max-connections is limited to 1, a git-receive-pack
		// might not be seen by a subsequent operation.
		"--max-connections=1",
	)

	// Environment must be inherited in order to acknowledge GIT_EXEC_PATH if set.
	s.daemon.Env = os.Environ()

	err := s.daemon.Start()
	s.NoError(err)

	// Connections might be refused if we start sending request too early.
	time.Sleep(time.Millisecond * 500)
}

func (s *BaseSuite) newEndpoint(name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("git://localhost:%d/%s", s.port, name))
	s.NoError(err)

	return ep
}

func (s *BaseSuite) prepareRepository(f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	s.NoError(err)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	s.NoError(err)

	return s.newEndpoint(name)
}

func (s *BaseSuite) TearDownTest() {
	if s.daemon != nil {
		_ = s.daemon.Process.Signal(os.Kill)
		_ = s.daemon.Wait()
	}
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
