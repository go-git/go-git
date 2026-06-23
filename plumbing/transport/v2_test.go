package transport

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

type V2SessionSuite struct {
	suite.Suite
}

func TestV2SessionSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(V2SessionSuite))
}

// fakeRunner returns queued responses in order and records every request.
type fakeRunner struct {
	requests  [][]byte
	responses [][]byte
	i         int
}

func (f *fakeRunner) Run(_ context.Context, req []byte) (io.ReadCloser, error) {
	f.requests = append(f.requests, req)
	resp := f.responses[f.i]
	f.i++
	return io.NopCloser(bytes.NewReader(resp)), nil
}

func (f *fakeRunner) Close() error { return nil }

func capsFor(t interface{ Fatal(...any) }, lines ...string) packp.V2Capabilities {
	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "version 2")
	for _, l := range lines {
		_, _ = pktline.Writeln(buf, l)
	}
	_ = pktline.WriteFlush(buf)
	var c packp.V2Capabilities
	if err := c.Decode(buf); err != nil {
		t.Fatal(err)
	}
	return c
}

func emptyPack() []byte {
	pack := make([]byte, 12, 12+sha1.Size)
	copy(pack, []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 0})
	sum := sha1.Sum(pack)
	return append(pack, sum[:]...)
}

// packfileResponse builds a fetch response whose only section is a
// sideband-multiplexed packfile carrying pack.
func packfileResponse(pack []byte) []byte {
	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "packfile")
	_, _ = pktline.Write(buf, append([]byte{1}, pack...))
	_ = pktline.WriteFlush(buf)
	return buf.Bytes()
}

// acksResponse builds a fetch response carrying only an acknowledgments
// section (no packfile), so negotiation continues for another round. With no
// acks it reports NAK.
func acksResponse(acks ...string) []byte {
	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "acknowledgments")
	if len(acks) == 0 {
		_, _ = pktline.Writeln(buf, "NAK")
	}
	for _, a := range acks {
		_, _ = pktline.Writeln(buf, "ACK "+a)
	}
	_ = pktline.WriteFlush(buf)
	return buf.Bytes()
}

// hashes returns n distinct hashes derived from their index.
func hashes(n int) []plumbing.Hash {
	out := make([]plumbing.Hash, n)
	for i := range out {
		out[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
	}
	return out
}

func (s *V2SessionSuite) TestGetRemoteRefs() {
	main := "1111111111111111111111111111111111111111"
	resp := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(resp, main+" HEAD symref-target:refs/heads/main")
	_, _ = pktline.Writeln(resp, main+" refs/heads/main")
	_ = pktline.WriteFlush(resp)

	runner := &fakeRunner{responses: [][]byte{resp.Bytes()}}
	caps := capsFor(s.T(), "ls-refs=unborn", "object-format=sha1")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	refs, err := sess.GetRemoteRefs(context.Background())
	s.Require().NoError(err)

	names := map[string]string{}
	for _, r := range refs {
		if r.Type() == plumbing.SymbolicReference {
			names[r.Name().String()] = r.Target().String()
		} else {
			names[r.Name().String()] = r.Hash().String()
		}
	}
	s.Equal("refs/heads/main", names["HEAD"])
	s.Equal(main, names["refs/heads/main"])

	s.Require().Len(runner.requests, 1)
	req := string(runner.requests[0])
	s.Contains(req, "command=ls-refs")
	s.Contains(req, "symrefs")
	s.Contains(req, "unborn")

	// Second call must be cached (no extra request).
	_, err = sess.GetRemoteRefs(context.Background())
	s.Require().NoError(err)
	s.Len(runner.requests, 1)
}

func (s *V2SessionSuite) TestFetchClone() {
	want := "1111111111111111111111111111111111111111"
	runner := &fakeRunner{responses: [][]byte{packfileResponse(emptyPack())}}
	caps := capsFor(s.T(), "fetch=shallow filter", "object-format=sha1")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	st := memory.NewStorage()
	err := sess.Fetch(context.Background(), st, &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash(want)},
	})
	s.Require().NoError(err)

	s.Require().Len(runner.requests, 1)
	req := string(runner.requests[0])
	s.Contains(req, "command=fetch")
	s.Contains(req, "want "+want)
	s.Contains(req, "done")
}

func (s *V2SessionSuite) TestFetchFilterUnsupported() {
	runner := &fakeRunner{}
	caps := capsFor(s.T(), "fetch=shallow", "object-format=sha1")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
		Wants:  []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		Filter: "blob:none",
	})
	s.Require().Error(err)
	s.True(strings.Contains(err.Error(), "filter") || err == ErrFilterNotSupported)
}

func (s *V2SessionSuite) TestPushUnsupported() {
	sess := NewV2Session(&fakeRunner{}, capsFor(s.T()), UploadPackService, false)
	err := sess.Push(context.Background(), memory.NewStorage(), &PushRequest{})
	s.Require().Error(err)
}

// Protocol v2 is stateless server-side even on a persistent stream
// connection, so acknowledged common commits must be resent every round
// regardless of transport.
func (s *V2SessionSuite) TestFetchStreamResendsCommon() {
	hs := hashes(18)
	ack := hs[0].String()
	runner := &fakeRunner{responses: [][]byte{
		acksResponse(ack),
		packfileResponse(emptyPack()),
	}}
	caps := capsFor(s.T(), "fetch", "object-format=sha1")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		Haves: hs,
	})
	s.Require().NoError(err)

	s.Require().Len(runner.requests, 2)
	// The hash acknowledged in round 1 must be resent as a have in round 2,
	// even though this is a (non-stateless) stream session.
	s.Contains(string(runner.requests[1]), "have "+ack)
}

// Once a common base is acknowledged, the client stops negotiating and asks
// for the pack after maxInVein haves are sent without further progress,
// rather than draining every local have.
func (s *V2SessionSuite) TestFetchMaxInVain() {
	runner := &fakeRunner{responses: [][]byte{
		acksResponse(hashes(1)[0].String()), // round 1: establish a common base
		acksResponse(),                      // round 2: NAK
		acksResponse(),                      // round 3: NAK
		acksResponse(),                      // round 4: NAK
		packfileResponse(emptyPack()),       // round 5: done -> pack
	}}
	caps := capsFor(s.T(), "fetch", "object-format=sha1")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		Haves: hashes(400),
	})
	s.Require().NoError(err)

	s.Require().Len(runner.requests, 5)
	s.Contains(string(runner.requests[4]), "done")
}

func (s *V2SessionSuite) TestFetchUnsupportedObjectFormat() {
	runner := &fakeRunner{}
	caps := capsFor(s.T(), "fetch", "object-format=sha999")
	sess := NewV2Session(runner, caps, UploadPackService, false)

	err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "object-format")
	s.Empty(runner.requests)
}

// The agent capability must only be sent when the server advertised it.
func (s *V2SessionSuite) TestFetchAgentGatedOnAdvertisement() {
	fetch := func(caps packp.V2Capabilities) string {
		runner := &fakeRunner{responses: [][]byte{packfileResponse(emptyPack())}}
		sess := NewV2Session(runner, caps, UploadPackService, false)
		err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
			Wants: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		})
		s.Require().NoError(err)
		s.Require().Len(runner.requests, 1)
		return string(runner.requests[0])
	}

	s.Contains(fetch(capsFor(s.T(), "agent=git/2.40.1", "object-format=sha1")), "agent=")
	s.NotContains(fetch(capsFor(s.T(), "object-format=sha1")), "agent=")
}
