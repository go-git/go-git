package git

import (
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

type RemoteSuite struct {
	BaseSuite
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) TestConnect(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})

	err := r.Connect()
	c.Assert(err, IsNil)
}

func (s *RemoteSuite) TestnewRemoteInvalidEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux"})

	err := r.Connect()
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestnewRemoteInvalidSchemaEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})

	err := r.Connect()
	c.Assert(err, NotNil)
}

/*
func (s *RemoteSuite) TestNewAuthenticatedRemote(c *C) {
	a := &http.BasicAuth{}
	r, err := NewAuthenticatedRemote("foo", RepositoryFixture, a)
	c.Assert(err, IsNil)
	c.Assert(r.Name, Equals, "foo")
	c.Assert(r.Endpoint.String(), Equals, RepositoryFixture)
	c.Assert(r.Auth, Equals, a)
}*/

func (s *RemoteSuite) TestInfo(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Info(), IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Info(), NotNil)
	c.Assert(r.Info().Capabilities.Get("ofs-delta"), NotNil)
}

func (s *RemoteSuite) TestDefaultBranch(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Head().Name(), Equals, core.ReferenceName("refs/heads/master"))
}

func (s *RemoteSuite) TestCapabilities(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Capabilities().Get("agent").Values, HasLen, 1)
}

func (s *RemoteSuite) TestFetch(c *C) {
	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)

	err := r.Fetch(&RemoteFetchOptions{
		RefSpecs: []config.RefSpec{config.DefaultRefSpec},
	})

	c.Assert(err, IsNil)
	c.Assert(sto.ObjectStorage().(*memory.ObjectStorage).Objects, HasLen, 28)
}

func (s *RemoteSuite) TestHead(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)
	c.Assert(r.Head().Hash(), Equals, core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *RemoteSuite) TestRef(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)

	ref, err := r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, core.HEAD)

	ref, err = r.Ref(core.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, core.ReferenceName("refs/heads/master"))
}

func (s *RemoteSuite) TestRefs(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)
	c.Assert(r.Refs(), NotNil)
}
