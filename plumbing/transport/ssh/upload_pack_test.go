package ssh

import (
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
	var err error
	s.port, err = test.FreePort()
	s.Require().NoError(err)
	s.base = s.T().TempDir()

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	s.Client = NewTransport(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})

	s.Endpoint = newEndpoint(s.T(), s.base, s.port, "basic.git")
	s.EmptyEndpoint = newEndpoint(s.T(), s.base, s.port, "empty.git")
	s.NonExistentEndpoint = newEndpoint(s.T(), s.base, s.port, "non-existent.git")
	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), s.base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), s.base, "empty.git")
	s.Storer = filesystem.NewStorage(basic, nil)
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	startServer(s.T(), s.port, s.opts...)
}

func startServer(t testing.TB, port int, opts ...ssh.Option) *ssh.Server {
	t.Helper()
	server := &ssh.Server{Handler: testutils.HandlerSSH}
	for _, opt := range opts {
		opt(server)
	}
	server.Addr = fmt.Sprintf(":%d", port)
	go func() {
		log.Fatal(server.ListenAndServe())
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
