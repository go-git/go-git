package ulreq

import (
	"bytes"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/pktline"

	. "gopkg.in/check.v1"
)

type SuiteEncoder struct{}

var _ = Suite(&SuiteEncoder{})

// returns a byte slice with the pkt-lines for the given payloads.
func pktlines(c *C, payloads ...string) []byte {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)

	err := e.EncodeString(payloads...)
	c.Assert(err, IsNil, Commentf("building pktlines for %v\n", payloads))

	return buf.Bytes()
}

func testEncode(c *C, ur *UlReq, expectedPayloads []string) {
	var buf bytes.Buffer
	e := NewEncoder(&buf)

	err := e.Encode(ur)
	c.Assert(err, IsNil)
	obtained := buf.Bytes()

	expected := pktlines(c, expectedPayloads...)

	comment := Commentf("\nobtained = %s\nexpected = %s\n", string(obtained), string(expected))

	c.Assert(obtained, DeepEquals, expected, comment)
}

func testEncodeError(c *C, ur *UlReq, expectedErrorRegEx string) {
	var buf bytes.Buffer
	e := NewEncoder(&buf)

	err := e.Encode(ur)
	c.Assert(err, ErrorMatches, expectedErrorRegEx)
}

func (s *SuiteEncoder) TestZeroValue(c *C) {
	ur := New()
	expectedErrorRegEx := ".*empty wants.*"

	testEncodeError(c, ur, expectedErrorRegEx)
}

func (s *SuiteEncoder) TestOneWant(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestOneWantWithCapabilities(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add("sysref", "HEAD:/refs/heads/master")
	ur.Capabilities.Add("multi_ack")
	ur.Capabilities.Add("thin-pack")
	ur.Capabilities.Add("side-band")
	ur.Capabilities.Add("ofs-delta")

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band sysref=HEAD:/refs/heads/master thin-pack\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestWants(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("4444444444444444444444444444444444444444"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("3333333333333333333333333333333333333333"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("2222222222222222222222222222222222222222"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("5555555555555555555555555555555555555555"))

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestWantsWithCapabilities(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("4444444444444444444444444444444444444444"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("3333333333333333333333333333333333333333"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("2222222222222222222222222222222222222222"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("5555555555555555555555555555555555555555"))

	ur.Capabilities.Add("sysref", "HEAD:/refs/heads/master")
	ur.Capabilities.Add("multi_ack")
	ur.Capabilities.Add("thin-pack")
	ur.Capabilities.Add("side-band")
	ur.Capabilities.Add("ofs-delta")

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band sysref=HEAD:/refs/heads/master thin-pack\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestShallow(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add("multi_ack")
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestManyShallows(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add("multi_ack")
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestDepthCommits(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Depth = DepthCommits(1234)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen 1234\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestDepthSinceUTC(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-since 1420167845\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestDepthSinceNonUTC(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	berlin, err := time.LoadLocation("Europe/Berlin")
	c.Assert(err, IsNil)
	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, berlin)
	// since value is 2015-01-02 03:04:05 +0100 UTC (Europe/Berlin) or
	// 2015-01-02 02:04:05 +0000 UTC, which is 1420164245 Unix seconds.
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-since 1420164245\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestDepthReference(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Depth = DepthReference("refs/heads/feature-foo")

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-not refs/heads/feature-foo\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}

func (s *SuiteEncoder) TestAll(c *C) {
	ur := New()
	ur.Wants = append(ur.Wants, plumbing.NewHash("4444444444444444444444444444444444444444"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("3333333333333333333333333333333333333333"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("2222222222222222222222222222222222222222"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("5555555555555555555555555555555555555555"))

	ur.Capabilities.Add("sysref", "HEAD:/refs/heads/master")
	ur.Capabilities.Add("multi_ack")
	ur.Capabilities.Add("thin-pack")
	ur.Capabilities.Add("side-band")
	ur.Capabilities.Add("ofs-delta")

	ur.Shallows = append(ur.Shallows, plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band sysref=HEAD:/refs/heads/master thin-pack\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"deepen-since 1420167845\n",
		pktline.FlushString,
	}

	testEncode(c, ur, expected)
}
