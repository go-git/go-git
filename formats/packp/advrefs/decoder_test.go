package advrefs_test

import (
	"bytes"
	"io"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp"
	"gopkg.in/src-d/go-git.v4/formats/packp/advrefs"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"

	. "gopkg.in/check.v1"
)

type SuiteDecoder struct{}

var _ = Suite(&SuiteDecoder{})

func (s *SuiteDecoder) TestEmpty(c *C) {
	ar := advrefs.New()
	var buf bytes.Buffer
	d := advrefs.NewDecoder(&buf)

	err := d.Decode(ar)
	c.Assert(err, Equals, advrefs.ErrEmpty)
}

func (s *SuiteDecoder) TestShortForHash(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*too short")
}

func toPktLines(c *C, payloads []string) io.Reader {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString(payloads...)
	c.Assert(err, IsNil)

	return &buf
}

func testDecoderErrorMatches(c *C, input io.Reader, pattern string) {
	ar := advrefs.New()
	d := advrefs.NewDecoder(input)

	err := d.Decode(ar)
	c.Assert(err, ErrorMatches, pattern)
}

func (s *SuiteDecoder) TestInvalidFirstHash(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796alberto2219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*invalid hash.*")
}

func (s *SuiteDecoder) TestZeroId(c *C) {
	payloads := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00multi_ack thin-pack\n",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(ar.Head, IsNil)
}

func testDecodeOK(c *C, payloads []string) *advrefs.AdvRefs {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString(payloads...)
	c.Assert(err, IsNil)

	ar := advrefs.New()
	d := advrefs.NewDecoder(&buf)

	err = d.Decode(ar)
	c.Assert(err, IsNil)

	return ar
}

func (s *SuiteDecoder) TestMalformedZeroId(c *C) {
	payloads := []string{
		"0000000000000000000000000000000000000000 wrong\x00multi_ack thin-pack\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed zero-id.*")
}

func (s *SuiteDecoder) TestShortZeroId(c *C) {
	payloads := []string{
		"0000000000000000000000000000000000000000 capabi",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*too short zero-id.*")
}

func (s *SuiteDecoder) TestHead(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(*ar.Head, Equals,
		core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *SuiteDecoder) TestFirstIsNotHead(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\x00",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(ar.Head, IsNil)
	c.Assert(ar.References["refs/heads/master"], Equals,
		core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *SuiteDecoder) TestShortRef(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 H",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*too short.*")
}

func (s *SuiteDecoder) TestNoNULL(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADofs-delta multi_ack",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*NULL not found.*")
}

func (s *SuiteDecoder) TestNoSpaceAfterHash(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5-HEAD\x00",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*no space after hash.*")
}

func (s *SuiteDecoder) TestNoCaps(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(ar.Capabilities.IsEmpty(), Equals, true)
}

func (s *SuiteDecoder) TestCaps(c *C) {
	for _, test := range [...]struct {
		input        []string
		capabilities []packp.Capability
	}{
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00\n",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{
				{
					Name:   "ofs-delta",
					Values: []string(nil),
				},
			},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta multi_ack",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{
				{Name: "ofs-delta", Values: []string(nil)},
				{Name: "multi_ack", Values: []string(nil)},
			},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta multi_ack\n",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{
				{Name: "ofs-delta", Values: []string(nil)},
				{Name: "multi_ack", Values: []string(nil)},
			},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:refs/heads/master agent=foo=bar\n",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{
				{Name: "symref", Values: []string{"HEAD:refs/heads/master"}},
				{Name: "agent", Values: []string{"foo=bar"}},
			},
		},
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:refs/heads/master agent=foo=bar agent=new-agent\n",
				pktline.FlushString,
			},
			capabilities: []packp.Capability{
				{Name: "symref", Values: []string{"HEAD:refs/heads/master"}},
				{Name: "agent", Values: []string{"foo=bar", "new-agent"}},
			},
		},
	} {
		ar := testDecodeOK(c, test.input)
		for _, fixCap := range test.capabilities {
			c.Assert(ar.Capabilities.Supports(fixCap.Name), Equals, true,
				Commentf("input = %q, capability = %q", test.input, fixCap.Name))
			c.Assert(ar.Capabilities.Get(fixCap.Name).Values, DeepEquals, fixCap.Values,
				Commentf("input = %q, capability = %q", test.input, fixCap.Name))
		}
	}
}

