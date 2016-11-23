package http

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	Endpoint transport.Endpoint
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	fmt.Println("SetUpSuite\n")
	var err error
	s.Endpoint, err = transport.NewEndpoint("https://github.com/git-fixtures/basic")
	c.Assert(err, IsNil)
}

func (s *FetchPackSuite) TestInfoEmpty(c *C) {
	endpoint, _ := transport.NewEndpoint("https://github.com/git-fixture/empty")
	r, err := DefaultClient.NewFetchPackSession(endpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrAuthorizationRequired)
	c.Assert(info, IsNil)
}

//TODO: Test this error with HTTP BasicAuth too.
func (s *FetchPackSuite) TestInfoNotExists(c *C) {
	endpoint, _ := transport.NewEndpoint("https://github.com/git-fixture/not-exists")
	r, err := DefaultClient.NewFetchPackSession(endpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrAuthorizationRequired)
	c.Assert(info, IsNil)
}

func (s *FetchPackSuite) TestDefaultBranch(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}

func (s *FetchPackSuite) TestCapabilities(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get("agent").Values, HasLen, 1)
}

func (s *FetchPackSuite) TestFullFetchPack(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(b, HasLen, 85374)
}

func (s *FetchPackSuite) TestFetchPack(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(b, HasLen, 85374)
}

func (s *FetchPackSuite) TestFetchPackNoChanges(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Have(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, Equals, transport.ErrEmptyUploadPackRequest)
	c.Assert(reader, IsNil)
}

func (s *FetchPackSuite) TestFetchPackMulti(c *C) {
	r, err := DefaultClient.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)

	req := &transport.UploadPackRequest{}
	req.Want(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Want(plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	b, err := ioutil.ReadAll(reader)
	c.Assert(err, IsNil)
	c.Assert(b, HasLen, 85585)
}
