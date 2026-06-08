// Package test implements common test suites for the new transport API.
package test

import (
	"context"
	"io"
	"net/url"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

// UploadPackSuite is a test suite for upload-pack over the new transport API.
type UploadPackSuite struct {
	suite.Suite
	Endpoint            *url.URL
	EmptyEndpoint       *url.URL
	NonExistentEndpoint *url.URL
	Storer              storage.Storer
	EmptyStorer         storage.Storer
	NonExistentStorer   storage.Storer
	Transport           transport.Transport
}

// TearDownTest closes all storers.
func (s *UploadPackSuite) TearDownTest() {
	for _, st := range []storage.Storer{s.Storer, s.EmptyStorer, s.NonExistentStorer} {
		if c, ok := st.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

func (s *UploadPackSuite) packClient() transport.Transport {
	return s.Transport
}

// TestAdvertisedReferencesEmpty tests advertised references on an empty repo.
func (s *UploadPackSuite) TestAdvertisedReferencesEmpty() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.EmptyEndpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	ar, err := conn.GetRemoteRefs(context.TODO())
	s.Require().ErrorIs(err, transport.ErrEmptyRemoteRepository)
	s.Require().Nil(ar)
}

// TestAdvertisedReferencesNotExists tests advertised references on a non-existent repo.
func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	pc := s.packClient()
	_, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.NonExistentEndpoint, Command: transport.UploadPackService})
	s.Require().Error(err)
}

// TestCallAdvertisedReferenceTwice tests that calling advertised references twice returns the same result.
func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	ar1, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(ar1)
	ar2, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Equal(ar1, ar2)
}

// TestDefaultBranch tests that the default branch is correctly advertised.
func (s *UploadPackSuite) TestDefaultBranch() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	symrefs := conn.Capabilities().Get(capability.SymRef)
	s.Require().Len(symrefs, 1)
	s.Require().Equal("HEAD:refs/heads/master", symrefs[0])
}

// TestAdvertisedReferencesFilterUnsupported tests filtering unsupported capabilities.
func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().True(conn.Capabilities().Supports(capability.MultiACK))
}

// TestCapabilities tests that capabilities are correctly reported.
func (s *UploadPackSuite) TestCapabilities() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)
	s.Require().Len(conn.Capabilities().Get(capability.Agent), 1)
}

// TestUploadPack tests a basic upload-pack fetch.
func (s *UploadPackSuite) TestUploadPack() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	clientStorer := memory.NewStorage(memory.WithObjectFormat(config.SHA1))
	err = conn.Fetch(context.Background(), clientStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(clientStorer)
	s.Require().Equal(28, afterCount)
}

// TestUploadPackWithContext tests upload-pack with a cancelled context.
func (s *UploadPackSuite) TestUploadPackWithContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	clientStorer := memory.NewStorage(memory.WithObjectFormat(config.SHA1))
	err = conn.Fetch(ctx, clientStorer, req)
	s.Require().Error(err)
}

// TestUploadPackWithContextOnRead tests upload-pack with context cancelled during read.
func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	ctx, cancel := context.WithCancel(context.Background())

	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	cancel()
	err = conn.Fetch(ctx, s.Storer, req)
	s.Require().Error(err)
}

// TestUploadPackFull tests a full upload-pack fetch with advertised references.
func (s *UploadPackSuite) TestUploadPackFull() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	info, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(info)

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	clientStorer := memory.NewStorage(memory.WithObjectFormat(config.SHA1))
	err = conn.Fetch(context.Background(), clientStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(clientStorer)
	s.Require().Equal(28, afterCount)
}

// TestUploadPackInvalidReq tests upload-pack with an invalid request.
func (s *UploadPackSuite) TestUploadPackInvalidReq() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), s.Storer, req)
	s.Require().NoError(err)
}

// TestUploadPackNoChanges tests upload-pack when there are no changes.
func (s *UploadPackSuite) TestUploadPackNoChanges() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	err = conn.Fetch(context.Background(), s.Storer, req)
	s.Require().ErrorIs(err, transport.ErrNoChange)
}

// TestUploadPackMulti tests upload-pack with multiple wants.
func (s *UploadPackSuite) TestUploadPackMulti() {
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))
	s.testUploadPackFetch(req, 31)
}

// TestUploadPackPartial tests upload-pack with haves for a partial fetch.
func (s *UploadPackSuite) TestUploadPackPartial() {
	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	s.testUploadPackFetch(req, 4)
}

func (s *UploadPackSuite) testUploadPackFetch(req *transport.FetchRequest, expectedObjects int) {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	beforeCount := s.countObjects(s.EmptyStorer)
	s.Zero(beforeCount)

	err = conn.Fetch(context.Background(), s.EmptyStorer, req)
	s.Require().NoError(err)

	afterCount := s.countObjects(s.EmptyStorer)
	s.Require().Equal(expectedObjects, afterCount)
}

// TestFetchError tests that fetching a non-existent object returns an error.
func (s *UploadPackSuite) TestFetchError() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.UploadPackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	req := &transport.FetchRequest{}
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	err = conn.Fetch(context.Background(), s.Storer, req)
	s.Require().Error(err)
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
