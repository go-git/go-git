package ssh

import (
	"errors"
	"fmt"
	"log"
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

	port int
	base string
}

func TestUploadPackSuite(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("tcp connections are not available in wasm")
	}
	suite.Run(t, new(UploadPackSuite))
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
	var err error
	port, err = test.FreePort()
	require.NoError(t, err)
	base = t.TempDir()

	sshconfig := &stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	}

	r := &runner{config: sshconfig}
	client = transport.NewPackTransport(r)

	server := startServer(t, port, opts...)
	t.Cleanup(func() {
		server.Close()
	})
	return
}

func startServer(t testing.TB, port int, opts ...ssh.Option) *ssh.Server {
	t.Helper()
	server := &ssh.Server{Handler: testutils.HandlerSSH}
	for _, opt := range opts {
		opt(server)
	}
	server.Addr = fmt.Sprintf(":%d", port)
	go func() {
		err := server.ListenAndServe()
		if !errors.Is(err, ssh.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	return server
}

func newEndpoint(t testing.TB, base string, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", port, filepath.ToSlash(base), name,
	))
	require.NoError(t, err)
	return ep
}
