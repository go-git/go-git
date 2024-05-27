// Package test implements common test suite for different transport
// implementations.
package test

import (
	"context"
	"io"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	Storer              storage.Storer
	Endpoint            *transport.Endpoint
	EmptyStorer         storage.Storer
	EmptyEndpoint       *transport.Endpoint
	NonExistentStorer   storage.Storer
	NonExistentEndpoint *transport.Endpoint
	EmptyAuth           transport.AuthMethod
	Client              transport.Transport
}

func (s *UploadPackSuite) TestAdvertisedReferencesEmpty(c *C) {
	r, err := s.Client.NewSession(s.EmptyStorer, s.EmptyEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	_, err = r.Handshake(context.TODO(), false)
	c.Assert(err, Equals, transport.ErrEmptyRemoteRepository)
}

func (s *UploadPackSuite) TestAdvertisedReferencesNotExists(c *C) {
	r, err := s.Client.NewSession(s.EmptyStorer, s.NonExistentEndpoint, s.EmptyAuth)
	c.Assert(err, IsNil)

	conn, err := r.Handshake(context.TODO(), false)
	// TODO: assert error type
	// It can be different for each transport. For example, the ssh transport
	// returns a RemoteError that comes from the Stderr channel. However, the
	// git transport in the other hand, doesn't have a Stderr channel and rely
	// on pkt error line.
	c.Assert(err, NotNil)
	c.Assert(conn, IsNil)
}

func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice(c *C) {
	r, err := s.Client.NewSession(memory.NewStorage(), s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	ar1, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(err, IsNil)
	c.Assert(ar1, NotNil)
	ar2, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(err, IsNil)
	c.Assert(ar2, DeepEquals, ar1)
}

func (s *UploadPackSuite) TestDefaultBranch(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	caps := conn.Capabilities()
	c.Assert(err, IsNil)
	symrefs := caps.Get(capability.SymRef)
	c.Assert(symrefs, HasLen, 1)
	c.Assert(symrefs[0], Equals, "HEAD:refs/heads/master")
}

func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported(c *C) {
	c.Skip("MultiACK is already not supported by the server side")

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	caps := conn.Capabilities()
	c.Assert(caps.Supports(capability.MultiACK), Equals, false)
}

func (s *UploadPackSuite) TestCapabilities(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	caps := conn.Capabilities()
	c.Assert(caps.Get(capability.Agent), HasLen, 1)
}

func (s *UploadPackSuite) TestUploadPack(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, IsNil)
}

func (s *UploadPackSuite) TestUploadPackWithContext(c *C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	_, err = r.Handshake(ctx, false)
	c.Assert(err, NotNil)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead(c *C) {
	ctx, cancel := context.WithCancel(context.Background())

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(ctx, false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	info, err := conn.GetRemoteRefs(ctx)
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	cancel()

	err = conn.Fetch(ctx, req)
	// TODO: assert error type
	c.Assert(err, NotNil)
}

func (s *UploadPackSuite) TestUploadPackFull(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, IsNil)
}

func (s *UploadPackSuite) TestUploadPackInvalidReq(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	req := &transport.FetchRequest{
		Wants:    []plumbing.Hash{plumbing.ZeroHash},
		Progress: io.Discard,
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, NotNil, Commentf("invalid request should return an error: %s", err))
}

func (s *UploadPackSuite) TestUploadPackNoChanges(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, NotNil)
}

func (s *UploadPackSuite) TestUploadPackMulti(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{
			plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, IsNil)
}

func (s *UploadPackSuite) TestUploadPackPartial(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer func() { c.Assert(conn.Close(), IsNil) }()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{
			plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		},
		Haves: []plumbing.Hash{
			plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, IsNil)
}

func (s *UploadPackSuite) TestFetchError(c *C) {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	c.Assert(err, IsNil)
	conn, err := r.Handshake(context.Background(), false)
	c.Assert(err, IsNil)
	defer conn.Close()

	req := &transport.FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
	}

	err = conn.Fetch(context.Background(), req)
	c.Assert(err, NotNil)

	// XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}
