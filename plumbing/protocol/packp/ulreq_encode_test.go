package packp

import (
	"bytes"
	"fmt"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type UlReqEncodeSuite struct {
	suite.Suite
}

func TestUlReqEncodeSuite(t *testing.T) {
	suite.Run(t, new(UlReqEncodeSuite))
}

func testUlReqEncode(s *UlReqEncodeSuite, ur *UploadRequest, expectedPayloads []string) {
	var buf bytes.Buffer
	e := newUlReqEncoder(&buf)

	err := e.Encode(ur)
	s.NoError(err)
	obtained := buf.Bytes()

	expected := pktlines(s.T(), expectedPayloads...)

	comment := fmt.Sprintf("\nobtained = %s\nexpected = %s\n", string(obtained), string(expected))

	s.Equal(expected, obtained, comment)
}

func testUlReqEncodeError(s *UlReqEncodeSuite, ur *UploadRequest, expectedErrorRegEx string) {
	var buf bytes.Buffer
	e := newUlReqEncoder(&buf)

	err := e.Encode(ur)
	s.Regexp(regexp.MustCompile(expectedErrorRegEx), err)
}

func (s *UlReqEncodeSuite) TestZeroValue() {
	ur := NewUploadRequest()
	expectedErrorRegEx := ".*empty wants.*"

	testUlReqEncodeError(s, ur, expectedErrorRegEx)
}

func (s *UlReqEncodeSuite) TestOneWant() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestOneWantWithCapabilities() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add(capability.MultiACK)
	ur.Capabilities.Add(capability.OFSDelta)
	ur.Capabilities.Add(capability.Sideband)
	ur.Capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")
	ur.Capabilities.Add(capability.ThinPack)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band symref=HEAD:/refs/heads/master thin-pack\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestWants() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants,
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("5555555555555555555555555555555555555555"),
	)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestWantsDuplicates() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants,
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
	)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestWantsWithCapabilities() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants,
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("5555555555555555555555555555555555555555"),
	)

	ur.Capabilities.Add(capability.MultiACK)
	ur.Capabilities.Add(capability.OFSDelta)
	ur.Capabilities.Add(capability.Sideband)
	ur.Capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")
	ur.Capabilities.Add(capability.ThinPack)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band symref=HEAD:/refs/heads/master thin-pack\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestShallow() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add(capability.MultiACK)
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestManyShallows() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add(capability.MultiACK)
	ur.Shallows = append(ur.Shallows,
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestShallowsDuplicate() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Capabilities.Add(capability.MultiACK)
	ur.Shallows = append(ur.Shallows,
		plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"),
		plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestDepthCommits() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Depth = DepthCommits(1234)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen 1234\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestDepthSinceUTC() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-since 1420167845\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestDepthSinceNonUTC() {
	if runtime.GOOS == "js" {
		s.T().Skip("time.LoadLocation not supported in wasm")
	}

	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	berlin, err := time.LoadLocation("Europe/Berlin")
	s.NoError(err)
	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, berlin)
	// since value is 2015-01-02 03:04:05 +0100 UTC (Europe/Berlin) or
	// 2015-01-02 02:04:05 +0000 UTC, which is 1420164245 Unix seconds.
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-since 1420164245\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestDepthReference() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Depth = DepthReference("refs/heads/feature-foo")

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"deepen-not refs/heads/feature-foo\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestFilter() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Filter = FilterTreeDepth(0)

	expected := []string{
		"want 1111111111111111111111111111111111111111\n",
		"filter tree:0\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}

func (s *UlReqEncodeSuite) TestAll() {
	ur := NewUploadRequest()
	ur.Wants = append(ur.Wants,
		plumbing.NewHash("4444444444444444444444444444444444444444"),
		plumbing.NewHash("1111111111111111111111111111111111111111"),
		plumbing.NewHash("3333333333333333333333333333333333333333"),
		plumbing.NewHash("2222222222222222222222222222222222222222"),
		plumbing.NewHash("5555555555555555555555555555555555555555"),
	)

	ur.Capabilities.Add(capability.MultiACK)
	ur.Capabilities.Add(capability.OFSDelta)
	ur.Capabilities.Add(capability.Sideband)
	ur.Capabilities.Add(capability.SymRef, "HEAD:/refs/heads/master")
	ur.Capabilities.Add(capability.ThinPack)

	ur.Shallows = append(ur.Shallows, plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddd"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)
	ur.Depth = DepthSince(since)

	expected := []string{
		"want 1111111111111111111111111111111111111111 multi_ack ofs-delta side-band symref=HEAD:/refs/heads/master thin-pack\n",
		"want 2222222222222222222222222222222222222222\n",
		"want 3333333333333333333333333333333333333333\n",
		"want 4444444444444444444444444444444444444444\n",
		"want 5555555555555555555555555555555555555555\n",
		"shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
		"shallow cccccccccccccccccccccccccccccccccccccccc\n",
		"shallow dddddddddddddddddddddddddddddddddddddddd\n",
		"deepen-since 1420167845\n",
		"",
	}

	testUlReqEncode(s, ur, expected)
}
