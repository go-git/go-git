package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/ioutil"
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
	defer func() { _ = st.Close() }()
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
	defer func() { _ = st.Close() }()

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

func (s *UploadPackServeSuite) TestUploadPackStatefulMultiRoundSendsFinalACK() {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	s.T().Cleanup(func() { _ = st.Close() })

	head, err := storer.ResolveReference(st, plumbing.HEAD)
	s.Require().NoError(err)
	headCommit, err := object.GetCommit(st, head.Hash())
	s.Require().NoError(err)
	s.Require().NotEmpty(headCommit.ParentHashes)
	common := headCommit.ParentHashes[0]

	var upreq packp.UploadRequest
	upreq.Capabilities.Add(capability.MultiACK)
	upreq.Capabilities.Add(capability.NoProgress)
	upreq.Wants = append(upreq.Wants, head.Hash())

	var firstRound packp.UploadHaves
	firstRound.Haves = []plumbing.Hash{common}

	var finalRound packp.UploadHaves
	finalRound.Done = true

	var reqW bytes.Buffer
	s.Require().NoError(upreq.Encode(&reqW))
	s.Require().NoError(firstRound.Encode(&reqW))
	s.Require().NoError(finalRound.Encode(&reqW))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	errc := make(chan error, 1)
	go func() {
		errc <- UploadPack(ctx, st, io.NopCloser(&reqW), ioutil.WriteNopCloser(&out), &UploadPackRequest{
			GitProtocol:   "version=1",
			AdvertiseRefs: false,
			StatelessRPC:  false,
		})
	}()

	select {
	case err := <-errc:
		s.Require().NoError(err)
	case <-time.After(5 * time.Second):
		s.FailNow("upload-pack did not complete stateful multi-round negotiation")
	}

	response := out.String()
	continueACK := fmt.Sprintf("ACK %s continue\n", common)
	finalACK := fmt.Sprintf("ACK %s\n", common)

	continueAt := strings.Index(response, continueACK)
	s.Require().NotEqual(-1, continueAt)
	nakAt := strings.Index(response[continueAt+len(continueACK):], "NAK\n")
	s.Require().NotEqual(-1, nakAt)
	finalAt := strings.Index(response[continueAt+len(continueACK)+nakAt:], finalACK)
	s.Require().NotEqual(-1, finalAt)
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
	testAdvertise(s.T(), ReceivePack, "version=2", false)
}

func (s *ReceivePackServeSuite) TestReceivePackAdvertiseV1() {
	buf := testAdvertise(s.T(), ReceivePack, "version=1", false)
	s.Contains(buf.String(), "version 1")
}

// TestUploadPackStatelessRPCUnreachableHavesEmitsSingleNAK verifies that when
// the client sends haves that are not reachable from any want, the server
// emits exactly one NAK pktline before the sideband-wrapped pack, not two.
//
// Previously, the upload-pack writer emitted an extra NAK in this case. It
// first called ServerResponse{ACKs: nil}.Encode, which wrote a NAK, and then
// fell through to the "ack.Hash.IsZero()" branch, which wrote a second NAK.
// packp.ServerResponse.Decode consumed only the first NAK, leaving the second
// "0008NAK\n" pktline in front of the sideband frames. The sideband demuxer
// then read "NAK\n" as a frame with channel byte 'N' (0x4E) and failed with
// "unknown channel NAK".
//
// A caller consuming the response with the standard go-git client pipeline
// (ServerResponse.Decode + sideband.Demuxer) cannot recover.
func (s *UploadPackServeSuite) TestUploadPackStatelessRPCUnreachableHavesEmitsSingleNAK() {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	defer func() { _ = st.Close() }()

	head, err := storer.ResolveReference(st, plumbing.HEAD)
	s.Require().NoError(err)

	var upreq packp.UploadRequest
	upreq.Capabilities.Add(capability.Sideband64k)
	upreq.Capabilities.Add(capability.NoProgress)
	upreq.Wants = append(upreq.Wants, head.Hash())

	// A hash the server definitely does not have and that is not reachable
	// from the want. This is the rewind / divergent-overwrite case: client
	// sends a "have" the server cannot match against the wants.
	unreachable := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")

	var uphav packp.UploadHaves
	uphav.Haves = []plumbing.Hash{unreachable}
	uphav.Done = true

	var reqW bytes.Buffer
	s.Require().NoError(upreq.Encode(&reqW))
	s.Require().NoError(uphav.Encode(&reqW))

	buf := testServe(s.T(), st, UploadPack, io.NopCloser(&reqW), &UploadPackRequest{
		GitProtocol:   "version=1",
		AdvertiseRefs: false,
		StatelessRPC:  true,
	})
	raw := buf.Bytes()

	// First: byte-level assertion. The response must not begin with two
	// consecutive NAK pktlines.
	const doubleNAK = "0008NAK\n0008NAK\n"
	s.Falsef(bytes.HasPrefix(raw, []byte(doubleNAK)),
		"response begins with two NAK pktlines, client-side sideband demux will fail:\n%s",
		prefixHex(raw, 64),
	)

	// Second: end-to-end assertion using the standard client pipeline.
	// ServerResponse.Decode + sideband.Demuxer + PACK signature read.
	rd := bytes.NewReader(raw)
	var srv packp.ServerResponse
	s.Require().NoError(srv.Decode(rd), "decode server response")

	demux := sideband.NewDemuxer(sideband.Sideband64k, rd)
	var signature [4]byte
	_, err = io.ReadFull(demux, signature[:])
	s.Require().NoError(err, "read PACK signature through sideband demuxer")
	s.Equal("PACK", string(signature[:]), "expected PACK magic after the NAK preamble")
}

func prefixHex(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	var sb strings.Builder
	for _, c := range b[:n] {
		if c >= 0x20 && c < 0x7f {
			sb.WriteByte(c)
		} else {
			sb.WriteString("\\x")
			const hexchars = "0123456789abcdef"
			sb.WriteByte(hexchars[c>>4])
			sb.WriteByte(hexchars[c&0x0f])
		}
	}
	return sb.String()
}
