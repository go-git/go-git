package file

import (
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type filePackEnv struct {
	Endpoint, EmptyEndpoint, NonExistentEndpoint *url.URL
	Storer, EmptyStorer, NonExistentStorer       storage.Storer
	Transport                                    transport.Transport
}

func setupFilePackEnv(t testing.TB) filePackEnv {
	t.Helper()
	base := t.TempDir()

	basicFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(t, fixtures.ByTag("empty").One(), base, "empty.git")

	basicPath, err := filepath.Abs(basicFS.Root())
	if err != nil {
		t.Fatal(err)
	}
	emptyPath, err := filepath.Abs(emptyFS.Root())
	if err != nil {
		t.Fatal(err)
	}

	return filePackEnv{
		Endpoint:            &url.URL{Scheme: "file", Path: basicPath},
		EmptyEndpoint:       &url.URL{Scheme: "file", Path: emptyPath},
		NonExistentEndpoint: &url.URL{Scheme: "file", Path: "/nonexistent/repo.git"},
		Storer:              filesystem.NewStorage(basicFS, nil),
		EmptyStorer:         filesystem.NewStorage(emptyFS, nil),
		NonExistentStorer:   memory.NewStorage(),
		Transport:           NewTransport(Options{}),
	}
}

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(uploadPackSuite))
}

type uploadPackSuite struct {
	test.UploadPackSuite
}

func (s *uploadPackSuite) SetupTest() {
	env := setupFilePackEnv(s.T())
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
	env := setupFilePackEnv(s.T())
	s.Endpoint = env.Endpoint
	s.EmptyEndpoint = env.EmptyEndpoint
	s.NonExistentEndpoint = env.NonExistentEndpoint
	s.Storer = env.Storer
	s.EmptyStorer = env.EmptyStorer
	s.NonExistentStorer = env.NonExistentStorer
	s.Transport = env.Transport
}
