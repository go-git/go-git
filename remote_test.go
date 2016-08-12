package git

import (
	"gopkg.in/src-d/go-git.v4/clients/http"
	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

type RemoteSuite struct {
	BaseSuite
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) TestNewAuthenticatedRemote(c *C) {
	a := &http.BasicAuth{}
	r, err := NewAuthenticatedRemote(RepositoryFixture, a)
	c.Assert(err, IsNil)
	c.Assert(r.Auth, Equals, a)
}

func (s *RemoteSuite) TestConnect(c *C) {
	r, err := NewRemote(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
}

func (s *RemoteSuite) TestDefaultBranch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Head().Name(), Equals, core.ReferenceName("refs/heads/master"))
}

func (s *RemoteSuite) TestCapabilities(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Capabilities().Get("agent").Values, HasLen, 1)
}

/*
func (s *RemoteSuite) TestFetch(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Connect(), IsNil)

	req := &common.GitUploadPackRequest{}
	req.Want(r.Head().Hash())

	reader, err := r.Fetch(req)
	c.Assert(err, IsNil)

	packfileReader := packfile.NewStream(reader)
	d := packfile.NewDecoder(packfileReader)

	sto := memory.NewObjectStorage()
	err = d.Decode(sto)
	c.Assert(err, IsNil)
	c.Assert(sto.Objects, HasLen, 28)
}
*/

func (s *RemoteSuite) TestHead(c *C) {
	r, err := NewRemote(RepositoryFixture)
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)

	err = r.Connect()
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)
	c.Assert(r.Head().Hash(), Equals, core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}
