package packp

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UlReqDecodeSuite struct {
	suite.Suite
}

func TestUlReqDecodeSuite(t *testing.T) {
	suite.Run(t, new(UlReqDecodeSuite))
}

func (s *UlReqDecodeSuite) TestEmpty() {
	ur := NewUploadRequest()
	var buf bytes.Buffer
	d := newUlReqDecoder(&buf)

	err := d.Decode(ur)
	s.ErrorContains(err, "pkt-line 1: EOF")
}

func (s *UlReqDecodeSuite) TestNoWant() {
	payloads := []string{
		"foobar",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*missing 'want '.*")
}

func (s *UlReqDecodeSuite) testDecoderErrorMatches(input io.Reader, pattern string) {
	ur := NewUploadRequest()
	d := newUlReqDecoder(input)

	err := d.Decode(ur)
	s.Regexp(regexp.MustCompile(pattern), err)
}

func (s *UlReqDecodeSuite) TestInvalidFirstHash() {
	payloads := []string{
		"want 6ecf0ef2c2dffb796alberto2219af86ec6584e5\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*invalid hash.*")
}

func (s *UlReqDecodeSuite) TestWantOK() {
	payloads := []string{
		"want 1111111111111111111111111111111111111111\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	s.Equal([]plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
	}, ur.Wants)
}

func (s *UlReqDecodeSuite) testDecodeOK(payloads []string, expectedHaveCalls int) (*UploadRequest, []plumbing.Hash) {
	var buf bytes.Buffer
	for _, p := range payloads {
		if p == "" {
			s.NoError(pktline.WriteFlush(&buf))
		} else {
			_, err := pktline.WriteString(&buf, p)
			s.NoError(err)
		}
	}

	ur := NewUploadRequest()
	d := newUlReqDecoder(&buf)

	s.Nil(d.Decode(ur))

	haves := []plumbing.Hash{}
	nbCall := 0

	for {
		var hav UploadHaves
		s.NoError(hav.Decode(&buf))
		if len(hav.Haves) > 0 {
			nbCall += len(hav.Haves)
			haves = append(haves, hav.Haves...)
		}
		if hav.Done || len(hav.Haves) == 0 {
			break
		}
	}

	s.Equal(expectedHaveCalls, nbCall)

	return ur, haves
}

func (s *UlReqDecodeSuite) TestWantWithCapabilities() {
	payloads := []string{
		"want 1111111111111111111111111111111111111111 ofs-delta multi_ack\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)
	s.Equal([]plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
	}, ur.Wants)

	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))
}

func (s *UlReqDecodeSuite) TestManyWantsNoCapabilities() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expected := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}

	sort.Sort(byHash(ur.Wants))
	sort.Sort(byHash(expected))
	s.Equal(expected, ur.Wants)
}

type byHash []plumbing.Hash

func (a byHash) Len() int      { return len(a) }
func (a byHash) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byHash) Less(i, j int) bool {
	return a[i].Compare(a[j].Bytes()) < 0
}

func (s *UlReqDecodeSuite) TestManyWantsBadWant() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"foo\n",
		"want 2222222222222222222222222222222222222222\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestManyWantsInvalidHash() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1234567890abcdef\n",
		"want 2222222222222222222222222222222222222222\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed hash.*")
}

func (s *UlReqDecodeSuite) TestManyWantsWithCapabilities() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expected := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}

	sort.Sort(byHash(ur.Wants))
	sort.Sort(byHash(expected))
	s.Equal(expected, ur.Wants)

	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))
}

func (s *UlReqDecodeSuite) TestSingleShallowSingleWant() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("3333333333333333333333333333333333333333"),
	}

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	s.Equal(expectedWants, ur.Wants)
	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))

	s.Equal(expectedShallows, ur.Shallows)
}

func (s *UlReqDecodeSuite) TestSingleShallowManyWants() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}
	sort.Sort(byHash(expectedWants))

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	sort.Sort(byHash(ur.Wants))
	s.Equal(expectedWants, ur.Wants)
	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))

	s.Equal(expectedShallows, ur.Shallows)
}

func (s *UlReqDecodeSuite) TestManyShallowSingleWant() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("3333333333333333333333333333333333333333"),
	}

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
	}
	sort.Sort(byHash(expectedShallows))

	s.Equal(expectedWants, ur.Wants)
	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))

	sort.Sort(byHash(ur.Shallows))
	s.Equal(expectedShallows, ur.Shallows)
}

