package transport

import (
	"bytes"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type UploadPackSuite struct {
	suite.Suite
}

func TestUploadPackSuite(t *testing.T) {
	t.Parallel()
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
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
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
	buf := testServe(s.T(), st, UploadPack, io.NopCloser(&reqW), &UploadPackOptions{
		GitProtocol:   "version=1",
		AdvertiseRefs: false,
		StatelessRPC:  true,
	})

	expected := "0008NAK\n0009\x01PACK" // NAK response + sideband pack header + PACK marker
	s.Equal(expected, buf.String()[:len(expected)], "pack file should be sent via sideband")
}

func (s *UploadPackSuite) TestUploadPackSkipDeltaCompression() {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	// Resolve HEAD to get a known commit hash as the want.
	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(s.T(), err)
	wantHash := head.Hash()

	// Helper to perform an upload-pack request and return the raw pack data.
	servePack := func(skipDelta bool) []byte {
		upreq := packp.NewUploadRequest()
		upreq.Capabilities.Add(capability.NoProgress)
		upreq.Wants = append(upreq.Wants, wantHash)

		var uphav packp.UploadHaves
		uphav.Done = true

		var reqW bytes.Buffer
		require.NoError(s.T(), upreq.Encode(&reqW))
		require.NoError(s.T(), uphav.Encode(&reqW))
		buf := testServe(s.T(), st, UploadPack, io.NopCloser(&reqW), &UploadPackOptions{
			GitProtocol:          "version=1",
			AdvertiseRefs:        false,
			StatelessRPC:         true,
			SkipDeltaCompression: skipDelta,
		})

		// Response without sideband: NAK pktline followed by raw pack data.
		const nakPktline = "0008NAK\n"
		require.Equal(s.T(), nakPktline, buf.String()[:len(nakPktline)])
		return buf.Bytes()[len(nakPktline):]
	}

	countDeltas := func(packData []byte) int {
		count := 0
		sc := packfile.NewScanner(bytes.NewReader(packData))
		for sc.Scan() {
			d := sc.Data()
			if d.Section == packfile.ObjectSection {
				oh := d.Value().(packfile.ObjectHeader)
				if oh.Type.IsDelta() {
					count++
				}
			}
		}
		require.NoError(s.T(), sc.Error())
		return count
	}

	// Control: without SkipDeltaCompression, the pack should contain deltas.
	normalPack := servePack(false)
	s.Greater(countDeltas(normalPack), 0, "expected delta objects without SkipDeltaCompression")

	// With SkipDeltaCompression, no delta objects should be produced.
	skipPack := servePack(true)
	s.Equal(0, countDeltas(skipPack), "expected no delta objects with SkipDeltaCompression")
}
