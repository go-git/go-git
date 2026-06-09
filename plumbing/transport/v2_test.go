package transport

import (
	"bytes"
	"context"
	"crypto/sha1"
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
	header := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 0}
	sum := sha1.Sum(header)
	return append(header, sum[:]...)
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

func (s *V2SessionSuite) TestGetRemoteRefs() {
	main := "1111111111111111111111111111111111111111"
	resp := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(resp, main+" HEAD symref-target:refs/heads/main")
	_, _ = pktline.Writeln(resp, main+" refs/heads/main")
	_ = pktline.WriteFlush(resp)

	runner := &fakeRunner{responses: [][]byte{resp.Bytes()}}
	caps := capsFor(s.T(), "ls-refs=unborn", "object-format=sha1")
	sess := newV2Session(runner, caps, UploadPackService, false)

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
	sess := newV2Session(runner, caps, UploadPackService, false)

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
	sess := newV2Session(runner, caps, UploadPackService, false)

	err := sess.Fetch(context.Background(), memory.NewStorage(), &FetchRequest{
		Wants:  []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		Filter: "blob:none",
	})
	s.Require().Error(err)
	s.True(strings.Contains(err.Error(), "filter") || err == ErrFilterNotSupported)
}

func (s *V2SessionSuite) TestPushUnsupported() {
	sess := newV2Session(&fakeRunner{}, capsFor(s.T()), UploadPackService, false)
	err := sess.Push(context.Background(), memory.NewStorage(), &PushRequest{})
	s.Require().Error(err)
}
