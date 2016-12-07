package http

import (
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	. "gopkg.in/check.v1"
)

type FetchPackSuite struct {
	test.FetchPackSuite
}

var _ = Suite(&FetchPackSuite{})

func (s *FetchPackSuite) SetUpSuite(c *C) {
	s.FetchPackSuite.Client = DefaultClient

	ep, err := transport.NewEndpoint("https://github.com/git-fixtures/basic.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.Endpoint = ep

	ep, err = transport.NewEndpoint("https://github.com/git-fixtures/empty.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.EmptyEndpoint = ep

	ep, err = transport.NewEndpoint("https://github.com/git-fixtures/non-existent.git")
	c.Assert(err, IsNil)
	s.FetchPackSuite.NonExistentEndpoint = ep
}

func (s *FetchPackSuite) TestInfoNotExists(c *C) {
	r, err := s.Client.NewFetchPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrAuthorizationRequired)
	c.Assert(info, IsNil)
}

func (s *FetchPackSuite) TestuploadPackRequestToReader(c *C) {
	r := packp.NewUploadPackRequest()
	r.Wants = append(r.Wants, plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Wants = append(r.Wants, plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Haves = append(r.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	sr, err := uploadPackRequestToReader(r)
	c.Assert(err, IsNil)
	b, _ := ioutil.ReadAll(sr)
	c.Assert(string(b), Equals,
		"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
			"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n0000"+
			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"+
			"0009done\n",
	)
}
