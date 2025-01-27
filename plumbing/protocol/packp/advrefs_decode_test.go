package packp

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type AdvRefsDecodeSuite struct {
	suite.Suite
}

func TestAdvRefsDecodeSuite(t *testing.T) {
	suite.Run(t, new(AdvRefsDecodeSuite))
}

func (s *AdvRefsDecodeSuite) TestEmpty() {
	var buf bytes.Buffer
	ar := NewAdvRefs()
	s.Equal(ErrEmptyInput, ar.Decode(&buf))
}

func (s *AdvRefsDecodeSuite) TestEmptyFlush() {
	var buf bytes.Buffer
	pktline.WriteFlush(&buf)
	ar := NewAdvRefs()
	s.Equal(ErrEmptyAdvRefs, ar.Decode(&buf))
}

func (s *AdvRefsDecodeSuite) TestShortForHash() {
	payloads := []string{
		"6ecf0ef2c2dffb796",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*too short.*")
}

func (s *AdvRefsDecodeSuite) testDecoderErrorMatches(input io.Reader, pattern string) {
	ar := NewAdvRefs()
	err := ar.Decode(input)
	s.Error(err)
	if err != nil {
		s.Regexp(regexp.MustCompile(pattern), err.Error())
	}
}

func (s *AdvRefsDecodeSuite) TestInvalidFirstHash() {
	payloads := []string{
		"6ecf0ef2c2dffb796alberto2219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*invalid hash.*")
}

func (s *AdvRefsDecodeSuite) TestZeroId() {
	payloads := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00multi_ack thin-pack\n",
		"",
	}
	ar := s.testDecodeOK(payloads)
	s.Nil(ar.Head)
}

func (s *AdvRefsDecodeSuite) testDecodeOK(payloads []string) *AdvRefs {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			s.Nil(pktline.WriteFlush(&buf))
		} else {
			_, err := pktline.WriteString(&buf, p)
			s.NoError(err)
		}
	}

	ar := NewAdvRefs()
	s.Nil(ar.Decode(&buf))

	return ar
}

func (s *AdvRefsDecodeSuite) TestMalformedZeroId() {
	payloads := []string{
		"0000000000000000000000000000000000000000 wrong\x00multi_ack thin-pack\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed zero-id.*")
}

func (s *AdvRefsDecodeSuite) TestShortZeroId() {
	payloads := []string{
		"0000000000000000000000000000000000000000 capabi",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*too short zero-id.*")
}

func (s *AdvRefsDecodeSuite) TestHead() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
		"",
	}
	ar := s.testDecodeOK(payloads)
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		*ar.Head)
}

func (s *AdvRefsDecodeSuite) TestFirstIsNotHead() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\x00",
		"",
	}
	ar := s.testDecodeOK(payloads)
	s.Nil(ar.Head)
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		ar.References["refs/heads/master"])
}

func (s *AdvRefsDecodeSuite) TestShortRef() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 H",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*too short.*")
}

func (s *AdvRefsDecodeSuite) TestNoNULL() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADofs-delta multi_ack",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*NULL not found.*")
}

func (s *AdvRefsDecodeSuite) TestNoSpaceAfterHash() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5-HEAD\x00",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*no space after hash.*")
}

func (s *AdvRefsDecodeSuite) TestNoCaps() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
		"",
	}
	ar := s.testDecodeOK(payloads)
	s.True(ar.Capabilities.IsEmpty())
}

