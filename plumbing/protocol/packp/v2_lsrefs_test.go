package packp

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type LsRefsSuite struct {
	suite.Suite
}

func TestLsRefsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(LsRefsSuite))
}

// readPktTokens reads all pkt-lines from r, representing flush as "<flush>"
// and delim as "<delim>" so request framing can be asserted exactly.
func readPktTokens(t interface{ Errorf(string, ...any) }, r io.Reader) []string {
	var out []string
	for {
		l, p, err := pktline.ReadLine(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("readPktTokens: %v", err)
			break
		}
		switch l {
		case pktline.Flush:
			out = append(out, "<flush>")
		case pktline.Delim:
			out = append(out, "<delim>")
		default:
			out = append(out, string(bytes.TrimRight(p, "\n")))
		}
	}
	return out
}

func (s *LsRefsSuite) TestEncode() {
	req := &LsRefsRequest{
		Capabilities: []string{"agent=git/2.40.1", "object-format=sha1"},
		Symrefs:      true,
		Peel:         true,
		Unborn:       true,
		RefPrefixes:  []string{"refs/heads/", "refs/tags/"},
	}

	buf := bytes.NewBuffer(nil)
	s.Require().NoError(req.Encode(buf))

	tokens := readPktTokens(s.T(), buf)
	s.Equal([]string{
		"command=ls-refs",
		"agent=git/2.40.1",
		"object-format=sha1",
		"<delim>",
		"peel",
		"symrefs",
		"unborn",
		"ref-prefix refs/heads/",
		"ref-prefix refs/tags/",
		"<flush>",
	}, tokens)
}

func (s *LsRefsSuite) TestEncodeNoArgs() {
	req := &LsRefsRequest{}

	buf := bytes.NewBuffer(nil)
	s.Require().NoError(req.Encode(buf))

	tokens := readPktTokens(s.T(), buf)
	// The delim-pkt is mandatory in the v2 grammar and always emitted,
	// even when there are no command-specific arguments.
	s.Equal([]string{"command=ls-refs", "<delim>", "<flush>"}, tokens)
}

func (s *LsRefsSuite) TestDecode() {
	main := "1111111111111111111111111111111111111111"
	tag := "2222222222222222222222222222222222222222"
	peeled := "3333333333333333333333333333333333333333"

	buf := bytes.NewBuffer(nil)
	_, err := pktline.Writeln(buf, fmt.Sprintf("%s HEAD symref-target:refs/heads/main", main))
	s.Require().NoError(err)
	_, err = pktline.Writeln(buf, fmt.Sprintf("%s refs/heads/main", main))
	s.Require().NoError(err)
	_, err = pktline.Writeln(buf, fmt.Sprintf("%s refs/tags/v1 peeled:%s", tag, peeled))
	s.Require().NoError(err)
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp LsRefsResponse
	s.Require().NoError(resp.Decode(buf))

	refs := map[string]*plumbing.Reference{}
	for _, r := range resp.References {
		refs[r.Name().String()] = r
	}

	head := refs["HEAD"]
	s.Require().NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
	s.Equal("refs/heads/main", head.Target().String())

	m := refs["refs/heads/main"]
	s.Require().NotNil(m)
	s.Equal(plumbing.HashReference, m.Type())
	s.Equal(main, m.Hash().String())

	v1 := refs["refs/tags/v1"]
	s.Require().NotNil(v1)
	s.Equal(tag, v1.Hash().String())

	pr := refs["refs/tags/v1^{}"]
	s.Require().NotNil(pr)
	s.Equal(peeled, pr.Hash().String())
}

func (s *LsRefsSuite) TestDecodeEmpty() {
	buf := bytes.NewBuffer(nil)
	s.Require().NoError(pktline.WriteFlush(buf))

	var resp LsRefsResponse
	s.Require().NoError(resp.Decode(buf))
	s.Empty(resp.References)
}
