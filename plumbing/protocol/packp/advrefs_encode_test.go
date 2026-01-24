package packp

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

type AdvRefsEncodeSuite struct {
	suite.Suite
}

func TestAdvRefsEncodeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(AdvRefsEncodeSuite))
}

func testEncode(s *AdvRefsEncodeSuite, input *AdvRefs, expected []byte) {
	var buf bytes.Buffer
	s.Nil(input.Encode(&buf))
	obtained := buf.Bytes()

	comment := fmt.Sprintf("\nobtained = %s\nexpected = %s\n", string(obtained), string(expected))

	s.Equal(expected, obtained, comment)
}

func (s *AdvRefsEncodeSuite) TestZeroValue() {
	ar := &AdvRefs{}

	expected := pktlines(s.T(),
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestHead() {
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	ar := &AdvRefs{
		Head: &hash,
	}

	expected := pktlines(s.T(),
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestCapsNoHead() {
	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACK)
	capabilities.Add(capability.OFSDelta)
	capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")
	ar := &AdvRefs{
		Capabilities: capabilities,
	}

	expected := pktlines(s.T(),
		"0000000000000000000000000000000000000000 capabilities^{}\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestCapsWithHead() {
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACK)
	capabilities.Add(capability.OFSDelta)
	capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")
	ar := &AdvRefs{
		Head:         &hash,
		Capabilities: capabilities,
	}

	expected := pktlines(s.T(),
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestRefs() {
	references := map[string]plumbing.Hash{
		"refs/heads/master":      plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": plumbing.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": plumbing.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": plumbing.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}
	ar := &AdvRefs{
		References: references,
	}

	expected := pktlines(s.T(),
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\x00\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n",
		"2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n",
		"3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestPeeled() {
	references := map[string]plumbing.Hash{
		"refs/heads/master":      plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": plumbing.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": plumbing.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": plumbing.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}
	peeled := map[string]plumbing.Hash{
		"refs/tags/v2.7.13-tree": plumbing.NewHash("4444444444444444444444444444444444444444"),
		"refs/tags/v2.6.12-tree": plumbing.NewHash("5555555555555555555555555555555555555555"),
	}
	ar := &AdvRefs{
		References: references,
		Peeled:     peeled,
	}

	expected := pktlines(s.T(),
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\x00\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n",
		"5555555555555555555555555555555555555555 refs/tags/v2.6.12-tree^{}\n",
		"2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n",
		"3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n",
		"4444444444444444444444444444444444444444 refs/tags/v2.7.13-tree^{}\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestShallow() {
	shallows := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
	}
	ar := &AdvRefs{
		Shallows: shallows,
	}

	expected := pktlines(s.T(),
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"shallow 3333333333333333333333333333333333333333\n",
		"shallow 4444444444444444444444444444444444444444\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestAll() {
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACK)
	capabilities.Add(capability.OFSDelta)
	capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")

	references := map[string]plumbing.Hash{
		"refs/heads/master":      plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
		"refs/tags/v2.6.12-tree": plumbing.NewHash("1111111111111111111111111111111111111111"),
		"refs/tags/v2.7.13-tree": plumbing.NewHash("3333333333333333333333333333333333333333"),
		"refs/tags/v2.6.13-tree": plumbing.NewHash("2222222222222222222222222222222222222222"),
		"refs/tags/v2.6.11-tree": plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
	}

	peeled := map[string]plumbing.Hash{
		"refs/tags/v2.7.13-tree": plumbing.NewHash("4444444444444444444444444444444444444444"),
		"refs/tags/v2.6.12-tree": plumbing.NewHash("5555555555555555555555555555555555555555"),
	}

	shallows := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
	}

	ar := &AdvRefs{
		Head:         &hash,
		Capabilities: capabilities,
		References:   references,
		Peeled:       peeled,
		Shallows:     shallows,
	}

	expected := pktlines(s.T(),
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"1111111111111111111111111111111111111111 refs/tags/v2.6.12-tree\n",
		"5555555555555555555555555555555555555555 refs/tags/v2.6.12-tree^{}\n",
		"2222222222222222222222222222222222222222 refs/tags/v2.6.13-tree\n",
		"3333333333333333333333333333333333333333 refs/tags/v2.7.13-tree\n",
		"4444444444444444444444444444444444444444 refs/tags/v2.7.13-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"shallow 3333333333333333333333333333333333333333\n",
		"shallow 4444444444444444444444444444444444444444\n",
		"",
	)

	testEncode(s, ar, expected)
}

func (s *AdvRefsEncodeSuite) TestErrorTooLong() {
	references := map[string]plumbing.Hash{
		strings.Repeat("a", pktline.MaxPayloadSize): plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
	}
	ar := &AdvRefs{
		References: references,
	}

	var buf bytes.Buffer
	err := ar.Encode(&buf)
	s.Regexp(regexp.MustCompile(".*payload is too long.*"), err)
}
