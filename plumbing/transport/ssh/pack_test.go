package ssh

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"
	stdssh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type sshPackEnv struct {
	Endpoint, EmptyEndpoint, NonExistentEndpoint *url.URL
	Storer, EmptyStorer, NonExistentStorer       storage.Storer
	Transport                                    transport.Transport
}

func setupSSHPackEnv(t testing.TB) sshPackEnv {
	t.Helper()
	addr := startSSHServer(t.(*testing.T))
	base := t.TempDir()

	basicFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(t, fixtures.ByTag("empty").One(), base, "empty.git")

	basicPath := filepath.ToSlash(basicFS.Root())
	emptyPath := filepath.ToSlash(emptyFS.Root())

	host := formatSSHHost(addr)
	return sshPackEnv{
		Endpoint:            &url.URL{Scheme: "ssh", User: url.User("git"), Host: host, Path: basicPath},
		EmptyEndpoint:       &url.URL{Scheme: "ssh", User: url.User("git"), Host: host, Path: emptyPath},
		NonExistentEndpoint: &url.URL{Scheme: "ssh", User: url.User("git"), Host: host, Path: "/nonexistent/repo.git"},
		Storer:              filesystem.NewStorage(basicFS, nil),
		EmptyStorer:         filesystem.NewStorage(emptyFS, nil),
		NonExistentStorer:   memory.NewStorage(),
		Transport: NewTransport(Options{
			ClientConfig: func(_ context.Context, _ *transport.Request) (*stdssh.ClientConfig, error) {
				return &stdssh.ClientConfig{
					User:            "git",
					Auth:            []stdssh.AuthMethod{stdssh.Password("")},
					HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
				}, nil
			},
		}),
	}
}

func formatSSHHost(addr *net.TCPAddr) string {
	return fmt.Sprintf("localhost:%d", addr.Port)
}

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(uploadPackSuite))
}

type uploadPackSuite struct {
	test.UploadPackSuite
}

func (s *uploadPackSuite) SetupTest() {
	env := setupSSHPackEnv(s.T())
	s.Endpoint = env.Endpoint
	s.EmptyEndpoint = env.EmptyEndpoint
	s.NonExistentEndpoint = env.NonExistentEndpoint
	s.Storer = env.Storer
	s.EmptyStorer = env.EmptyStorer
	s.NonExistentStorer = env.NonExistentStorer
	s.Transport = env.Transport
}

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(receivePackSuite))
}

type receivePackSuite struct {
	test.ReceivePackSuite
}

func (s *receivePackSuite) SetupTest() {
	env := setupSSHPackEnv(s.T())
	s.Endpoint = env.Endpoint
	s.EmptyEndpoint = env.EmptyEndpoint
	s.NonExistentEndpoint = env.NonExistentEndpoint
	s.Storer = env.Storer
	s.EmptyStorer = env.EmptyStorer
	s.NonExistentStorer = env.NonExistentStorer
	s.Transport = env.Transport
}
