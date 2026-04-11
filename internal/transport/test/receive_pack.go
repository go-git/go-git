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

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

// ReceivePackSuite is a test suite for receive-pack transport implementations.
type ReceivePackSuite struct {
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

// TearDownTest closes all storers.
func (s *ReceivePackSuite) TearDownTest() {
	for _, st := range []storage.Storer{s.Storer, s.EmptyStorer, s.NonExistentStorer} {
		if c, ok := st.(io.Closer); ok {
			_ = c.Close()
		}
	}
}

// TestAdvertisedReferencesEmpty tests advertised references on an empty repo.
func (s *ReceivePackSuite) TestAdvertisedReferencesEmpty() {
	r, err := s.Client.NewSession(s.EmptyStorer, s.EmptyEndpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()
	refs, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Len(refs, 0)
}

// TestAdvertisedReferencesNotExists tests advertised references on a non-existent repo.
func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.NonExistentStorer, s.NonExistentEndpoint, s.EmptyAuth)
	s.Require().NoError(err)

	_, err = r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().Error(err)
}

// TestCallAdvertisedReferenceTwice tests that calling advertised references twice returns the same result.
func (s *ReceivePackSuite) TestCallAdvertisedReferenceTwice() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	refs1, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().NotNil(refs1)

	refs2, err := conn.GetRemoteRefs(context.TODO())
	s.Require().NoError(err)
	s.Require().Equal(refs1, refs2)
}

// TestDefaultBranch tests that the default branch is correctly advertised.
func (s *ReceivePackSuite) TestDefaultBranch() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

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
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()
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

	r, err := s.Client.NewSession(s.EmptyStorer, s.EmptyEndpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	ctx, cancel := context.WithCancel(context.TODO())
	cancel()

	err = conn.Push(ctx, req)
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
	// req.Capabilities.Set(capability.ReportStatus)
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
	// req.Capabilities.Set(capability.ReportStatus)

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
	// XXX: Recent git versions return "failed to update ref", while older
	//     (>=1.9) return "failed to lock".
	// More recent versions: command error on <ref>: reference already exists
	s.Regexp(regexp.MustCompile(".*(failed to update ref|failed to lock|reference already exists).*"), err)
	s.checkRemoteHead(endpoint, plumbing.NewHash(fixture.Head))
}

func (s *ReceivePackSuite) receivePackNoCheck(ep *transport.Endpoint,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) error {
	s.T().Helper()
	ctx := context.TODO()
	url := ""
	if fixture != nil {
		url = fixture.URL
	}
	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
		ep.String(), url, callAdvertisedReferences,
	)

	// Set write permissions to endpoint directory files. By default
	// fixtures are generated with read only permissions, this causes
	// errors deleting or modifying files.
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

	r, err := s.Client.NewSession(s.EmptyStorer, ep, s.EmptyAuth)
	s.Require().NoError(err, comment)

	conn, err := r.Handshake(ctx, transport.ReceivePackService)
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

	return conn.Push(ctx, req)
}

func (s *ReceivePackSuite) receivePack(ep *transport.Endpoint,
	req *transport.PushRequest, fixture *fixtures.Fixture,
	callAdvertisedReferences bool,
) {
	s.T().Helper()
	url := ""
	if fixture != nil {
		url = fixture.URL
	}

	comment := fmt.Sprintf(
		"failed with ep=%s fixture=%s callAdvertisedReferences=%v",
		ep.String(), url, callAdvertisedReferences,
	)
	err := s.receivePackNoCheck(ep, req, fixture, callAdvertisedReferences)
	// report, err := s.receivePackNoCheck(ep, req, fixture, callAdvertisedReferences)
	s.Require().NoError(err, comment)
	// if req.Capabilities.Supports(capability.ReportStatus) {
	// 	s.Require().NotNil(report, comment)
	// 	s.Require().NoError(report.Error(), comment)
	// } else {
	// 	s.Require().Nil(report, comment)
	// }
}

func (s *ReceivePackSuite) checkRemoteHead(ep *transport.Endpoint, head plumbing.Hash) {
	s.T().Helper()
	s.checkRemoteReference(ep, plumbing.Master, head)
}

func (s *ReceivePackSuite) checkRemoteReference(ep *transport.Endpoint,
	refName plumbing.ReferenceName, head plumbing.Hash,
) {
	s.T().Helper()
	ctx := context.TODO()
	r, err := s.Client.NewSession(s.EmptyStorer, ep, s.EmptyAuth)
	s.Require().NoError(err)
	conn, err := r.Handshake(ctx, transport.ReceivePackService)
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
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(ctx, transport.ReceivePackService)
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
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(ctx, transport.ReceivePackService)
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
