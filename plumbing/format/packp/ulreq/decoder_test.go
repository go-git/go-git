package ulreq

import (
	"bytes"
	"io"
	"sort"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/pktline"

	. "gopkg.in/check.v1"
)

type SuiteDecoder struct{}

var _ = Suite(&SuiteDecoder{})

func (s *SuiteDecoder) TestEmpty(c *C) {
	ur := New()
	var buf bytes.Buffer
	d := NewDecoder(&buf)

	err := d.Decode(ur)
	c.Assert(err, ErrorMatches, "pkt-line 1: EOF")
}

func (s *SuiteDecoder) TestNoWant(c *C) {
	payloads := []string{
		"foobar",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*missing 'want '.*")
}

func toPktLines(c *C, payloads []string) io.Reader {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString(payloads...)
	c.Assert(err, IsNil)

	return &buf
}

func testDecoderErrorMatches(c *C, input io.Reader, pattern string) {
	ur := New()
	d := NewDecoder(input)

	err := d.Decode(ur)
	c.Assert(err, ErrorMatches, pattern)
}

func (s *SuiteDecoder) TestInvalidFirstHash(c *C) {
	payloads := []string{
		"want 6ecf0ef2c2dffb796alberto2219af86ec6584e5\n",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*invalid hash.*")
}

func (s *SuiteDecoder) TestWantOK(c *C) {
	payloads := []string{
		"want 1111111111111111111111111111111111111111",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	c.Assert(ur.Wants, DeepEquals, []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
	})
}

func testDecodeOK(c *C, payloads []string) *UlReq {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString(payloads...)
	c.Assert(err, IsNil)

	ur := New()
	d := NewDecoder(&buf)

	err = d.Decode(ur)
	c.Assert(err, IsNil)

	return ur
}

func (s *SuiteDecoder) TestWantWithCapabilities(c *C) {
	payloads := []string{
		"want 1111111111111111111111111111111111111111 ofs-delta multi_ack",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)
	c.Assert(ur.Wants, DeepEquals, []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111")})

	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)
}

func (s *SuiteDecoder) TestManyWantsNoCapabilities(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333",
		"want 4444444444444444444444444444444444444444",
		"want 1111111111111111111111111111111111111111",
		"want 2222222222222222222222222222222222222222",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expected := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}

	sort.Sort(byHash(ur.Wants))
	sort.Sort(byHash(expected))
	c.Assert(ur.Wants, DeepEquals, expected)
}

type byHash []plumbing.Hash

func (a byHash) Len() int      { return len(a) }
func (a byHash) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byHash) Less(i, j int) bool {
	ii := [20]byte(a[i])
	jj := [20]byte(a[j])
	return bytes.Compare(ii[:], jj[:]) < 0
}

func (s *SuiteDecoder) TestManyWantsBadWant(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333",
		"want 4444444444444444444444444444444444444444",
		"foo",
		"want 2222222222222222222222222222222222222222",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestManyWantsInvalidHash(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333",
		"want 4444444444444444444444444444444444444444",
		"want 1234567890abcdef",
		"want 2222222222222222222222222222222222222222",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed hash.*")
}

func (s *SuiteDecoder) TestManyWantsWithCapabilities(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 4444444444444444444444444444444444444444",
		"want 1111111111111111111111111111111111111111",
		"want 2222222222222222222222222222222222222222",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expected := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}

	sort.Sort(byHash(ur.Wants))
	sort.Sort(byHash(expected))
	c.Assert(ur.Wants, DeepEquals, expected)

	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)
}

func (s *SuiteDecoder) TestSingleShallowSingleWant(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("3333333333333333333333333333333333333333"),
	}

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	c.Assert(ur.Wants, DeepEquals, expectedWants)
	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)

	c.Assert(ur.Shallows, DeepEquals, expectedShallows)
}

func (s *SuiteDecoder) TestSingleShallowManyWants(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 4444444444444444444444444444444444444444",
		"want 1111111111111111111111111111111111111111",
		"want 2222222222222222222222222222222222222222",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

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
	c.Assert(ur.Wants, DeepEquals, expectedWants)
	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)

	c.Assert(ur.Shallows, DeepEquals, expectedShallows)
}

func (s *SuiteDecoder) TestManyShallowSingleWant(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"shallow cccccccccccccccccccccccccccccccccccccccc",
		"shallow dddddddddddddddddddddddddddddddddddddddd",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

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

	c.Assert(ur.Wants, DeepEquals, expectedWants)
	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)

	sort.Sort(byHash(ur.Shallows))
	c.Assert(ur.Shallows, DeepEquals, expectedShallows)
}

func (s *SuiteDecoder) TestManyShallowManyWants(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 4444444444444444444444444444444444444444",
		"want 1111111111111111111111111111111111111111",
		"want 2222222222222222222222222222222222222222",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"shallow cccccccccccccccccccccccccccccccccccccccc",
		"shallow dddddddddddddddddddddddddddddddddddddddd",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

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
	c.Assert(ur.Wants, DeepEquals, expectedWants)
	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)

	sort.Sort(byHash(ur.Shallows))
	c.Assert(ur.Shallows, DeepEquals, expectedShallows)
}

