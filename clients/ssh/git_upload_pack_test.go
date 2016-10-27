package ssh

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
)

type RemoteSuite struct {
	Endpoint common.Endpoint
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) SetUpSuite(c *C) {
	var err error
	s.Endpoint, err = common.NewEndpoint("git@github.com:git-fixtures/basic.git")
	c.Assert(err, IsNil)

	if os.Getenv("SSH_AUTH_SOCK") == "" {
		c.Skip("SSH_AUTH_SOCK is not set")
	}
}

// A mock implementation of client.common.AuthMethod
// to test non ssh auth method detection.
type mockAuth struct{}

func (*mockAuth) Name() string   { return "" }
func (*mockAuth) String() string { return "" }

func (s *RemoteSuite) TestSetAuthWrongType(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.SetAuth(&mockAuth{}), Equals, ErrInvalidAuthMethod)
}

func (s *RemoteSuite) TestAlreadyConnected(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	defer func() {
		c.Assert(r.Disconnect(), IsNil)
	}()

	c.Assert(r.Connect(), Equals, ErrAlreadyConnected)
}

func (s *RemoteSuite) TestDisconnect(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Disconnect(), IsNil)
}

func (s *RemoteSuite) TestDisconnectedWhenNonConnected(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Disconnect(), Equals, ErrNotConnected)
}

func (s *RemoteSuite) TestAlreadyDisconnected(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Disconnect(), IsNil)
	c.Assert(r.Disconnect(), Equals, ErrNotConnected)
}

func (s *RemoteSuite) TestServeralConnections(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Disconnect(), IsNil)

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Disconnect(), IsNil)

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Disconnect(), IsNil)
}

func (s *RemoteSuite) TestInfoNotConnected(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	_, err := r.Info()
	c.Assert(err, Equals, ErrNotConnected)
}

func (s *RemoteSuite) TestDefaultBranch(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	defer func() { c.Assert(r.Disconnect(), IsNil) }()

	info, err := r.Info()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}

func (s *RemoteSuite) TestCapabilities(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	defer func() { c.Assert(r.Disconnect(), IsNil) }()

	info, err := r.Info()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get("agent").Values, HasLen, 1)
}

func (s *RemoteSuite) TestFetchNotConnected(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	pr := &common.GitUploadPackRequest{}
	pr.Want(core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	_, err := r.Fetch(pr)
	c.Assert(err, Equals, ErrNotConnected)
}

func (s *RemoteSuite) TestFetch(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	defer func() { c.Assert(r.Disconnect(), IsNil) }()

	req := &common.GitUploadPackRequest{}
	req.Want(core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Want(core.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
	reader, err := r.Fetch(req)
	c.Assert(err, IsNil)
	defer func() { c.Assert(reader.Close(), IsNil) }()

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Check(len(b), Equals, 85585)
}

func (s *RemoteSuite) TestFetchError(c *C) {
	r := NewGitUploadPackService(s.Endpoint)
	c.Assert(r.Connect(), IsNil)
	defer func() { c.Assert(r.Disconnect(), IsNil) }()

	req := &common.GitUploadPackRequest{}
	req.Want(core.NewHash("1111111111111111111111111111111111111111"))

	_, err := r.Fetch(req)
	c.Assert(err, Not(IsNil))
}
