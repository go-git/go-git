// Package test implements common test suite for different transport
// implementations.
//
package test

import (
	"bytes"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

type SendPackSuite struct {
	Endpoint            transport.Endpoint
	EmptyEndpoint       transport.Endpoint
	NonExistentEndpoint transport.Endpoint
	Client              transport.Client
}

func (s *SendPackSuite) TestInfoEmpty(c *C) {
	r, err := s.Client.NewSendPackSession(s.EmptyEndpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()
	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Head, IsNil)
}

func (s *SendPackSuite) TestInfoNotExists(c *C) {
	r, err := s.Client.NewSendPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()
	info, err := r.AdvertisedReferences()
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(info, IsNil)

	r, err = s.Client.NewSendPackSession(s.NonExistentEndpoint)
	c.Assert(err, IsNil)
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"master", plumbing.ZeroHash, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	writer, err := r.SendPack(req)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(writer, IsNil)
}

func (s *SendPackSuite) TestCallAdvertisedReferenceTwice(c *C) {
	r, err := s.Client.NewSendPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	ar1, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar1, NotNil)
	ar2, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(ar2, DeepEquals, ar1)
}

func (s *SendPackSuite) TestDefaultBranch(c *C) {
	r, err := s.Client.NewSendPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	ref, ok := info.References["refs/heads/master"]
	c.Assert(ok, Equals, true)
	c.Assert(ref, Equals, fixtures.Basic().One().Head)
}

func (s *SendPackSuite) TestCapabilities(c *C) {
	r, err := s.Client.NewSendPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	info, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)
	c.Assert(info.Capabilities.Get("agent"), HasLen, 1)
}

func (s *SendPackSuite) TestFullSendPackOnEmpty(c *C) {
	endpoint := s.EmptyEndpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) TestSendPackOnEmpty(c *C) {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) TestSendPackOnEmptyWithReportStatus(c *C) {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	req.Capabilities.Set(capability.ReportStatus)
	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) TestFullSendPackOnNonEmpty(c *C) {
	endpoint := s.Endpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) TestSendPackOnNonEmpty(c *C) {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) TestSendPackOnNonEmptyWithReportStatus(c *C) {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/master", plumbing.ZeroHash, fixture.Head},
	}
	req.Capabilities.Set(capability.ReportStatus)

	s.sendPack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, fixture.Head)
}

func (s *SendPackSuite) sendPack(c *C, ep transport.Endpoint,
	req *packp.ReferenceUpdateRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool) {

	url := ""
	if fixture != nil {
		url = fixture.URL
	}
	comment := Commentf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%s",
		ep.String(), url, callAdvertisedReferences,
	)

	r, err := s.Client.NewSendPackSession(ep)
	c.Assert(err, IsNil, comment)
	defer func() { c.Assert(r.Close(), IsNil, comment) }()

	if callAdvertisedReferences {
		info, err := r.AdvertisedReferences()
		c.Assert(err, IsNil, comment)
		c.Assert(info, NotNil, comment)
	}

	if fixture != nil {
		c.Assert(fixture.Packfile(), NotNil)
		req.Packfile = fixture.Packfile()
	} else {
		req.Packfile = s.emptyPackfile()
	}

	report, err := r.SendPack(req)
	c.Assert(err, IsNil, comment)
	if req.Capabilities.Supports(capability.ReportStatus) {
		c.Assert(report, NotNil, comment)
		c.Assert(report.Ok(), Equals, true, comment)
	} else {
		c.Assert(report, IsNil, comment)
	}
}

func (s *SendPackSuite) checkRemoteHead(c *C, ep transport.Endpoint, head plumbing.Hash) {
	s.checkRemoteReference(c, ep, "refs/heads/master", head)
}

func (s *SendPackSuite) checkRemoteReference(c *C, ep transport.Endpoint,
	refName string, head plumbing.Hash) {

	r, err := s.Client.NewFetchPackSession(ep)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()
	ar, err := r.AdvertisedReferences()
	c.Assert(err, IsNil, Commentf("endpoint: %s", ep.String()))
	ref, ok := ar.References[refName]
	if head == plumbing.ZeroHash {
		c.Assert(ok, Equals, false)
	} else {
		c.Assert(ok, Equals, true)
		c.Assert(ref, DeepEquals, head)
	}
}

func (s *SendPackSuite) TestSendPackAddDeleteReference(c *C) {
	s.testSendPackAddReference(c)
	s.testSendPackDeleteReference(c)
}

func (s *SendPackSuite) testSendPackAddReference(c *C) {
	r, err := s.Client.NewSendPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	fixture := fixtures.Basic().ByTag("packfile").One()

	ar, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)

	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/newbranch", plumbing.ZeroHash, fixture.Head},
	}
	if ar.Capabilities.Supports(capability.ReportStatus) {
		req.Capabilities.Set(capability.ReportStatus)
	}

	s.sendPack(c, s.Endpoint, req, nil, false)
	s.checkRemoteReference(c, s.Endpoint, "refs/heads/newbranch", fixture.Head)
}

func (s *SendPackSuite) testSendPackDeleteReference(c *C) {
	r, err := s.Client.NewSendPackSession(s.Endpoint)
	c.Assert(err, IsNil)
	defer func() { c.Assert(r.Close(), IsNil) }()

	fixture := fixtures.Basic().ByTag("packfile").One()

	ar, err := r.AdvertisedReferences()
	c.Assert(err, IsNil)

	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{"refs/heads/newbranch", fixture.Head, plumbing.ZeroHash},
	}
	if ar.Capabilities.Supports(capability.ReportStatus) {
		req.Capabilities.Set(capability.ReportStatus)
	}

	s.sendPack(c, s.Endpoint, req, nil, false)
	s.checkRemoteReference(c, s.Endpoint, "refs/heads/newbranch", plumbing.ZeroHash)
}

func (s *SendPackSuite) emptyPackfile() io.ReadCloser {
	var buf bytes.Buffer
	e := packfile.NewEncoder(&buf, memory.NewStorage(), false)
	_, err := e.Encode(nil)
	if err != nil {
		panic(err)
	}

	return ioutil.NopCloser(&buf)
}
