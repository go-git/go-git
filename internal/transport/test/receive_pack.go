// Package test implements common test suite for different transport
// implementations.
package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type ReceivePackSuite struct {
	suite.Suite
	Endpoint            *transport.Endpoint
	EmptyEndpoint       *transport.Endpoint
	NonExistentEndpoint *transport.Endpoint
	EmptyAuth           transport.AuthMethod
	Client              transport.Transport
}

func (s *ReceivePackSuite) TestAdvertisedReferencesEmpty() {
	r, err := s.Client.NewReceivePackSession(s.EmptyEndpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	ar, err := r.AdvertisedReferences()
	s.NoError(err)
	s.Nil(ar.Head)
}

func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewReceivePackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	ar, err := r.AdvertisedReferences()
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(ar)
	s.Nil(r.Close())

	r, err = s.Client.NewReceivePackSession(s.NonExistentEndpoint, s.EmptyAuth)
	s.NoError(err)
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "master", Old: plumbing.ZeroHash, New: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
	}

	writer, err := r.ReceivePack(context.Background(), req)
	s.ErrorIs(err, transport.ErrRepositoryNotFound)
	s.Nil(writer)
	s.Nil(r.Close())
}

func (s *ReceivePackSuite) TestCallAdvertisedReferenceTwice() {
	r, err := s.Client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	defer func() { s.Nil(r.Close()) }()
	s.NoError(err)
	ar1, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(ar1)
	ar2, err := r.AdvertisedReferences()
	s.NoError(err)
	s.Equal(ar1, ar2)
}

func (s *ReceivePackSuite) TestDefaultBranch() {
	r, err := s.Client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	ref, ok := info.References["refs/heads/master"]
	s.True(ok)
	s.Equal(fixtures.Basic().One().Head, ref.String())
}

func (s *ReceivePackSuite) TestCapabilities() {
	r, err := s.Client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.Len(info.Capabilities.Get("agent"), 1)
}

func (s *ReceivePackSuite) TestFullSendPackOnEmpty() {
	endpoint := s.EmptyEndpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackWithContext() {
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Packfile = fixture.Packfile()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}

	r, err := s.Client.NewReceivePackSession(s.EmptyEndpoint, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()

	info, err := r.AdvertisedReferences()
	s.NoError(err)
	s.NotNil(info)

	ctx, close := context.WithCancel(context.TODO())
	close()

	report, err := r.ReceivePack(ctx, req)
	s.NotNil(err)
	s.Nil(report)
}

func (s *ReceivePackSuite) TestSendPackOnEmpty() {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnEmptyWithReportStatus() {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	req.Capabilities.Set(capability.ReportStatus)
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestFullSendPackOnNonEmpty() {
	endpoint := s.Endpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmpty() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatus() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	req.Capabilities.Set(capability.ReportStatus)

	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatusWithError() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	req.Capabilities.Set(capability.ReportStatus)

	report, err := s.receivePackNoCheck(endpoint, req, fixture, full)
	// XXX: Recent git versions return "failed to update ref", while older
	//     (>=1.9) return "failed to lock".
	s.Regexp(regexp.MustCompile(".*(failed to update ref|failed to lock).*"), err)
	s.Equal("ok", report.UnpackStatus)
	s.Len(report.CommandStatuses, 1)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), report.CommandStatuses[0].ReferenceName)
	s.Regexp(regexp.MustCompile("(failed to update ref|failed to lock)"), report.CommandStatuses[0].Status)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) receivePackNoCheck(ep *transport.Endpoint,
	req *packp.ReferenceUpdateRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) (*packp.ReportStatus, error) {
	url := ""
	if fixture != nil {
		url = fixture.URL
	}
	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
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
		s.NoError(err)

		for _, file := range files {
			path := filepath.Join(objectPath, file.Name())
			err = os.Chmod(path, 0o644)
			s.NoError(err)
		}
	}

	r, err := s.Client.NewReceivePackSession(ep, s.EmptyAuth)
	s.NoError(err, comment)
	defer func() { s.NoError(r.Close(), comment) }()

	if callAdvertisedReferences {
		info, err := r.AdvertisedReferences()
		s.NoError(err, comment)
		s.NotNil(info, comment)
	}

	if fixture != nil {
		s.NotNil(fixture.Packfile())
		req.Packfile = fixture.Packfile()
	} else {
		req.Packfile = s.emptyPackfile()
	}

	return r.ReceivePack(context.Background(), req)
}

func (s *ReceivePackSuite) receivePack(ep *transport.Endpoint,
	req *packp.ReferenceUpdateRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) {
	url := ""
	if fixture != nil {
		url = fixture.URL
	}

	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
		ep.String(), url, callAdvertisedReferences,
	)
	report, err := s.receivePackNoCheck(ep, req, fixture, callAdvertisedReferences)
	s.NoError(err, comment)
	if req.Capabilities.Supports(capability.ReportStatus) {
		s.NotNil(report, comment)
		s.NoError(report.Error(), comment)
	} else {
		s.Nil(report, comment)
	}
}

func (s *ReceivePackSuite) checkRemoteHead(ep *transport.Endpoint, head plumbing.Hash) {
	s.checkRemoteReference(ep, "refs/heads/master", head)
}

func (s *ReceivePackSuite) checkRemoteReference(ep *transport.Endpoint,
	refName string, head plumbing.Hash,
) {
	r, err := s.Client.NewUploadPackSession(ep, s.EmptyAuth)
	s.NoError(err)
	defer func() { s.Nil(r.Close()) }()
	ar, err := r.AdvertisedReferences()
	s.NoError(err, fmt.Sprintf("endpoint: %s", ep.String()))
	ref, ok := ar.References[refName]
	if head == plumbing.ZeroHash {
		s.False(ok)
	} else {
		s.True(ok)
		s.Equal(head, ref)
	}
}

func (s *ReceivePackSuite) TestSendPackAddDeleteReference() {
	s.testSendPackAddReference()
	s.testSendPackDeleteReference()
}

func (s *ReceivePackSuite) testSendPackAddReference() {
	r, err := s.Client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	fixture := fixtures.Basic().ByTag("packfile").One()

	ar, err := r.AdvertisedReferences()
	s.NoError(err)

	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/newbranch", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	if ar.Capabilities.Supports(capability.ReportStatus) {
		req.Capabilities.Set(capability.ReportStatus)
	}

	s.Nil(r.Close())

	s.receivePack(s.Endpoint, req, nil, false)
	s.checkRemoteReference(s.Endpoint, "refs/heads/newbranch", plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) testSendPackDeleteReference() {
	r, err := s.Client.NewReceivePackSession(s.Endpoint, s.EmptyAuth)
	s.NoError(err)

	fixture := fixtures.Basic().ByTag("packfile").One()

	ar, err := r.AdvertisedReferences()
	s.NoError(err)

	req := packp.NewReferenceUpdateRequest()
	req.Commands = []*packp.Command{
		{Name: "refs/heads/newbranch", Old: plumbing.NewHash(fixture.Head), New: plumbing.ZeroHash},
	}
	if ar.Capabilities.Supports(capability.ReportStatus) {
		req.Capabilities.Set(capability.ReportStatus)
	}

	if !ar.Capabilities.Supports(capability.DeleteRefs) {
		s.Fail("capability delete-refs not supported")
	}

	s.Nil(r.Close())

	s.receivePack(s.Endpoint, req, nil, false)
	s.checkRemoteReference(s.Endpoint, "refs/heads/newbranch", plumbing.ZeroHash)
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
