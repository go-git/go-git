package ssh

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	stdssh "golang.org/x/crypto/ssh"
)

type UploadPackSuite struct {
	test.UploadPackSuite
	opts []ssh.Option

	authBuilder func(string) (AuthMethod, error)
	port        int
	base        string
}

func TestUploadPackSuite(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("tcp connections are not available in wasm")
	}
	suite.Run(t, new(UploadPackSuite))
}

func (s *UploadPackSuite) SetupSuite() {
	s.authBuilder = DefaultAuthBuilder
	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}
}

func (s *UploadPackSuite) TearDownSuite() {
	DefaultAuthBuilder = s.authBuilder
}

func (s *UploadPackSuite) SetupTest() {
	s.base, s.port, s.Client = setupTest(s.T(), s.opts...)
	s.Endpoint = newEndpoint(s.T(), s.base, s.port, "basic.git")
	s.EmptyEndpoint = newEndpoint(s.T(), s.base, s.port, "empty.git")
	s.NonExistentEndpoint = newEndpoint(s.T(), s.base, s.port, "non-existent.git")
	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), s.base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), s.base, "empty.git")
	s.Storer = filesystem.NewStorage(basic, nil)
	s.EmptyStorer = filesystem.NewStorage(empty, nil)
}

func setupTest(t testing.TB, opts ...ssh.Option) (base string, port int, client transport.Transport) {
	sshconfig := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}

	r := &runner{config: sshconfig}
	client = transport.NewPackTransport(r)

	addr := startServer(t, opts...)

	base = t.TempDir()

	return base, addr.Port, client
}

func startServer(t testing.TB, opts ...ssh.Option) *net.TCPAddr {
	t.Helper()

	l := test.ListenTCP(t)

	server := &ssh.Server{Handler: handlerSSH}
	for _, opt := range opts {
		opt(server)
	}

	done := make(chan struct{})

	go func() {
		defer func() { close(done) }()
		require.ErrorIs(t, server.Serve(l), net.ErrClosed)
	}()

	t.Cleanup(func() {
		// server.Serve(l) tracks the given listener, and server.Close() closes all tracked listeners.
		// If the test finishes too early and calls server.Close() before the listener is tracked,
		// server.Serve() may hang. Therefore, we should close the listener directly.
		require.NoError(t, l.Close())
		<-done
	})

	return l.Addr().(*net.TCPAddr)
}

func newEndpoint(t testing.TB, base string, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", port, filepath.ToSlash(base), name,
	))
	require.NoError(t, err)
	return ep
}

func handlerSSH(s ssh.Session) {
	cmd, stdin, stderr, stdout, err := buildCommand(s.Command())
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		defer stdin.Close()
		io.Copy(stdin, s)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(s.Stderr(), stderr)
	}()

	go func() {
		defer wg.Done()
		io.Copy(s, stdout)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return
	}

}

func buildCommand(c []string) (cmd *exec.Cmd, stdin io.WriteCloser, stderr, stdout io.ReadCloser, err error) {
	if len(c) != 2 {
		err = fmt.Errorf("invalid command")
		return
	}

	// fix for Windows environments
	var path string
	if runtime.GOOS == "windows" {
		path = strings.Replace(c[1], "/C:", "C:", 1)
	} else {
		path = c[1]
	}

	cmd = exec.Command(c[0], path)
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	stdin, err = cmd.StdinPipe()
	if err != nil {
		return
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	return
}
