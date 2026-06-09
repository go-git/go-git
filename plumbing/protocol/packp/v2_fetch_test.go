package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type FetchV2Suite struct {
	suite.Suite
}

func TestFetchV2Suite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(FetchV2Suite))
}

func (s *FetchV2Suite) TestEncode() {
	want := "1111111111111111111111111111111111111111"
	have := "2222222222222222222222222222222222222222"
	shallow := "3333333333333333333333333333333333333333"

	req := &FetchRequestV2{
		Capabilities: []string{"agent=git/2.40.1", "object-format=sha1"},
		Wants:        []plumbing.Hash{plumbing.NewHash(want)},
		Haves:        []plumbing.Hash{plumbing.NewHash(have)},
		Shallows:     []plumbing.Hash{plumbing.NewHash(shallow)},
		Depth:        1,
		Filter:       "blob:none",
		ThinPack:     true,
		OfsDelta:     true,
		IncludeTag:   true,
		NoProgress:   true,
		Done:         true,
	}

	buf := bytes.NewBuffer(nil)
	s.Require().NoError(req.Encode(buf))

	tokens := readPktTokens(s.T(), buf)
	s.Equal([]string{
		"command=fetch",
		"agent=git/2.40.1",
		"object-format=sha1",
		"<delim>",
		"thin-pack",
		"no-progress",
		"include-tag",
		"ofs-delta",
		"want " + want,
		"shallow " + shallow,
		"deepen 1",
		"filter blob:none",
		"have " + have,
		"done",
		"<flush>",
	}, tokens)
}

func (s *FetchV2Suite) TestDecodeAcknowledgmentsNotReady() {
	oid1 := "1111111111111111111111111111111111111111"
	oid2 := "2222222222222222222222222222222222222222"

	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "acknowledgments")
	_, _ = pktline.Writeln(buf, "ACK "+oid1)
	_, _ = pktline.Writeln(buf, "ACK "+oid2)
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp FetchResponseV2
	s.Require().NoError(resp.Decode(buf))

	s.False(resp.Ready)
	s.False(resp.HasPackfile)
	s.Equal([]plumbing.Hash{plumbing.NewHash(oid1), plumbing.NewHash(oid2)}, resp.Acks)
}

func (s *FetchV2Suite) TestDecodeNAK() {
	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "acknowledgments")
	_, _ = pktline.Writeln(buf, "NAK")
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp FetchResponseV2
	s.Require().NoError(resp.Decode(buf))
	s.Empty(resp.Acks)
	s.False(resp.Ready)
}

func (s *FetchV2Suite) TestDecodeFullResponse() {
	oid1 := "1111111111111111111111111111111111111111"
	oid2 := "2222222222222222222222222222222222222222"
	oid3 := "3333333333333333333333333333333333333333"

	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "acknowledgments")
	_, _ = pktline.Writeln(buf, "ACK "+oid1)
	_, _ = pktline.Writeln(buf, "ready")
	s.Require().NoError(pktline.WriteDelim(buf))
	_, _ = pktline.Writeln(buf, "shallow-info")
	_, _ = pktline.Writeln(buf, "shallow "+oid2)
	_, _ = pktline.Writeln(buf, "unshallow "+oid3)
	s.Require().NoError(pktline.WriteDelim(buf))
	_, _ = pktline.Writeln(buf, "packfile")
	_, _ = pktline.Write(buf, []byte{1, 'P', 'A', 'C', 'K'})
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp FetchResponseV2
	s.Require().NoError(resp.Decode(buf))

	s.True(resp.Ready)
	s.True(resp.HasPackfile)
	s.Equal([]plumbing.Hash{plumbing.NewHash(oid1)}, resp.Acks)
	s.Equal([]plumbing.Hash{plumbing.NewHash(oid2)}, resp.Shallows)
	s.Equal([]plumbing.Hash{plumbing.NewHash(oid3)}, resp.Unshallows)

	// The reader must be positioned at the packfile sideband stream.
	_, payload, err := pktline.ReadLine(buf)
	s.Require().NoError(err)
	s.Equal([]byte{1, 'P', 'A', 'C', 'K'}, payload)
}

func (s *FetchV2Suite) TestDecodePackfileOnly() {
	buf := bytes.NewBuffer(nil)
	_, _ = pktline.Writeln(buf, "packfile")
	_, _ = pktline.Write(buf, []byte{1, 'P', 'A', 'C', 'K'})
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp FetchResponseV2
	s.Require().NoError(resp.Decode(buf))

	s.True(resp.HasPackfile)
	s.False(resp.Ready)
}
