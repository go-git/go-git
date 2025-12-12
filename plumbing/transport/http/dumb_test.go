package http

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/trace"
	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

// The dumb http transport only supports git-upload-pack service so there's no
// need to test the receive-pack service.

type DumbSuite struct {
	test.UploadPackSuite
}

func TestDumbSuite(t *testing.T) {
	t.Parallel()
	trace.ReadEnv()
	suite.Run(t, new(DumbSuite))
}

func (s *DumbSuite) SetupTest() {
	base, port := setupServer(s.T(), false)

	s.Client = NewTransport(&TransportOptions{
		// Set to true to use the dumb transport.
		UseDumb: true,
	})

	basic := test.PrepareRepository(s.T(), fixtures.Basic().One(), base, "basic.git")
	empty := test.PrepareRepository(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = newEndpoint(s.T(), port, "basic.git")
	s.Storer = filesystem.NewStorage(basic, nil)

	s.EmptyEndpoint = newEndpoint(s.T(), port, "empty.git")
	s.EmptyStorer = filesystem.NewStorage(empty, nil)

	s.NonExistentEndpoint = newEndpoint(s.T(), port, "non-existent.git")
	s.NonExistentStorer = memory.NewStorage()

	err := transport.UpdateServerInfo(s.Storer, basic)
	s.Require().NoError(err)
	err = transport.UpdateServerInfo(s.EmptyStorer, empty)
	s.Require().NoError(err)
}

// The following tests are not applicable to the dumb transport as it does not
// support reference and capability advertisement.

func (*DumbSuite) TestDefaultBranch()                         {}
func (*DumbSuite) TestAdvertisedReferencesFilterUnsupported() {}
func (*DumbSuite) TestCapabilities()                          {}
func (*DumbSuite) TestUploadPack()                            {}
func (*DumbSuite) TestUploadPackFull()                        {}
func (*DumbSuite) TestUploadPackInvalidReq()                  {}
func (*DumbSuite) TestUploadPackMulti()                       {}
func (*DumbSuite) TestUploadPackNoChanges()                   {}
func (*DumbSuite) TestUploadPackPartial()                     {}
