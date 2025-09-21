package transport

import (
	"bytes"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UploadPackSuite struct {
	suite.Suite
}

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV0() {
	testAdvertise(s.T(), UploadPack, "", false)
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV2() {
	// TODO: support version 2
	testAdvertise(s.T(), UploadPack, "version=2", false)
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV1() {
	buf := testAdvertise(s.T(), UploadPack, "version=1", false)
	s.Containsf(buf.String(), "version 1", "advertisement should contain version 1")
}

func (s *UploadPackSuite) TestUploadPackAlwaysUseSidebandWhenAvailable() {
	dot := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	upreq := packp.NewUploadRequest()
	upreq.Capabilities.Add(capability.Sideband64k)
	upreq.Capabilities.Add(capability.NoProgress)
	iter, err := st.IterEncodedObjects(plumbing.AnyObject)
	require.NoError(s.T(), err)
	defer iter.Close()
	obj, err := iter.Next()
	require.NoError(s.T(), err)
	upreq.Wants = append(upreq.Wants, obj.Hash())

	var uphav packp.UploadHaves
	uphav.Done = true

	var reqW bytes.Buffer
	require.NoError(s.T(), upreq.Encode(&reqW))
	require.NoError(s.T(), uphav.Encode(&reqW))
	buf := testServe(s.T(), st, UploadPack, &reqW, &UploadPackOptions{
		GitProtocol:   "version=1",
		AdvertiseRefs: false,
		StatelessRPC:  true,
	})

	expected := "0008NAK\n0009\x01PACK" // NAK response + sideband pack header + PACK marker
	s.Equal(expected, buf.String()[:len(expected)], "pack file should be sent via sideband")
}
