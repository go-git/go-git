package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

// ReceivePackSuite is a test suite for receive-pack over the new transport API.
type ReceivePackSuite struct {
	suite.Suite
	Endpoint            *url.URL
	EmptyEndpoint       *url.URL
	NonExistentEndpoint *url.URL
	Storer              storage.Storer
	EmptyStorer         storage.Storer
	NonExistentStorer   storage.Storer
	Transport           transport.Transport
}

func (s *ReceivePackSuite) packClient() transport.Transport {
	return s.Transport
}

// TestAdvertisedReferencesEmpty tests advertised references on an empty repo.
func (s *ReceivePackSuite) TestAdvertisedReferencesEmpty() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.EmptyEndpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	refs, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Len(refs, 0)
}

// TestAdvertisedReferencesNotExists tests advertised references on a non-existent repo.
func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists() {
	pc := s.packClient()
	_, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.NonExistentEndpoint, Command: transport.ReceivePackService})
	s.Require().Error(err)
}

// TestCallAdvertisedReferenceTwice tests that calling advertised references twice returns the same result.
func (s *ReceivePackSuite) TestCallAdvertisedReferenceTwice() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	refs1, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(refs1)

	refs2, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Equal(refs1, refs2)
}

// TestDefaultBranch tests that the default branch is correctly advertised.
func (s *ReceivePackSuite) TestDefaultBranch() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	refs, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	ok := false
	var ref *plumbing.Reference
	for _, r := range refs {
		if r.Name() == plumbing.Master {
			ref = r
			ok = true
			break
		}
	}
	s.Require().True(ok)
	s.Require().Equal(fixtures.Basic().One().Head, ref.Hash().String())
}

// TestCapabilities tests that capabilities are correctly reported.
func (s *ReceivePackSuite) TestCapabilities() {
	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.Endpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()
	s.Require().Len(conn.Capabilities().Get("agent"), 1)
}

// TestFullSendPackOnEmpty tests a full send-pack on an empty repo.
func (s *ReceivePackSuite) TestFullSendPackOnEmpty() {
	endpoint := s.EmptyEndpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestSendPackWithContext tests send-pack with a cancelled context.
func (s *ReceivePackSuite) TestSendPackWithContext() {
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Packfile: s.mustPackfile(fixture),
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}

	pc := s.packClient()
	conn, err := pc.Handshake(context.TODO(), &transport.Request{URL: s.EmptyEndpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	defer func() { s.Require().NoError(conn.Close()) }()

	ctx, cancel := context.WithCancel(context.TODO())
	cancel()

	err = conn.Push(ctx, s.EmptyStorer, req)
	s.Require().NotNil(err)
}

// TestSendPackOnEmpty tests send-pack on an empty repo.
func (s *ReceivePackSuite) TestSendPackOnEmpty() {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestSendPackOnEmptyWithReportStatus tests send-pack on an empty repo with report-status.
func (s *ReceivePackSuite) TestSendPackOnEmptyWithReportStatus() {
	endpoint := s.EmptyEndpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestFullSendPackOnNonEmpty tests a full send-pack on a non-empty repo.
func (s *ReceivePackSuite) TestFullSendPackOnNonEmpty() {
	endpoint := s.Endpoint
	full := true
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestSendPackOnNonEmpty tests send-pack on a non-empty repo.
func (s *ReceivePackSuite) TestSendPackOnNonEmpty() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestSendPackOnNonEmptyWithReportStatus tests send-pack on a non-empty repo with report-status.
func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatus() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.NewHash(fixture.Head), New: plumbing.NewHash(fixture.Head)},
	}
	s.receivePack(endpoint, req, fixture, full)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

// TestSendPackOnNonEmptyWithReportStatusWithError tests send-pack error handling.
func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatusWithError() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	err := s.receivePackNoCheck(endpoint, req, fixture, full)
	s.Regexp(regexp.MustCompile(".*(failed to update ref|failed to lock|reference already exists).*"), err)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) receivePackNoCheck(ep *url.URL,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) error {
	s.T().Helper()
	ctx := context.TODO()
	fixtureURL := ""
	if fixture != nil {
		fixtureURL = fixture.URL
	}
	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
		ep.String(), fixtureURL, callAdvertisedReferences,
	)

	rootPath := ep.Path
	stat, err := os.Stat(ep.Path)
	if rootPath != "" && err == nil && stat.IsDir() {
		objectPath := filepath.Join(rootPath, "objects/pack")
		files, err := os.ReadDir(objectPath)
		s.Require().NoError(err)
		for _, file := range files {
			path := filepath.Join(objectPath, file.Name())
			err = os.Chmod(path, 0o644)
			s.Require().NoError(err)
		}
	}

	pc := s.packClient()
	conn, err := pc.Handshake(ctx, &transport.Request{URL: ep, Command: transport.ReceivePackService})
	s.Require().NoError(err, comment)
	defer func() { s.Require().NoError(conn.Close()) }()

	if callAdvertisedReferences {
		info, err := conn.GetRemoteRefs(ctx)
		s.Require().NoError(err, comment)
		s.Require().NotNil(info, comment)
	}

	var needPackfile bool
	for _, cmd := range req.Commands {
		if cmd.Action() != packp.Delete {
			needPackfile = true
			break
		}
	}

	if needPackfile {
		if fixture != nil {
			req.Packfile = s.mustPackfile(fixture)
		} else {
			req.Packfile = s.emptyPackfile()
		}
	}

	return conn.Push(ctx, s.EmptyStorer, req)
}

func (s *ReceivePackSuite) receivePack(ep *url.URL,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) {
	s.T().Helper()
	fixtureURL := ""
	if fixture != nil {
		fixtureURL = fixture.URL
	}
	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
		ep.String(), fixtureURL, callAdvertisedReferences,
	)
	err := s.receivePackNoCheck(ep, req, fixture, callAdvertisedReferences)
	s.Require().NoError(err, comment)
}