func (s *SuiteDecoder) TestMalformedShallow(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shalow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestMalformedShallowHash(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*malformed hash.*")
}

func (s *SuiteDecoder) TestMalformedShallowManyShallows(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"shalow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"shallow cccccccccccccccccccccccccccccccccccccccc",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestMalformedDeepenSpec(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-foo 34",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected deepen.*")
}

func (s *SuiteDecoder) TestMalformedDeepenSingleWant(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"depth 32",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestMalformedDeepenMultiWant(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 2222222222222222222222222222222222222222",
		"depth 32",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestMalformedDeepenWithSingleShallow(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow 2222222222222222222222222222222222222222",
		"depth 32",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestMalformedDeepenWithMultiShallow(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"shallow 2222222222222222222222222222222222222222",
		"shallow 5555555555555555555555555555555555555555",
		"depth 32",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}

func (s *SuiteDecoder) TestDeepenCommits(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 1234",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	c.Assert(ur.Depth, FitsTypeOf, DepthCommits(0))
	commits, ok := ur.Depth.(DepthCommits)
	c.Assert(ok, Equals, true)
	c.Assert(int(commits), Equals, 1234)
}

func (s *SuiteDecoder) TestDeepenCommitsInfiniteInplicit(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 0",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	c.Assert(ur.Depth, FitsTypeOf, DepthCommits(0))
	commits, ok := ur.Depth.(DepthCommits)
	c.Assert(ok, Equals, true)
	c.Assert(int(commits), Equals, 0)
}

func (s *SuiteDecoder) TestDeepenCommitsInfiniteExplicit(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	c.Assert(ur.Depth, FitsTypeOf, DepthCommits(0))
	commits, ok := ur.Depth.(DepthCommits)
	c.Assert(ok, Equals, true)
	c.Assert(int(commits), Equals, 0)
}

func (s *SuiteDecoder) TestMalformedDeepenCommits(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen -32",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*negative depth.*")
}

func (s *SuiteDecoder) TestDeepenCommitsEmpty(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen ",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*invalid syntax.*")
}

func (s *SuiteDecoder) TestDeepenSince(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-since 1420167845", // 2015-01-02T03:04:05+00:00
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expected := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)

	c.Assert(ur.Depth, FitsTypeOf, DepthSince(time.Now()))
	since, ok := ur.Depth.(DepthSince)
	c.Assert(ok, Equals, true)
	c.Assert(time.Time(since).Equal(expected), Equals, true,
		Commentf("obtained=%s\nexpected=%s", time.Time(since), expected))
}

func (s *SuiteDecoder) TestDeepenReference(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen-not refs/heads/master",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expected := "refs/heads/master"

	c.Assert(ur.Depth, FitsTypeOf, DepthReference(""))
	reference, ok := ur.Depth.(DepthReference)
	c.Assert(ok, Equals, true)
	c.Assert(string(reference), Equals, expected)
}

func (s *SuiteDecoder) TestAll(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"want 4444444444444444444444444444444444444444",
		"want 1111111111111111111111111111111111111111",
		"want 2222222222222222222222222222222222222222",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"shallow cccccccccccccccccccccccccccccccccccccccc",
		"shallow dddddddddddddddddddddddddddddddddddddddd",
		"deepen 1234",
		pktline.FlushString,
	}
	ur := testDecodeOK(c, payloads)

	expectedWants := []plumbing.Hash{
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("4444444444444444444444444444444444444444"),
	}
	sort.Sort(byHash(expectedWants))
	sort.Sort(byHash(ur.Wants))
	c.Assert(ur.Wants, DeepEquals, expectedWants)

	c.Assert(ur.Capabilities.Supports("ofs-delta"), Equals, true)
	c.Assert(ur.Capabilities.Supports("multi_ack"), Equals, true)

	expectedShallows := []plumbing.Hash{
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
	}
	sort.Sort(byHash(expectedShallows))
	sort.Sort(byHash(ur.Shallows))
	c.Assert(ur.Shallows, DeepEquals, expectedShallows)

	c.Assert(ur.Depth, FitsTypeOf, DepthCommits(0))
	commits, ok := ur.Depth.(DepthCommits)
	c.Assert(ok, Equals, true)
	c.Assert(int(commits), Equals, 1234)
}

func (s *SuiteDecoder) TestExtraData(c *C) {
	payloads := []string{
		"want 3333333333333333333333333333333333333333 ofs-delta multi_ack",
		"deepen 32",
		"foo",
		pktline.FlushString,
	}
	r := toPktLines(c, payloads)
	testDecoderErrorMatches(c, r, ".*unexpected payload.*")
}
