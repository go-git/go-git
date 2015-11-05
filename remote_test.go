package git

import (
	"gopkg.in/src-d/go-git.v2/clients/http"
	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/formats/packfile"

	. "gopkg.in/check.v1"
)

type SuiteRemote struct{}

var _ = Suite(&SuiteRemote{})

const RepositoryFixture = "https://github.com/tyba/git-fixture"

func (s *SuiteRemote) TestNewAuthenticatedRemote(c *C) {
	auth := &http.BasicAuth{}
	r, err := NewAuthenticatedRemote(RepositoryFixture, auth)
	c.Assert(err, IsNil)
	c.Assert(r.Auth, Equals, auth)
}

func (s *SuiteRemote) TestConnect(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
}

func (s *SuiteRemote) TestDefaultBranch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.DefaultBranch(), Equals, "refs/heads/master")
}

func (s *SuiteRemote) TestCapabilities(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Capabilities().Get("agent").Values, HasLen, 1)
}

func (s *SuiteRemote) TestFetchDefaultBranch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)

	reader, err := r.FetchDefaultBranch()
	c.Assert(err, IsNil)

	pr := packfile.NewReader(reader)

	storage := core.NewRAWObjectStorage()
	_, err = pr.Read(storage)
	c.Assert(err, IsNil)
	c.Assert(storage.Objects, HasLen, 28)
}
