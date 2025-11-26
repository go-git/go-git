// Package test implements common test suite for different transport
// implementations.
package test

import (
	"context"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
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
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	ar, err := conn.GetRemoteRefs(context.TODO())
	s.Require().ErrorIs(err, transport.ErrEmptyRemoteRepository)
	s.Require().Nil(ar)
}

func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.NonExistentStorer, s.NonExistentEndpoint, s.EmptyAuth)
	s.Require().NoError(err)
	_, err = r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().Error(err)
}

func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	ar1, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(ar1)
	ar2, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Equal(ar1, ar2)
}

func (s *UploadPackSuite) TestDefaultBranch() {
	ctx := context.TODO()
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(ctx, transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(info)
	symrefs := conn.Capabilities().Get(capability.SymRef)
	s.Require().Len(symrefs, 1)
	s.Require().Equal("HEAD:refs/heads/master", symrefs[0])
}

func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().True(conn.Capabilities().Supports(capability.MultiACK))
}

func (s *UploadPackSuite) TestCapabilities() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().Len(conn.Capabilities().Get(capability.Agent), 1)
}

func (s *UploadPackSuite) TestUploadPack() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)

	s.Require().Equal(28, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackWithContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(ctx, req)
	s.Require().NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	ctx, cancel := context.WithCancel(context.Background())

	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	cancel()
	err = conn.Fetch(ctx, req)
	s.Require().NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackFull() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	beforeCount := s.countObjects(s.Storer)
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(28, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackInvalidReq() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	// Invalid capabilities are now handled by the transport layer

	err = conn.Fetch(context.Background(), req)
	s.Require().NoError(err) // Should succeed as invalid capabilities are handled internally
}

func (s *UploadPackSuite) TestUploadPackNoChanges() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), req)
	s.Require().ErrorIs(err, transport.ErrNoChange)
}

func (s *UploadPackSuite) TestUploadPackMulti() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	err = conn.Fetch(context.Background(), req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(31, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestUploadPackPartial() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	beforeCount := s.countObjects(s.Storer)
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	err = conn.Fetch(context.Background(), req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.Storer)
	s.Require().Equal(4, afterCount-beforeCount)
}

func (s *UploadPackSuite) TestFetchError() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(context.TODO(), transport.UploadPackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	err = conn.Fetch(context.Background(), req)
	s.Require().NotNil(err)

	// XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}

func (s *UploadPackSuite) countObjects(st storage.Storer) int {
	iter, err := st.IterEncodedObjects(plumbing.AnyObject)
	s.Require().NoError(err)
	defer iter.Close()
	var count int
	err = iter.ForEach(func(plumbing.EncodedObject) error {
		count++
		return nil
	})
	s.Require().NoError(err)
	return count
}
