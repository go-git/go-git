// Package test implements common test suite for different transport
// implementations.
//
package test

import (
	"bytes"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
)

type FetchPackSuite struct {
	Endpoint            transport.Endpoint
	EmptyEndpoint       transport.Endpoint
	NonExistentEndpoint transport.Endpoint
	Client              transport.Client
}

func (s *FetchPackSuite) TestInfoEmpty(c *C) {
	r, err := s.Client.NewFetchPackSession(s.EmptyEndpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrEmptyRemoteRepository)
	c.Assert(info, IsNil)
}

func (s *FetchPackSuite) TestInfoNotExists(c *C) {
	r, err := s.Client.NewFetchPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(info, IsNil)

	r, err = s.Client.NewFetchPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	reader, err := r.FetchPack(req)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(reader, IsNil)
}

func (s *FetchPackSuite) TestCallAdvertisedReferenceTwice(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	ar1, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar1, NotNil)
	ar2, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar2, DeepEquals, ar1)
}

func (s *FetchPackSuite) TestDefaultBranch(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	symrefs := info.Capabilities.Get(capability.SymRef)
	c.Assert(symrefs, HasLen, 1)
	c.Assert(symrefs[0], Equals, "HEAD:refs/heads/master")
}

func (s *FetchPackSuite) TestAdvertisedReferencesFilterUnsupported(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Supports(capability.MultiACK), Equals, false)
}

func (s *FetchPackSuite) TestCapabilities(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get(capability.Agent), HasLen, 1)
}

func (s *FetchPackSuite) TestFullFetchPack(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	s.checkObjectNumber(c, reader, 28)
}

func (s *FetchPackSuite) TestFetchPack(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	s.checkObjectNumber(c, reader, 28)
}

func (s *FetchPackSuite) TestFetchPackInvalidReq(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Capabilities.Set(capability.Sideband)
	req.Capabilities.Set(capability.Sideband64k)

	_, err = r.FetchPack(req)
	c.Assert(err, NotNil)
}

func (s *FetchPackSuite) TestFetchPackNoChanges(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.FetchPack(req)
	c.Assert(err, Equals, transport.ErrEmptyUploadPackRequest)
	c.Assert(reader, IsNil)
}

func (s *FetchPackSuite) TestFetchPackMulti(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	reader, err := r.FetchPack(req)
	c.Assert(err, IsNil)

	s.checkObjectNumber(c, reader, 31)
}

func (s *FetchPackSuite) TestFetchError(c *C) {
	r, err := s.Client.NewFetchPackSession(s.Endpoint)
	c.Assert(err, IsNil)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	reader, err := r.FetchPack(req)
	c.Assert(err, Equals, transport.ErrEmptyUploadPackRequest)
	c.Assert(reader, IsNil)

	//XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}

func (s *FetchPackSuite) checkObjectNumber(c *C, r io.Reader, n int) {
	b, err := ioutil.ReadAll(r)
	c.Assert(err, IsNil)
	buf := bytes.NewBuffer(b)
	scanner := packfile.NewScanner(buf)
	storage := memory.NewStorage()
	d, err := packfile.NewDecoder(scanner, storage)
	c.Assert(err, IsNil)
	_, err = d.Decode()
	c.Assert(err, IsNil)
	c.Assert(len(storage.Objects), Equals, n)
}
