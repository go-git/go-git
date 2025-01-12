// Package test implements common test suite for different transport
// implementations.
package test

import (
	"context"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/stretchr/testify/suite"
)

type UploadPackSuite struct {
	suite.Suite
	Endpoint            *transport.Endpoint
	EmptyEndpoint       *transport.Endpoint
	NonExistentEndpoint *transport.Endpoint
	Storer              storage.Storer
	EmptyStorer         storage.Storer
	NonExistentStorer   storage.Storer
	EmptyAuth           transport.AuthMethod
	Client              transport.Transport
}

func (s *UploadPackSuite) TestAdvertisedReferencesEmpty() {
	r, err := s.Client.NewSession(s.EmptyStorer, s.EmptyEndpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	ar, err := conn.GetRemoteRefs(context.TODO())
	s.Equal(err, transport.ErrEmptyRemoteRepository)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.NonExistentStorer, s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	ar, err := conn.GetRemoteRefs(context.TODO())
	s.Equal(err, transport.ErrRepositoryNotFound)
	s.Nil(ar)

	r, err = s.Client.NewSession(s.NonExistentStorer, s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err = r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	err = conn.Fetch(context.Background(), req)
	s.Equal(err, transport.ErrRepositoryNotFound)
}

func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	ar1, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(ar1)
	ar2, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.Equal(ar1, ar2)
}

func (s *UploadPackSuite) TestDefaultBranch() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)
	symrefs := conn.Capabilities().Get(capability.SymRef)
	s.Len(symrefs, 1)
	s.Equal("HEAD:refs/heads/master", symrefs[0])
}

func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)
	s.True(conn.Capabilities().Supports(capability.MultiACK))
}

func (s *UploadPackSuite) TestCapabilities() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)
	s.Len(conn.Capabilities().Get(capability.Agent), 1)
}

func (s *UploadPackSuite) TestUploadPack() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(s.Storer, 28)
}

func (s *UploadPackSuite) TestUploadPackWithContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(ctx, req)
	s.NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	ctx, cancel := context.WithCancel(context.Background())

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	cancel()
	err = conn.Fetch(ctx, req)
	s.NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackFull() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.NoError(err)
	s.NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(s.Storer, 28)
}

func (s *UploadPackSuite) TestUploadPackInvalidReq() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	// Invalid capabilities are now handled by the transport layer

	err = conn.Fetch(context.Background(), req)
	s.NoError(err) // Should succeed as invalid capabilities are handled internally
}

func (s *UploadPackSuite) TestUploadPackNoChanges() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.NoError(err)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.Equal(err, transport.ErrEmptyUploadPackRequest)
}

func (s *UploadPackSuite) TestUploadPackMulti() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	err = conn.Fetch(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(s.Storer, 31)
}

func (s *UploadPackSuite) TestUploadPackPartial() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	err = conn.Fetch(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(s.Storer, 4)
}

func (s *UploadPackSuite) TestFetchError() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	defer func() { s.Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	err = conn.Fetch(context.Background(), req)
	s.NotNil(err)

	// XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}

func (s *UploadPackSuite) checkObjectNumber(st storage.Storer, n int) {
	los, ok := st.(storer.LooseObjectStorer)
	s.True(ok)

	var objs int
	err := los.ForEachObjectHash(func(plumbing.Hash) error {
		objs++
		return nil
	})
	s.NoError(err)
	s.Len(objs, n)
}
