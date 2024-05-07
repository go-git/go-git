// Package test implements common test suite for different transport
// implementations.
package test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	Storer              storage.Storer
	Endpoint            *transport.Endpoint
	EmptyStorer         storage.Storer
	EmptyEndpoint       *transport.Endpoint
	NonExistentStorer   storage.Storer
	NonExistentEndpoint *transport.Endpoint
	EmptyAuth           transport.AuthMethod
	Client              transport.Transport
}

func (s *ReceivePackSuite) TestAdvertisedReferencesEmpty(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.EmptyEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()
	refs, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(len(refs), Equals, 0)
}

func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(conn.Close(), IsNil)

	r, err = s.Client.NewSession(s.Storer, s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "master", Old: plumbing.ZeroHash, New: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		},
	}
	err = conn.Push(context.TODO(), req)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)
	c.Assert(conn.Close(), IsNil)
}

func (s *ReceivePackSuite) TestCallAdvertisedReferenceTwice(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()
	refs1, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(len(refs1), Not(Equals), 0)
	refs2, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(len(refs2), Not(Equals), 0)
	c.Assert(err, IsNil)
	c.Assert(refs1, DeepEquals, refs2)
}

func (s *ReceivePackSuite) TestDefaultBranch(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()
	refs, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(err, IsNil)
	headExists := false
	for _, ref := range refs {
		if ref.String() == fixtures.Basic().One().Head {
			headExists = true
			break
		}
	}
	c.Assert(headExists, Equals, true)
}

func (s *ReceivePackSuite) TestCapabilities(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()
	caps := conn.Capabilities()
	c.Assert(caps.Get("agent"), HasLen, 1)
}

func (s *ReceivePackSuite) TestFullSendPackOnEmpty(c *C) {
	endpoint := s.EmptyEndpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}
	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackWithContext(c *C) {
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Packfile: fixture.Packfile(),
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}

	r, err := s.Client.NewSession(s.Storer, s.EmptyEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := r.Handshake(context.TODO(), true)
	defer func() { c.Assert(conn.Close(), IsNil) }()
	c.Assert(err, IsNil)

	ctx, close := context.WithCancel(context.TODO())
	close()

	err = conn.Push(ctx, req)
	c.Assert(err, NotNil)
}

func (s *ReceivePackSuite) TestSendPackOnEmpty(c *C) {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}
	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnEmptyWithReportStatus(c *C) {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{Commands: []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}}
	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestFullSendPackOnNonEmpty(c *C) {
	endpoint := s.Endpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{Commands: []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}}
	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmpty(c *C) {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{Commands: []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}}
	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatus(c *C) {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
		},
	}

	s.receivePack(c, endpoint, req, fixture, full)
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatusWithError(c *C) {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}

	err := s.receivePackNoCheck(c, endpoint, req, fixture, full)
	// XXX: Recent git versions return "failed to update ref", while older
	//     (>=1.9) return "failed to lock".
	c.Assert(err, ErrorMatches, ".*(failed to update ref|failed to lock).*")
	s.checkRemoteHead(c, endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) receivePackNoCheck(c *C, ep *transport.Endpoint,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) error {
	url := ""
	if fixture != nil {
		url = fixture.URL
	}
	comment := Commentf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%s",
		ep.String(), url, callAdvertisedReferences,
	)

	// Set write permissions to endpoint directory files. By default
	// fixtures are generated with read only permissions, this casuses
	// errors deleting or modifying files.
	rootPath := ep.Path
	stat, err := os.Stat(ep.Path)

	if rootPath != "" && err == nil && stat.IsDir() {
		objectPath := filepath.Join(rootPath, "objects/pack")
		files, err := os.ReadDir(objectPath)
		c.Assert(err, IsNil)

		for _, file := range files {
			path := filepath.Join(objectPath, file.Name())
			err = os.Chmod(path, 0644)
			c.Assert(err, IsNil)
		}
	}

	r, err := s.Client.NewSession(s.Storer, ep, s.EmptyAuth)
	c.Assert(err, IsNil, comment)

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil, comment)
	c.Assert(conn, NotNil, comment)
	defer func() { c.Assert(conn.Close(), IsNil, comment) }()

	if fixture != nil {
		c.Assert(fixture.Packfile(), NotNil)
		req.Packfile = fixture.Packfile()
	} else {
		req.Packfile = s.emptyPackfile()
	}

	return conn.Push(context.Background(), req)
}

func (s *ReceivePackSuite) receivePack(c *C, ep *transport.Endpoint,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) {
	url := ""
	if fixture != nil {
		url = fixture.URL
	}

	comment := Commentf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%s",
		ep.String(), url, callAdvertisedReferences,
	)
	err := s.receivePackNoCheck(c, ep, req, fixture, callAdvertisedReferences)
	c.Assert(err, IsNil, comment)
}

func (s *ReceivePackSuite) checkRemoteHead(c *C, ep *transport.Endpoint, head plumbing.Hash) {
	s.checkRemoteReference(c, ep, "refs/heads/master", head)
}

func (s *ReceivePackSuite) checkRemoteReference(c *C, ep *transport.Endpoint,
	refName string, head plumbing.Hash,
) {
	r, err := s.Client.NewSession(s.Storer, ep, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.TODO(), false)
	c.Assert(err, IsNil, Commentf("endpoint: %s", ep.String()))
	defer func() { c.Assert(conn.Close(), IsNil) }()
	refs, err := conn.GetRemoteRefs(context.TODO())
	var ok bool
	var ref *plumbing.Reference
	for _, r := range refs {
		if r.String() == refName {
			ok = true
			ref = r
			break
		}
	}
	if head == plumbing.ZeroHash {
		c.Assert(ok, Equals, false)
	} else {
		c.Assert(ok, Equals, true)
		c.Assert(ref, DeepEquals, head)
	}
}

func (s *ReceivePackSuite) TestSendPackAddDeleteReference(c *C) {
	s.testSendPackAddReference(c)
	s.testSendPackDeleteReference(c)
}

func (s *ReceivePackSuite) testSendPackAddReference(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	fixture := fixtures.Basic().ByTag("packfile").One()

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)

	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/newbranch", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}

	c.Assert(conn.Close(), IsNil)
	s.receivePack(c, s.Endpoint, req, nil, false)
	s.checkRemoteReference(c, s.Endpoint, "refs/heads/newbranch", plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) testSendPackDeleteReference(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	fixture := fixtures.Basic().ByTag("packfile").One()

	conn, err := r.Handshake(context.TODO(), true)
	c.Assert(err, IsNil)

	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/newbranch", Old: plumbing.NewHash(fixture.Head), New: plumbing.ZeroHash},
		},
	}

	caps := conn.Capabilities()
	if !caps.Supports(capability.DeleteRefs) {
		c.Fatal("capability delete-refs not supported")
	}

	c.Assert(conn.Close(), IsNil)

	s.receivePack(c, s.Endpoint, req, nil, false)
	s.checkRemoteReference(c, s.Endpoint, "refs/heads/newbranch", plumbing.ZeroHash)
}

func (s *ReceivePackSuite) emptyPackfile() io.ReadCloser {
	var buf bytes.Buffer
	e := packfile.NewEncoder(&buf, memory.NewStorage(), false)
	_, err := e.Encode(nil, 10)
	if err != nil {
		panic(err)
	}

	return io.NopCloser(&buf)
}