func (s *AdvRefsDecodeSuite) TestCaps() {
	type entry struct {
		Name   capability.Capability
		Values []string
	}

	for _, test := range [...]struct {
		input        []string
		capabilities []entry
	}{{
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
			"",
		},
		capabilities: []entry{},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00\n",
			"",
		},
		capabilities: []entry{},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta",
			"",
		},
		capabilities: []entry{
			{
				Name:   capability.OFSDelta,
				Values: []string(nil),
			},
		},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta multi_ack",
			"",
		},
		capabilities: []entry{
			{Name: capability.OFSDelta, Values: []string(nil)},
			{Name: capability.MultiACK, Values: []string(nil)},
		},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta multi_ack\n",
			"",
		},
		capabilities: []entry{
			{Name: capability.OFSDelta, Values: []string(nil)},
			{Name: capability.MultiACK, Values: []string(nil)},
		},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:refs/heads/master agent=foo=bar\n",
			"",
		},
		capabilities: []entry{
			{Name: capability.SymRef, Values: []string{"HEAD:refs/heads/master"}},
			{Name: capability.Agent, Values: []string{"foo=bar"}},
		},
	}, {
		input: []string{
			"0000000000000000000000000000000000000000 capabilities^{}\x00report-status report-status-v2 delete-refs side-band-64k quiet atomic ofs-delta object-format=sha1 agent=git/2.41.0\n",
			"",
		},
		capabilities: []entry{
			{Name: capability.ReportStatus, Values: []string(nil)},
			{Name: capability.ObjectFormat, Values: []string{"sha1"}},
			{Name: capability.Agent, Values: []string{"git/2.41.0"}},
		},
	}} {
		ar := s.testDecodeOK(test.input)
		for _, fixCap := range test.capabilities {
			s.True(ar.Capabilities.Supports(fixCap.Name),
				fmt.Sprintf("input = %q, capability = %q", test.input, fixCap.Name))
			s.Equal(fixCap.Values, ar.Capabilities.Get(fixCap.Name),
				fmt.Sprintf("input = %q, capability = %q", test.input, fixCap.Name))
		}
	}
}

func (s *AdvRefsDecodeSuite) TestOtherRefs() {
	for _, test := range [...]struct {
		input      []string
		references map[string]plumbing.Hash
		peeled     map[string]plumbing.Hash
	}{{
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"",
		},
		references: make(map[string]plumbing.Hash),
		peeled:     make(map[string]plumbing.Hash),
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"1111111111111111111111111111111111111111 ref/foo",
			"",
		},
		references: map[string]plumbing.Hash{
			"ref/foo": plumbing.NewHash("1111111111111111111111111111111111111111"),
		},
		peeled: make(map[string]plumbing.Hash),
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"1111111111111111111111111111111111111111 ref/foo\n",
			"",
		},
		references: map[string]plumbing.Hash{
			"ref/foo": plumbing.NewHash("1111111111111111111111111111111111111111"),
		},
		peeled: make(map[string]plumbing.Hash),
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"1111111111111111111111111111111111111111 ref/foo\n",
			"2222222222222222222222222222222222222222 ref/bar",
			"",
		},
		references: map[string]plumbing.Hash{
			"ref/foo": plumbing.NewHash("1111111111111111111111111111111111111111"),
			"ref/bar": plumbing.NewHash("2222222222222222222222222222222222222222"),
		},
		peeled: make(map[string]plumbing.Hash),
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"1111111111111111111111111111111111111111 ref/foo^{}\n",
			"",
		},
		references: make(map[string]plumbing.Hash),
		peeled: map[string]plumbing.Hash{
			"ref/foo": plumbing.NewHash("1111111111111111111111111111111111111111"),
		},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"1111111111111111111111111111111111111111 ref/foo\n",
			"2222222222222222222222222222222222222222 ref/bar^{}",
			"",
		},
		references: map[string]plumbing.Hash{
			"ref/foo": plumbing.NewHash("1111111111111111111111111111111111111111"),
		},
		peeled: map[string]plumbing.Hash{
			"ref/bar": plumbing.NewHash("2222222222222222222222222222222222222222"),
		},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
			"51b8b4fb32271d39fbdd760397406177b2b0fd36 refs/pull/10/head\n",
			"02b5a6031ba7a8cbfde5d65ff9e13ecdbc4a92ca refs/pull/100/head\n",
			"c284c212704c43659bf5913656b8b28e32da1621 refs/pull/100/merge\n",
			"3d6537dce68c8b7874333a1720958bd8db3ae8ca refs/pull/101/merge\n",
			"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11\n",
			"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11^{}\n",
			"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
			"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
			"",
		},
		references: map[string]plumbing.Hash{
			"refs/heads/master":      plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
			"refs/pull/10/head":      plumbing.NewHash("51b8b4fb32271d39fbdd760397406177b2b0fd36"),
			"refs/pull/100/head":     plumbing.NewHash("02b5a6031ba7a8cbfde5d65ff9e13ecdbc4a92ca"),
			"refs/pull/100/merge":    plumbing.NewHash("c284c212704c43659bf5913656b8b28e32da1621"),
			"refs/pull/101/merge":    plumbing.NewHash("3d6537dce68c8b7874333a1720958bd8db3ae8ca"),
			"refs/tags/v2.6.11":      plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
			"refs/tags/v2.6.11-tree": plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
		},
		peeled: map[string]plumbing.Hash{
			"refs/tags/v2.6.11":      plumbing.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"),
			"refs/tags/v2.6.11-tree": plumbing.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"),
		},
	}} {
		ar := s.testDecodeOK(test.input)
		comment := fmt.Sprintf("input = %v\n", test.input)
		s.Equal(test.references, ar.References, comment)
		s.Equal(test.peeled, ar.Peeled, comment)
	}
}

