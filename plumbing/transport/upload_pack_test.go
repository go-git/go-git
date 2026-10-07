package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
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

// A have that is reachable from any of the wants must be recognised as common,
// not only one reachable from the first want.
//
// The request asks for multi_ack_detailed rather than multi_ack because only
// the former distinguishes the two outcomes: it answers "common" when the have
// is in the reachable set and "ready" when it is not. multi_ack answers
// "continue" either way, so it cannot tell a populated reachable set from an
// empty one.
func (s *UploadPackServeSuite) TestUploadPackCommonAcrossMultipleWants() {
	st := memory.NewStorage()
	sig := object.Signature{Name: "t", Email: "t@example.com", When: time.Unix(0, 0).UTC()}

	commit := func(content string, parents ...plumbing.Hash) plumbing.Hash {
		blob := &plumbing.MemoryObject{}
		blob.SetType(plumbing.BlobObject)
		_, err := blob.Write([]byte(content))
		s.Require().NoError(err)
		bh, err := st.SetEncodedObject(blob)
		s.Require().NoError(err)

		tree := &object.Tree{Entries: []object.TreeEntry{
			{Name: "f.txt", Mode: filemode.Regular, Hash: bh},
		}}
		to := &plumbing.MemoryObject{}
		s.Require().NoError(tree.Encode(to))
		th, err := st.SetEncodedObject(to)
		s.Require().NoError(err)

		c := &object.Commit{Author: sig, Committer: sig, Message: content, TreeHash: th, ParentHashes: parents}
		co := &plumbing.MemoryObject{}
		s.Require().NoError(c.Encode(co))
		ch, err := st.SetEncodedObject(co)
		s.Require().NoError(err)
		return ch
	}

	// base --- tipA          (want #1)
	//     \--- mid --- tipB  (want #2); mid is reachable only through tipB
	base := commit("base")
	tipA := commit("tipA", base)
	mid := commit("mid", base)
	tipB := commit("tipB", mid)

	s.Require().NoError(st.SetReference(plumbing.NewHashReference(plumbing.HEAD, tipA)))

	var upreq packp.UploadRequest
	upreq.Capabilities.Add(capability.MultiACKDetailed)
	upreq.Capabilities.Add(capability.NoProgress)
	upreq.Wants = []plumbing.Hash{tipA, tipB}

	var firstRound packp.UploadHaves
	firstRound.Haves = []plumbing.Hash{mid}

	var finalRound packp.UploadHaves
	finalRound.Done = true

	var req bytes.Buffer
	s.Require().NoError(upreq.Encode(&req))
	s.Require().NoError(firstRound.Encode(&req))
	s.Require().NoError(finalRound.Encode(&req))

	var out bytes.Buffer
	s.Require().NoError(UploadPack(context.Background(), st,
		io.NopCloser(&req), ioutil.WriteNopCloser(&out),
		&UploadPackRequest{GitProtocol: "version=1"}))

	s.Contains(out.String(), fmt.Sprintf("ACK %s common\n", mid),
		"a have reachable from the second want must be acknowledged as common")
	s.NotContains(out.String(), fmt.Sprintf("ACK %s ready\n", mid),
		"the have must not be reported as merely ready: it is reachable through the second want")
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

// TestReceivePackAdvertiseV2SmartHTTP verifies the receive-pack fallback when a
// client requests protocol v2 over smart HTTP. Protocol v2 has no push, so git
// ignores the request and serves a classic v0 advertisement (no version line),
// while http-backend still omits the "# service=..." smart reply for the v2
// request (builtin/receive-pack.c, http-backend.c get_info_refs).
func (s *ReceivePackServeSuite) TestReceivePackAdvertiseV2SmartHTTP() {
	adv := testAdvertise(s.T(), ReceivePack, "version=2", true).String()
	s.NotContains(adv, "# service=")
	s.NotContains(adv, "version 2")
	s.NotContains(adv, "version 1")
	s.Contains(adv, "refs/heads/master")
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

// uploadPackFixture is a repository whose object store holds more than its
// references reach: refs/heads/main and the annotated tag refs/tags/v1 are
// advertised, taggedCommit sits under the tag object, and orphanCommit is
// what a force-pushed branch leaves behind.
type uploadPackFixture struct {
	storer       storage.Storer
	mainCommit   plumbing.Hash
	tagObject    plumbing.Hash
	taggedCommit plumbing.Hash
	orphanCommit plumbing.Hash
	orphanBlob   plumbing.Hash
}

// encodableObject is the shape the object types share for writing themselves
// into a store.
type encodableObject interface {
	Encode(plumbing.EncodedObject) error
}

func newUploadPackFixture(t *testing.T) uploadPackFixture {
	t.Helper()

	st := memory.NewStorage()

	store := func(encoder encodableObject) plumbing.Hash {
		t.Helper()
		obj := st.NewEncodedObject()
		require.NoError(t, encoder.Encode(obj))
		hash, err := st.SetEncodedObject(obj)
		require.NoError(t, err)
		return hash
	}

	storeBlob := func(content string) plumbing.Hash {
		t.Helper()
		obj := &plumbing.MemoryObject{}
		obj.SetType(plumbing.BlobObject)
		_, err := obj.Write([]byte(content))
		require.NoError(t, err)
		hash, err := st.SetEncodedObject(obj)
		require.NoError(t, err)
		return hash
	}

	treeFor := func(name string, blob plumbing.Hash) plumbing.Hash {
		t.Helper()
		return store(&object.Tree{Entries: []object.TreeEntry{
			{Name: name, Mode: filemode.Regular, Hash: blob},
		}})
	}

	when := time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)
	who := object.Signature{Name: "go-git", Email: "go-git@example.com", When: when}

	mainCommit := store(&object.Commit{
		Author: who, Committer: who, Message: "main",
		TreeHash: treeFor("main.txt", storeBlob("main\n")),
	})
	require.NoError(t, st.SetReference(
		plumbing.NewHashReference("refs/heads/main", mainCommit),
	))

	taggedCommit := store(&object.Commit{
		Author: who, Committer: who, Message: "tagged",
		TreeHash:     treeFor("tagged.txt", storeBlob("tagged\n")),
		ParentHashes: []plumbing.Hash{mainCommit},
	})
	tagObject := store(&object.Tag{
		Name: "v1", Tagger: who, Message: "v1\n",
		Target: taggedCommit, TargetType: plumbing.CommitObject,
	})
	require.NoError(t, st.SetReference(
		plumbing.NewHashReference("refs/tags/v1", tagObject),
	))

	orphanBlob := storeBlob("AWS_SECRET_ACCESS_KEY=hunter2\n")
	orphanCommit := store(&object.Commit{
		Author: who, Committer: who, Message: "orphan",
		TreeHash:     treeFor("credentials", orphanBlob),
		ParentHashes: []plumbing.Hash{mainCommit},
	})

	return uploadPackFixture{
		storer:       st,
		mainCommit:   mainCommit,
		tagObject:    tagObject,
		taggedCommit: taggedCommit,
		orphanCommit: orphanCommit,
		orphanBlob:   orphanBlob,
	}
}

// serveWant runs one upload-pack exchange for a single want and returns the
// objects the server packed, or the refusal it wrote instead.
func serveWant(t *testing.T, st storage.Storer, want plumbing.Hash, statelessRPC bool) (storage.Storer, error) {
	t.Helper()

	upreq := &packp.UploadRequest{}
	upreq.Wants = append(upreq.Wants, want)

	var uphav packp.UploadHaves
	uphav.Done = true

	var request bytes.Buffer
	require.NoError(t, upreq.Encode(&request))
	require.NoError(t, uphav.Encode(&request))

	var response bytes.Buffer
	err := UploadPack(
		context.TODO(),
		st,
		io.NopCloser(&request),
		ioutil.WriteNopCloser(&response),
		&UploadPackRequest{StatelessRPC: statelessRPC},
	)
	if err != nil {
		return nil, err
	}

	pack := bytes.Index(response.Bytes(), []byte("PACK"))
	require.GreaterOrEqual(t, pack, 0, "no packfile in response")

	served := memory.NewStorage()
	parser := packfile.NewParser(
		bytes.NewReader(response.Bytes()[pack:]),
		packfile.WithStorage(served),
	)
	_, err = parser.Parse()
	require.NoError(t, err)
	return served, nil
}

// TestUploadPackRefusesUnadvertisedWant pins the two arms of upstream's
// is_our_ref / check_non_tip. An advertised reference value is always
// servable; a commit that is only reachable from one is servable over a
// stateless transport alone; an object no reference reaches is never
// servable.
func TestUploadPackRefusesUnadvertisedWant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		want       func(uploadPackFixture) plumbing.Hash
		servedWhen []bool // StatelessRPC values the want is served for
	}{
		{
			name:       "advertised branch tip",
			want:       func(f uploadPackFixture) plumbing.Hash { return f.mainCommit },
			servedWhen: []bool{false, true},
		},
		{
			name:       "advertised tag object",
			want:       func(f uploadPackFixture) plumbing.Hash { return f.tagObject },
			servedWhen: []bool{false, true},
		},
		{
			name:       "commit under an annotated tag",
			want:       func(f uploadPackFixture) plumbing.Hash { return f.taggedCommit },
			servedWhen: []bool{true},
		},
		{
			name:       "commit no reference reaches",
			want:       func(f uploadPackFixture) plumbing.Hash { return f.orphanCommit },
			servedWhen: nil,
		},
	} {
		for _, statelessRPC := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stateless=%v", tc.name, statelessRPC), func(t *testing.T) {
				t.Parallel()

				fixture := newUploadPackFixture(t)
				want := tc.want(fixture)
				served, err := serveWant(t, fixture.storer, want, statelessRPC)

				if !slices.Contains(tc.servedWhen, statelessRPC) {
					require.ErrorIs(t, err, ErrNotOurRef)
					assert.Contains(t, err.Error(), want.String())
					return
				}

				require.NoError(t, err)
				_, err = served.EncodedObject(plumbing.AnyObject, want)
				assert.NoError(t, err, "want missing from the packfile")
			})
		}
	}
}

// TestUploadPackUnadvertisedWantKeepsObjectsBack is the leak the gate closes:
// the blob under an unreferenced commit travels with it, so a peer holding
// only the commit id walks away with the contents too.
func TestUploadPackUnadvertisedWantKeepsObjectsBack(t *testing.T) {
	t.Parallel()

	fixture := newUploadPackFixture(t)

	_, err := serveWant(t, fixture.storer, fixture.orphanCommit, true)
	require.ErrorIs(t, err, ErrNotOurRef)

	served, err := serveWant(t, fixture.storer, fixture.mainCommit, true)
	require.NoError(t, err)
	_, err = served.EncodedObject(plumbing.AnyObject, fixture.orphanBlob)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}
