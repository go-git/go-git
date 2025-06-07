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

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v5"
)

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

func (s *ReceivePackSuite) TestAdvertisedReferencesNotExists() {
	r, err := s.Client.NewSession(s.NonExistentStorer, s.NonExistentEndpoint, s.EmptyAuth)
	s.Require().NoError(err)

	_, err = r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().Error(err)
}

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

func (s *ReceivePackSuite) TestCapabilities() {
	r, err := s.Client.NewSession(s.Storer, s.Endpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()
	s.Require().Len(conn.Capabilities().Get("agent"), 1)
}

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

func (s *ReceivePackSuite) TestSendPackWithContext() {
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{
		Packfile: fixture.Packfile(),
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
		},
	}

	r, err := s.Client.NewSession(s.EmptyStorer, s.EmptyEndpoint, s.EmptyAuth)
	s.Require().NoError(err)

	conn, err := r.Handshake(context.TODO(), transport.ReceivePackService)
	s.Require().NoError(err)
	defer func() { s.Require().Nil(conn.Close()) }()

	ctx, close := context.WithCancel(context.TODO())
	close()

	err = conn.Push(ctx, req)
	s.Require().NotNil(err)
}

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

func (s *ReceivePackSuite) TestSendPackOnNonEmptyWithReportStatusWithError() {
	endpoint := s.Endpoint
	full := false
	fixture := fixtures.Basic().ByTag("packfile").One()
	req := &transport.PushRequest{}
	req.Commands = []*packp.Command{
		{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash(fixture.Head)},
	}
	// req.Capabilities.Set(capability.ReportStatus)

	// report, err := s.receivePackNoCheck(endpoint, req, fixture, full)
	err := s.receivePackNoCheck(endpoint, req, fixture, full)
	// XXX: Recent git versions return "failed to update ref", while older
	//     (>=1.9) return "failed to lock".
	s.Regexp(regexp.MustCompile(".*(failed to update ref|failed to lock).*"), err)
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
	// fixtures are generated with read only permissions, this casuses
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
			s.Require().NotNil(fixture.Packfile())
			req.Packfile = fixture.Packfile()
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
