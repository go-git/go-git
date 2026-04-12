package transport

import (
	"bytes"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type UploadPackServeSuite struct {
	suite.Suite
}

func TestUploadPackServeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(UploadPackServeSuite))
}

func (s *UploadPackServeSuite) TestUploadPackAdvertiseV0() {
	testAdvertise(s.T(), UploadPack, "", false)
}

func (s *UploadPackServeSuite) TestUploadPackAdvertiseV2() {
	testAdvertise(s.T(), UploadPack, "version=2", false)
}

func (s *UploadPackServeSuite) TestUploadPackAdvertiseV1() {
	buf := testAdvertise(s.T(), UploadPack, "version=1", false)
	s.Contains(buf.String(), "version 1")
}

func (s *UploadPackServeSuite) TestUploadPackAlwaysUseSidebandWhenAvailable() {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	upreq := &packp.UploadRequest{}
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
	buf := testServe(s.T(), st, UploadPack, io.NopCloser(&reqW), &UploadPackRequest{
		GitProtocol:   "version=1",
		AdvertiseRefs: false,
		StatelessRPC:  true,
	})

	expected := "0008NAK\n0009\x01PACK"
	s.Equal(expected, buf.String()[:len(expected)])
}

func (s *UploadPackServeSuite) TestUploadPackSkipDeltaCompression() {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	head, err := storer.ResolveReference(st, plumbing.HEAD)
	require.NoError(s.T(), err)
	wantHash := head.Hash()

	servePack := func(skipDelta bool) []byte {
		upreq := &packp.UploadRequest{}
		upreq.Capabilities.Add(capability.NoProgress)
		upreq.Wants = append(upreq.Wants, wantHash)

		var uphav packp.UploadHaves
		uphav.Done = true

		var reqW bytes.Buffer
		require.NoError(s.T(), upreq.Encode(&reqW))
		require.NoError(s.T(), uphav.Encode(&reqW))
		buf := testServe(s.T(), st, UploadPack, io.NopCloser(&reqW), &UploadPackRequest{
			GitProtocol:          "version=1",
			AdvertiseRefs:        false,
			StatelessRPC:         true,
			SkipDeltaCompression: skipDelta,
		})

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

	normalPack := servePack(false)
	s.Greater(countDeltas(normalPack), 0)

	skipPack := servePack(true)
	s.Equal(0, countDeltas(skipPack))
}

type ReceivePackServeSuite struct {
	suite.Suite
}

func TestReceivePackServeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReceivePackServeSuite))
}

func (s *ReceivePackServeSuite) TestReceivePackAdvertiseV0() {
	testAdvertise(s.T(), ReceivePack, "", false)
}

func (s *ReceivePackServeSuite) TestReceivePackAdvertiseV2() {
	testAdvertise(s.T(), UploadPack, "version=2", false)
}

func (s *ReceivePackServeSuite) TestReceivePackAdvertiseV1() {
	buf := testAdvertise(s.T(), ReceivePack, "version=1", false)
	s.Contains(buf.String(), "version 1")
}
