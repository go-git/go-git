package git

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type gitPackEnv struct {
	Endpoint, EmptyEndpoint, NonExistentEndpoint *url.URL
	Storer, EmptyStorer, NonExistentStorer       storage.Storer
	Transport                                    transport.Transport
}

func setupGitPackEnv(t testing.TB) gitPackEnv {
	t.Helper()
	port := freePort(t.(*testing.T))
	base := filepath.Join(t.TempDir(), fmt.Sprintf("git-proto-%d", port))

	basicFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(t, fixtures.ByTag("empty").One(), base, "empty.git")

	startDaemon(t.(*testing.T), base, port)

	storer := filesystem.NewStorage(basicFS, nil)
	t.Cleanup(func() { _ = storer.Close() })
	emptyStorer := filesystem.NewStorage(emptyFS, nil)
	t.Cleanup(func() { _ = emptyStorer.Close() })

	host := fmt.Sprintf("localhost:%d", port)
	return gitPackEnv{
		Endpoint:            &url.URL{Scheme: "git", Host: host, Path: "/basic.git"},
		EmptyEndpoint:       &url.URL{Scheme: "git", Host: host, Path: "/empty.git"},
		NonExistentEndpoint: &url.URL{Scheme: "git", Host: host, Path: "/non-existent.git"},
		Storer:              storer,
		EmptyStorer:         emptyStorer,
		NonExistentStorer:   memory.NewStorage(),
		Transport:           NewTransport(Options{}),
	}
}

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}
	suite.Run(t, new(uploadPackSuite))
}

type uploadPackSuite struct {
	test.UploadPackSuite
}

func (s *uploadPackSuite) SetupTest() {
	env := setupGitPackEnv(s.T())
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
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}
	suite.Run(t, new(receivePackSuite))
}

type receivePackSuite struct {
	test.ReceivePackSuite
}

func (s *receivePackSuite) SetupTest() {
	env := setupGitPackEnv(s.T())
	s.Endpoint = env.Endpoint
	s.EmptyEndpoint = env.EmptyEndpoint
	s.NonExistentEndpoint = env.NonExistentEndpoint
	s.Storer = env.Storer
	s.EmptyStorer = env.EmptyStorer
	s.NonExistentStorer = env.NonExistentStorer
	s.Transport = env.Transport
}