func (s *ReceivePackSuite) checkRemoteHead(ep *url.URL, head plumbing.Hash) {
	s.T().Helper()
	s.checkRemoteReference(ep, plumbing.Master, head)
}

func (s *ReceivePackSuite) checkRemoteReference(ep *url.URL,
	refName plumbing.ReferenceName, head plumbing.Hash,
) {
	s.T().Helper()
	ctx := context.TODO()
	pc := s.packClient()
	conn, err := pc.Handshake(ctx, &transport.Request{URL: ep, Command: transport.ReceivePackService})
	s.Require().NoError(err)
	ar, err := conn.GetRemoteRefs(ctx)
	s.Require().NoError(err, fmt.Sprintf("endpoint: %s", ep.String()))
	ok := false
	var ref *plumbing.Reference
	for _, r := range ar {
		if r.Name() == refName {
			ref = r
			ok = true
			break
		}
	}
	if head == plumbing.ZeroHash {
		s.Require().False(ok)
	} else {
		s.Require().True(ok)
		s.Require().Equal(head, ref.Hash())
	}
	s.Require().NoError(conn.Close())
}

// TestSendPackAddDeleteReference tests adding and deleting a reference via send-pack.
func (s *ReceivePackSuite) TestSendPackAddDeleteReference() {
	s.testSendPackAddReference()
	s.testSendPackDeleteReference()
}

func (s *ReceivePackSuite) testSendPackAddReference() {
	s.T().Helper()
	ctx := context.TODO()
	pc := s.packClient()
	conn, err := pc.Handshake(ctx, &transport.Request{URL: s.Endpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)

	refs, err := conn.GetRemoteRefs(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(refs)
	s.Require().NoError(conn.Close())

	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/newbranch", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}
	s.receivePack(s.Endpoint, req, fixture, false)
	s.checkRemoteReference(s.Endpoint, "refs/heads/newbranch", plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) testSendPackDeleteReference() {
	s.T().Helper()
	ctx := context.TODO()
	pc := s.packClient()
	conn, err := pc.Handshake(ctx, &transport.Request{URL: s.Endpoint, Command: transport.ReceivePackService})
	s.Require().NoError(err)

	caps := conn.Capabilities()
	refs, err := conn.GetRemoteRefs(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(refs)
	s.Require().NoError(conn.Close())

	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/newbranch", Old: plumbing.NewHash(fixture.Head), New: plumbing.ZeroHash},
		},
	}

	if !caps.Supports(capability.DeleteRefs) {
		s.Fail("capability delete-refs not supported")
	}

	s.receivePack(s.Endpoint, req, fixture, false)
	s.checkRemoteReference(s.Endpoint, "refs/heads/newbranch", plumbing.ZeroHash)
}

func (s *ReceivePackSuite) mustPackfile(fixture *fixtures.Fixture) io.ReadCloser {
	s.T().Helper()
	f, err := fixture.Packfile()
	s.Require().NoError(err)
	return f
}

func (s *ReceivePackSuite) emptyPackfile() io.ReadCloser {
	s.T().Helper()
	var buf bytes.Buffer
	e := packfile.NewEncoder(&buf, memory.NewStorage(), false)
	_, err := e.Encode(nil, 10)
	if err != nil {
		panic(err)
	}
	return io.NopCloser(&buf)
}