func (s *AdvRefsDecodeSuite) TestMalformedOtherRefsNoSpace() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8crefs/tags/v2.6.11\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed ref data.*")
}

func (s *AdvRefsDecodeSuite) TestMalformedOtherRefsMultipleSpaces() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags v2.6.11\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed ref data.*")
}

func (s *AdvRefsDecodeSuite) TestShallow() {
	for _, test := range [...]struct {
		input    []string
		shallows []plumbing.Hash
	}{{
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
			"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
			"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
			"",
		},
		shallows: []plumbing.Hash{},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
			"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
			"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
			"shallow 1111111111111111111111111111111111111111\n",
			"",
		},
		shallows: []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
	}, {
		input: []string{
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
			"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
			"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
			"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
			"shallow 1111111111111111111111111111111111111111\n",
			"shallow 2222222222222222222222222222222222222222\n",
			"",
		},
		shallows: []plumbing.Hash{
			plumbing.NewHash("1111111111111111111111111111111111111111"),
			plumbing.NewHash("2222222222222222222222222222222222222222"),
		},
	}} {
		ar := s.testDecodeOK(test.input)
		comment := fmt.Sprintf("input = %v\n", test.input)
		s.Equal(test.shallows, ar.Shallows, comment)
	}
}

func (s *AdvRefsDecodeSuite) TestInvalidShallowHash() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 11111111alcortes111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*invalid hash text.*")
}

func (s *AdvRefsDecodeSuite) TestGarbageAfterShallow() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"b5be40b90dbaa6bd337f3b77de361bfc0723468b refs/tags/v4.4",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed shallow prefix.*")
}

func (s *AdvRefsDecodeSuite) TestMalformedShallowHash() {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222 malformed\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed shallow hash.*")
}

func (s *AdvRefsDecodeSuite) TestEOFRefs() {
	input := strings.NewReader("" +
		"005b6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n" +
		"003fa6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n" +
		"00355dc01c595e6c6ec9ccda4f6ffbf614e4d92bb0c7 refs/foo\n",
	)
	s.testDecoderErrorMatches(input, ".*invalid pkt-len.*")
}

func (s *AdvRefsDecodeSuite) TestEOFShallows() {
	input := strings.NewReader("" +
		"005b6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n" +
		"003fa6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n" +
		"00445dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n" +
		"0047c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n" +
		"0035shallow 1111111111111111111111111111111111111111\n" +
		"0034shallow 222222222222222222222222")
	s.testDecoderErrorMatches(input, ".*unexpected EOF.*")
}
