package ssh

import (
	"io/ioutil"
	"os"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	Endpoint transport.Endpoint
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	var err error
	s.Endpoint, err = transport.NewEndpoint("git@github.com:git-fixtures/basic.git")
	c.Assert(err, IsNil)

	if os.Getenv("SSH_AUTH_SOCK") == "" {
		c.Skip("SSH_AUTH_SOCK is not set")
	}
}

func (s *FetchPackSuite) TestDefaultBranch(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}

func (s *FetchPackSuite) TestCapabilities(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get("agent").Values, HasLen, 1)
}

func (s *FetchPackSuite) TestFullFetchPack(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	_, err = r.AdvertisedReferences()
	c.Assert(err, IsNil)

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Want(plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	defer func() { c.Assert(reader.Close(), IsNil) }()

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Check(len(b), Equals, 85585)
}

func (s *FetchPackSuite) TestFetchPack(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Want(plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)
	defer func() { c.Assert(reader.Close(), IsNil) }()

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Check(len(b), Equals, 85585)
}

func (s *FetchPackSuite) TestFetchError(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("1111111111111111111111111111111111111111"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	err = reader.Close()
	c.Assert(err, Not(IsNil))
}
