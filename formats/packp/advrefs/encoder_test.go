package advrefs_test

import (
	"bytes"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp"
	"gopkg.in/src-d/go-git.v4/formats/packp/advrefs"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"

	. "gopkg.in/check.v1"
)

type SuiteEncoder struct{}

var _ = Suite(&SuiteEncoder{})

// returns a byte slice with the pkt-lines for the given payloads.
func pktlines(c *C, payloads ...[]byte) []byte {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.Encode(payloads...)
	c.Assert(err, IsNil, Commentf("building pktlines for %v\n", payloads))

	return buf.Bytes()
}

func testEncode(c *C, input *advrefs.AdvRefs, expected []byte) {
	var buf bytes.Buffer
	e := advrefs.NewEncoder(&buf)
	err := e.Encode(input)
	c.Assert(err, IsNil)
	obtained := buf.Bytes()

	comment := Commentf("\nobtained = %s\nexpected = %s\n", string(obtained), string(expected))

	c.Assert(obtained, DeepEquals, expected, comment)
}

func (s *SuiteEncoder) TestZeroValue(c *C) {
	ar := &advrefs.AdvRefs{}

	expected := pktlines(c,
		[]byte("0000000000000000000000000000000000000000 capabilities^{}\x00\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestHead(c *C) {
	hash := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	ar := &advrefs.AdvRefs{
		Head: &hash,
	}

	expected := pktlines(c,
		[]byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestCapsNoHead(c *C) {
	capabilities := packp.NewCapabilities()
	capabilities.Add("symref", "HEAD:/refs/heads/master")
	capabilities.Add("ofs-delta")
	capabilities.Add("multi_ack")
	ar := &advrefs.AdvRefs{
		Capabilities: capabilities,
	}

	expected := pktlines(c,
		[]byte("0000000000000000000000000000000000000000 capabilities^{}\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestCapsWithHead(c *C) {
	hash := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	capabilities := packp.NewCapabilities()
	capabilities.Add("symref", "HEAD:/refs/heads/master")
	capabilities.Add("ofs-delta")
	capabilities.Add("multi_ack")
	ar := &advrefs.AdvRefs{
		Head:         &hash,
		Capabilities: capabilities,
	}

	expected := pktlines(c,
		[]byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestRefs(c *C) {
	references := map[string]core.Hash{
		"refs/heads/master":      core.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": core.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": core.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": core.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": core.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}
	ar := &advrefs.AdvRefs{
		References: references,
	}

	expected := pktlines(c,
		[]byte("0000000000000000000000000000000000000000 capabilities^{}\x00\n"),
		[]byte("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n"),
		[]byte("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n"),
		[]byte("1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n"),
		[]byte("2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n"),
		[]byte("3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestPeeled(c *C) {
	references := map[string]core.Hash{
		"refs/heads/master":      core.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": core.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": core.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": core.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": core.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}
	peeled := map[string]core.Hash{
		"refs/tags/v2.7.13-tree": core.NewHash("4444444444444444444444444444444444444444"),
		"refs/tags/v2.6.12-tree": core.NewHash("5555555555555555555555555555555555555555"),
	}
	ar := &advrefs.AdvRefs{
		References: references,
		Peeled:     peeled,
	}

	expected := pktlines(c,
		[]byte("0000000000000000000000000000000000000000 capabilities^{}\x00\n"),
		[]byte("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n"),
		[]byte("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n"),
		[]byte("1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n"),
		[]byte("5555555555555555555555555555555555555555 refs/tags/v2.6.12-tree^{}\n"),
		[]byte("2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n"),
		[]byte("3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n"),
		[]byte("4444444444444444444444444444444444444444 refs/tags/v2.7.13-tree^{}\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestShallow(c *C) {
	shallows := []core.Hash{
		core.NewHash("1111111111111111111111111111111111111111"),
		core.NewHash("4444444444444444444444444444444444444444"),
		core.NewHash("3333333333333333333333333333333333333333"),
		core.NewHash("2222222222222222222222222222222222222222"),
	}
	ar := &advrefs.AdvRefs{
		Shallows: shallows,
	}

	expected := pktlines(c,
		[]byte("0000000000000000000000000000000000000000 capabilities^{}\x00\n"),
		[]byte("shallow 1111111111111111111111111111111111111111\n"),
		[]byte("shallow 2222222222222222222222222222222222222222\n"),
		[]byte("shallow 3333333333333333333333333333333333333333\n"),
		[]byte("shallow 4444444444444444444444444444444444444444\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestAll(c *C) {
	hash := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	capabilities := packp.NewCapabilities()
	capabilities.Add("symref", "HEAD:/refs/heads/master")
	capabilities.Add("ofs-delta")
	capabilities.Add("multi_ack")

	references := map[string]core.Hash{
		"refs/heads/master":      core.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": core.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": core.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": core.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": core.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}

	peeled := map[string]core.Hash{
		"refs/tags/v2.7.13-tree": core.NewHash("4444444444444444444444444444444444444444"),
		"refs/tags/v2.6.12-tree": core.NewHash("5555555555555555555555555555555555555555"),
	}

	shallows := []core.Hash{
		core.NewHash("1111111111111111111111111111111111111111"),
		core.NewHash("4444444444444444444444444444444444444444"),
		core.NewHash("3333333333333333333333333333333333333333"),
		core.NewHash("2222222222222222222222222222222222222222"),
	}

	ar := &advrefs.AdvRefs{
		Head:         &hash,
		Capabilities: capabilities,
		References:   references,
		Peeled:       peeled,
		Shallows:     shallows,
	}

	expected := pktlines(c,
		[]byte("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n"),
		[]byte("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n"),
		[]byte("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n"),
		[]byte("1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n"),
		[]byte("5555555555555555555555555555555555555555 refs/tags/v2.6.12-tree^{}\n"),
		[]byte("2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n"),
		[]byte("3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n"),
		[]byte("4444444444444444444444444444444444444444 refs/tags/v2.7.13-tree^{}\n"),
		[]byte("shallow 1111111111111111111111111111111111111111\n"),
		[]byte("shallow 2222222222222222222222222222222222222222\n"),
		[]byte("shallow 3333333333333333333333333333333333333333\n"),
		[]byte("shallow 4444444444444444444444444444444444444444\n"),
		pktline.Flush,
	)

	testEncode(c, ar, expected)
}

func (s *SuiteEncoder) TestErrorTooLong(c *C) {
	references := map[string]core.Hash{
		strings.Repeat("a", pktline.MaxPayloadSize): core.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
	}
	ar := &advrefs.AdvRefs{
		References: references,
	}

	var buf bytes.Buffer
	e := advrefs.NewEncoder(&buf)
	err := e.Encode(ar)
	c.Assert(err, ErrorMatches, ".*payload is too long.*")
}
