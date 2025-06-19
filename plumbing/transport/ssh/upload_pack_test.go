package ssh

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	testutils "github.com/go-git/go-git/v6/internal/transport/ssh/test"
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
	s.base, s.port, s.Client = setupTest(s.T())
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

	base, err := os.MkdirTemp(t.TempDir(), fmt.Sprintf("go-git-ssh-%d", addr.Port))
	require.NoError(t, err)

	return base, addr.Port, client
}

func startServer(t testing.TB, opts ...ssh.Option) *net.TCPAddr {
	t.Helper()

	l := test.ListenTCP(t)

	server := &ssh.Server{Handler: testutils.HandlerSSH}
	for _, opt := range opts {
		opt(server)
	}

	go func() {
		require.ErrorIs(t, server.Serve(l), ssh.ErrServerClosed)
	}()

	t.Cleanup(func() { require.NoError(t, server.Close()) })

	return l.Addr().(*net.TCPAddr)
}

func newEndpoint(t testing.TB, base string, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", port, filepath.ToSlash(base), name,
	))
	require.NoError(t, err)
	return ep
}
