// Package test implements common test suite for different transport
// implementations.
package test

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"
)

type UploadPackSuite struct {
	suite.Suite
	Endpoint            *transport.Endpoint
	EmptyEndpoint       *transport.Endpoint
	NonExistentEndpoint *transport.Endpoint
	EmptyAuth           transport.AuthMethod
	Client              transport.Transport
}

func (s *UploadPackSuite) TestAdvertisedReferencesEmpty() {
	r, err := s.Client.NewUploadPackSession(s.EmptyEndpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	ar, err := r.AdvertisedReferences()
	s.Equal(err, transport.ErrEmptyRemoteRepository)
	s.Nil(ar)
}

func (s *UploadPackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	ar, err := r.AdvertisedReferences()
	s.Equal(err, transport.ErrRepositoryNotFound)
	s.Nil(ar)

	r, err = s.Client.NewUploadPackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	reader, err := r.UploadPack(context.Background(), req)
	s.Equal(err, transport.ErrRepositoryNotFound)
	s.Nil(reader)
}

func (s *UploadPackSuite) TestCallAdvertisedReferenceTwice() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	ar1, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(ar1)
	ar2, err := r.AdvertisedReferences()
	s.NoError(err)
	s.Equal(ar1, ar2)
}

func (s *UploadPackSuite) TestDefaultBranch() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	symrefs := info.Capabilities.Get(capability.SymRef)
	s.Len(symrefs, 1)
	s.Equal("HEAD:refs/heads/master", symrefs[0])
}

func (s *UploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.True(info.Capabilities.Supports(capability.MultiACK))
}

func (s *UploadPackSuite) TestCapabilities() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.Len(info.Capabilities.Get(capability.Agent), 1)
}

func (s *UploadPackSuite) TestUploadPack() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.UploadPack(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(reader, 28)
}

func (s *UploadPackSuite) TestUploadPackWithContext() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(info)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.UploadPack(ctx, req)
	s.NotNil(err)
	s.Nil(reader)
}

func (s *UploadPackSuite) TestUploadPackWithContextOnRead() {
	ctx, cancel := context.WithCancel(context.Background())

	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(info)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.UploadPack(ctx, req)
	s.NoError(err)
	s.NotNil(reader)

	cancel()

	_, err = io.Copy(io.Discard, reader)
	s.NotNil(err)

	err = reader.Close()
	s.NoError(err)
	err = r.Close()
	s.NoError(err)
}

func (s *UploadPackSuite) TestUploadPackFull() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(info)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.UploadPack(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(reader, 28)
}

func (s *UploadPackSuite) TestUploadPackInvalidReq() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Capabilities.Set(capability.Sideband)
	req.Capabilities.Set(capability.Sideband64k)

	_, err = r.UploadPack(context.Background(), req)
	s.NotNil(err)
}

func (s *UploadPackSuite) TestUploadPackNoChanges() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	reader, err := r.UploadPack(context.Background(), req)
	s.Equal(err, transport.ErrEmptyUploadPackRequest)
	s.Nil(reader)
}

func (s *UploadPackSuite) TestUploadPackMulti() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Wants = append(req.Wants, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"))

	reader, err := r.UploadPack(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(reader, 31)
}

func (s *UploadPackSuite) TestUploadPackPartial() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	req.Haves = append(req.Haves, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	reader, err := r.UploadPack(context.Background(), req)
	s.NoError(err)

	s.checkObjectNumber(reader, 4)
}

func (s *UploadPackSuite) TestFetchError() {
	r, err := s.Client.NewUploadPackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	req := packp.NewUploadPackRequest()
	req.Wants = append(req.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	reader, err := r.UploadPack(context.Background(), req)
	s.NotNil(err)
	s.Nil(reader)

	//XXX: We do not test Close error, since implementations might return
	//     different errors if a previous error was found.
}

func (s *UploadPackSuite) checkObjectNumber(r io.Reader, n int) {
	b, err := io.ReadAll(r)
	s.NoError(err)
	buf := bytes.NewBuffer(b)
	storage := memory.NewStorage()
	err = packfile.UpdateObjectStorage(storage, buf)
	s.NoError(err)
	s.Len(storage.Objects, n)
}
