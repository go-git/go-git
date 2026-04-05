package ssh

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"
	stdssh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	xtest "github.com/go-git/go-git/v6/plumbing/transport/test"
)

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(uploadPackSuite))
}

type uploadPackSuite struct {
	xtest.UploadPackSuite
}

func (s *uploadPackSuite) SetupTest() {
	addr := startSSHServer(s.T())
	base := s.T().TempDir()

	basicFS := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	basicPath := filepath.ToSlash(basicFS.Root())
	emptyPath := filepath.ToSlash(emptyFS.Root())

	s.Endpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: basicPath}
	s.EmptyEndpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: emptyPath}
	s.NonExistentEndpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: "/nonexistent/repo.git"}

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{
		ClientConfig: func(_ context.Context, _ *transport.Request) (*stdssh.ClientConfig, error) {
			return &stdssh.ClientConfig{
				User:            "git",
				Auth:            []stdssh.AuthMethod{stdssh.Password("")},
				HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
			}, nil
		},
	})
}

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(receivePackSuite))
}

type receivePackSuite struct {
	xtest.ReceivePackSuite
}

func (s *receivePackSuite) SetupTest() {
	addr := startSSHServer(s.T())
	base := s.T().TempDir()

	basicFS := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	basicPath := filepath.ToSlash(basicFS.Root())
	emptyPath := filepath.ToSlash(emptyFS.Root())

	s.Endpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: basicPath}
	s.EmptyEndpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: emptyPath}
	s.NonExistentEndpoint = &url.URL{Scheme: "ssh", User: url.User("git"), Host: fmt.Sprintf("localhost:%d", addr.Port), Path: "/nonexistent/repo.git"}

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{
		ClientConfig: func(_ context.Context, _ *transport.Request) (*stdssh.ClientConfig, error) {
			return &stdssh.ClientConfig{
				User:            "git",
				Auth:            []stdssh.AuthMethod{stdssh.Password("")},
				HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
			}, nil
		},
	})
}