func (s *SuiteDecoder) TestWithPrefix(c *C) {
	payloads := []string{
		"# this is a prefix\n",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00foo\n",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(len(ar.Prefix), Equals, 1)
	c.Assert(ar.Prefix[0], DeepEquals, []byte("# this is a prefix"))
}

func (s *SuiteDecoder) TestWithPrefixAndFlush(c *C) {
	payloads := []string{
		"# this is a prefix\n",
		pktline.FlushString,
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00foo\n",
		pktline.FlushString,
	}
	ar := testDecodeOK(c, payloads)
	c.Assert(len(ar.Prefix), Equals, 2)
	c.Assert(ar.Prefix[0], DeepEquals, []byte("# this is a prefix"))
	c.Assert(ar.Prefix[1], DeepEquals, []byte(pktline.FlushString))
}

func (s *SuiteDecoder) TestOtherRefs(c *C) {
	for _, test := range [...]struct {
		input      []string
		references map[string]core.Hash
		peeled     map[string]core.Hash
	}{
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				pktline.FlushString,
			},
			references: make(map[string]core.Hash),
			peeled:     make(map[string]core.Hash),
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"1111111111111111111111111111111111111111 ref/foo",
				pktline.FlushString,
			},
			references: map[string]core.Hash{
				"ref/foo": core.NewHash("1111111111111111111111111111111111111111"),
			},
			peeled: make(map[string]core.Hash),
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"1111111111111111111111111111111111111111 ref/foo\n",
				pktline.FlushString,
			},
			references: map[string]core.Hash{
				"ref/foo": core.NewHash("1111111111111111111111111111111111111111"),
			},
			peeled: make(map[string]core.Hash),
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"1111111111111111111111111111111111111111 ref/foo\n",
				"2222222222222222222222222222222222222222 ref/bar",
				pktline.FlushString,
			},
			references: map[string]core.Hash{
				"ref/foo": core.NewHash("1111111111111111111111111111111111111111"),
				"ref/bar": core.NewHash("2222222222222222222222222222222222222222"),
			},
			peeled: make(map[string]core.Hash),
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"1111111111111111111111111111111111111111 ref/foo^{}\n",
				pktline.FlushString,
			},
			references: make(map[string]core.Hash),
			peeled: map[string]core.Hash{
				"ref/foo": core.NewHash("1111111111111111111111111111111111111111"),
			},
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"1111111111111111111111111111111111111111 ref/foo\n",
				"2222222222222222222222222222222222222222 ref/bar^{}",
				pktline.FlushString,
			},
			references: map[string]core.Hash{
				"ref/foo": core.NewHash("1111111111111111111111111111111111111111"),
			},
			peeled: map[string]core.Hash{
				"ref/bar": core.NewHash("2222222222222222222222222222222222222222"),
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
				pktline.FlushString,
			},
			references: map[string]core.Hash{
				"refs/heads/master":      core.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
				"refs/pull/10/head":      core.NewHash("51b8b4fb32271d39fbdd760397406177b2b0fd36"),
				"refs/pull/100/head":     core.NewHash("02b5a6031ba7a8cbfde5d65ff9e13ecdbc4a92ca"),
				"refs/pull/100/merge":    core.NewHash("c284c212704c43659bf5913656b8b28e32da1621"),
				"refs/pull/101/merge":    core.NewHash("3d6537dce68c8b7874333a1720958bd8db3ae8ca"),
				"refs/tags/v2.6.11":      core.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
				"refs/tags/v2.6.11-tree": core.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
			},
			peeled: map[string]core.Hash{
				"refs/tags/v2.6.11":      core.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"),
				"refs/tags/v2.6.11-tree": core.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"),
			},
		},
	} {
		ar := testDecodeOK(c, test.input)
		comment := Commentf("input = %v\n", test.input)
		c.Assert(ar.References, DeepEquals, test.references, comment)
		c.Assert(ar.Peeled, DeepEquals, test.peeled, comment)
	}
}

func (s *SuiteDecoder) TestMalformedOtherRefsNoSpace(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8crefs/tags/v2.6.11\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed ref data.*")
}

func (s *SuiteDecoder) TestMalformedOtherRefsMultipleSpaces(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack thin-pack\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags v2.6.11\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed ref data.*")
}

func (s *SuiteDecoder) TestShallow(c *C) {
	for _, test := range [...]struct {
		input    []string
		shallows []core.Hash
	}{
		{
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
				"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
				"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
				pktline.FlushString,
			},
			shallows: []core.Hash{},
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
				"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
				"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
				"shallow 1111111111111111111111111111111111111111\n",
				pktline.FlushString,
			},
			shallows: []core.Hash{core.NewHash("1111111111111111111111111111111111111111")},
		}, {
			input: []string{
				"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
				"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
				"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
				"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
				"shallow 1111111111111111111111111111111111111111\n",
				"shallow 2222222222222222222222222222222222222222\n",
				pktline.FlushString,
			},
			shallows: []core.Hash{
				core.NewHash("1111111111111111111111111111111111111111"),
				core.NewHash("2222222222222222222222222222222222222222"),
			},
		},
	} {
		ar := testDecodeOK(c, test.input)
		comment := Commentf("input = %v\n", test.input)
		c.Assert(ar.Shallows, DeepEquals, test.shallows, comment)
	}
}

func (s *SuiteDecoder) TestInvalidShallowHash(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 11111111alcortes111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*invalid hash text.*")
}

func (s *SuiteDecoder) TestGarbageAfterShallow(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"b5be40b90dbaa6bd337f3b77de361bfc0723468b refs/tags/v4.4",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed shallow prefix.*")
}

func (s *SuiteDecoder) TestMalformedShallowHash(c *C) {
	payloads := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222 malformed\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed shallow hash.*")
}

func (s *SuiteDecoder) TestEOFRefs(c *C) {
	input := strings.NewReader("" +
		"005b6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n" +
		"003fa6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n" +
		"00355dc01c595e6c6ec9ccda4f6ffbf614e4d92bb0c7 refs/foo\n",
	)
	testDecoderErrorMatches(c, input, ".*invalid pkt-len.*")
}

func (s *SuiteDecoder) TestEOFShallows(c *C) {
	input := strings.NewReader("" +
		"005b6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta symref=HEAD:/refs/heads/master\n" +
		"003fa6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n" +
		"00445dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n" +
		"0047c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n" +
		"0035shallow 1111111111111111111111111111111111111111\n" +
		"0034shallow 222222222222222222222222")
	testDecoderErrorMatches(c, input, ".*unexpected EOF.*")
}