func (s *UlReqDecodeSuite) TestManyShallowManyWants() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}
	sort.Sort(byHash(expectedWants))

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
	}
	sort.Sort(byHash(expectedShallows))

	sort.Sort(byHash(ur.Wants))
	s.Equal(expectedWants, ur.Wants)
	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))

	sort.Sort(byHash(ur.Shallows))
	s.Equal(expectedShallows, ur.Shallows)
}

func (s *UlReqDecodeSuite) TestMalformedShallow() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"shalow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestMalformedShallowHash() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*malformed hash.*")
}

func (s *UlReqDecodeSuite) TestMalformedShallowManyShallows() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shalow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestMalformedDeepenSpec() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-foo 34",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected deepen.*")
}

func (s *UlReqDecodeSuite) TestMalformedDeepenSingleWant() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"depth 32",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestMalformedDeepenMultiWant() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 2222222222222222222222222222222222222222",
		"depth 32",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestMalformedDeepenWithSingleShallow() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow 2222222222222222222222222222222222222222",
		"depth 32",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestMalformedDeepenWithMultiShallow() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow 2222222222222222222222222222222222222222",
		"shallow 5555555555555555555555555555555555555555",
		"depth 32",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}

func (s *UlReqDecodeSuite) TestDeepenCommits() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 1234",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	s.IsType(DepthCommits(0), ur.Depth)
	commits, ok := ur.Depth.(DepthCommits)
	s.True(ok)
	s.Equal(1234, int(commits))
}

func (s *UlReqDecodeSuite) TestDeepenCommitsInfiniteImplicit() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 0",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	s.IsType(DepthCommits(0), ur.Depth)
	commits, ok := ur.Depth.(DepthCommits)
	s.True(ok)
	s.Equal(0, int(commits))
}

func (s *UlReqDecodeSuite) TestDeepenCommitsInfiniteExplicit() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	s.IsType(DepthCommits(0), ur.Depth)
	commits, ok := ur.Depth.(DepthCommits)
	s.True(ok)
	s.Equal(0, int(commits))
}

func (s *UlReqDecodeSuite) TestMalformedDeepenCommits() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen -32",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*negative depth.*")
}

func (s *UlReqDecodeSuite) TestDeepenCommitsEmpty() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen ",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*invalid syntax.*")
}

func (s *UlReqDecodeSuite) TestDeepenSince() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-since 1420167845", // 2015-01-02T03:04:05+00:00
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expected := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)

	s.IsType(DepthSince(time.Now()), ur.Depth)
	since, ok := ur.Depth.(DepthSince)
	s.True(ok)
	s.True(time.Time(since).Equal(expected),
		fmt.Sprintf("obtained=%s\nexpected=%s", time.Time(since), expected))
}

func (s *UlReqDecodeSuite) TestDeepenReference() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-not refs/heads/master",
		"",
	}
	ur, _ := s.testDecodeOK(payloads, 0)

	expected := "refs/heads/master"

	s.IsType(DepthReference(""), ur.Depth)
	reference, ok := ur.Depth.(DepthReference)
	s.True(ok)
	s.Equal(expected, string(reference))
}

func (s *UlReqDecodeSuite) TestAll() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"deepen 1234\n",
		"",
		"have 5555555555555555555555555555555555555555\n",
		"",
		"have 6666666666666666666666666666666666666666\n",
		"done",
	}
	ur, haves := s.testDecodeOK(payloads, 2)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}
	expectedHave := []plumbing.Hash{
		plumbing.NewHash("5555555555555555555555555555555555555555"),
		plumbing.NewHash("6666666666666666666666666666666666666666"),
	}
	sort.Sort(byHash(expectedHave))
	sort.Sort(byHash(haves))
	s.Equal(expectedHave, haves)
	s.True(ur.Capabilities.Supports(capability.OFSDelta))
	s.True(ur.Capabilities.Supports(capability.MultiACK))
	sort.Sort(byHash(expectedWants))
	sort.Sort(byHash(ur.Wants))
	s.Equal(expectedWants, ur.Wants)

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
	}
	sort.Sort(byHash(expectedShallows))
	sort.Sort(byHash(ur.Shallows))
	s.Equal(expectedShallows, ur.Shallows)

	s.IsType(DepthCommits(0), ur.Depth)
	commits, ok := ur.Depth.(DepthCommits)
	s.True(ok)
	s.Equal(1234, int(commits))
}

func (s *UlReqDecodeSuite) TestExtraData() {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 32",
		"foo",
		"",
	}
	r := toPktLines(s.T(), payloads)
	s.testDecoderErrorMatches(r, ".*unexpected payload.*")
}
