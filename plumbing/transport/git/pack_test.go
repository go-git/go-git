package git

import (
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
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
	port := freePort(s.T())
	base := filepath.Join(s.T().TempDir(), fmt.Sprintf("git-proto-%d", port))

	basicFS := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	startDaemon(s.T(), base, port)

	s.Endpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/basic.git"}
	s.EmptyEndpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/empty.git"}
	s.NonExistentEndpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/non-existent.git"}

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{})
}

func TestReceivePackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(receivePackSuite))
}

type receivePackSuite struct {
	xtest.ReceivePackSuite
}

func (s *receivePackSuite) SetupTest() {
	port := freePort(s.T())
	base := filepath.Join(s.T().TempDir(), fmt.Sprintf("git-proto-%d", port))

	basicFS := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	startDaemon(s.T(), base, port)

	s.Endpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/basic.git"}
	s.EmptyEndpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/empty.git"}
	s.NonExistentEndpoint = &url.URL{Scheme: "git", Host: fmt.Sprintf("localhost:%d", port), Path: "/non-existent.git"}

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{})
}
