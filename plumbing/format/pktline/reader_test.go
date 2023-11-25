package pktline_test

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"

	. "gopkg.in/check.v1"
)

func init() {
	// trace.SetTarget(trace.Packet)
}

type SuiteReader struct{}

var _ = Suite(&SuiteReader{})

func (s *SuiteReader) TestInvalid(c *C) {
	for i, test := range [...]string{
		"0003",
		"fff5", "ffff",
		"gorka",
		"0", "003",
		"   5a", "5   a", "5   \n",
		"-001", "-000",
	} {
		r := strings.NewReader(test)
		sc := pktline.NewReader(r)
		_, _, err := sc.ReadPacket()
		c.Assert(err, ErrorMatches, pktline.ErrInvalidPktLen.Error()+".*",
			Commentf("i = %d, data = %q", i, test))
	}
}

func (s *SuiteReader) TestDecodeOversizePktLines(c *C) {
	for _, test := range [...]string{
		"fff1" + strings.Repeat("a", 0xfff1),
		"fff2" + strings.Repeat("a", 0xfff2),
		"fff3" + strings.Repeat("a", 0xfff3),
		"fff4" + strings.Repeat("a", 0xfff4),
	} {
		r := strings.NewReader(test)
		sc := pktline.NewReader(r)
		_, _, err := sc.ReadPacket()
		c.Assert(err, NotNil)
	}
}

func (s *SuiteReader) TestEmptyReader(c *C) {
	r := strings.NewReader("")
	sc := pktline.NewReader(r)
	l, p, err := sc.ReadPacket()
	c.Assert(l, Equals, -1)
	c.Assert(p, IsNil)
	c.Assert(err, ErrorMatches, io.EOF.Error())
}

func (s *SuiteReader) TestFlush(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.Flush()
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	l, p, err := sc.ReadPacket()
	c.Assert(l, Equals, pktline.Flush)
	c.Assert(p, IsNil)
	c.Assert(err, IsNil)
	c.Assert(len(p), Equals, 0)
}

func (s *SuiteReader) TestPktLineTooShort(c *C) {
	r := strings.NewReader("010cfoobar")

	sc := pktline.NewReader(r)

	_, _, err := sc.ReadPacket()
	c.Assert(err, ErrorMatches, "unexpected EOF")
}

func (s *SuiteReader) TestScanAndPayload(c *C) {
	for i, test := range [...]string{
		"a",
		"a\n",
		strings.Repeat("a", 100),
		strings.Repeat("a", 100) + "\n",
		strings.Repeat("\x00", 100),
		strings.Repeat("\x00", 100) + "\n",
		strings.Repeat("a", pktline.MaxPayloadSize),
		strings.Repeat("a", pktline.MaxPayloadSize-1) + "\n",
	} {
		var buf bytes.Buffer
		e := pktline.NewEncoder(&buf)
		err := e.EncodeString(test)
		c.Assert(err, IsNil,
			Commentf("input len=%x, contents=%.10q\n", len(test), test))

		sc := pktline.NewReader(&buf)
		_, p, err := sc.ReadPacket()
		c.Assert(err, IsNil)
		c.Assert(p, NotNil,
			Commentf("i = %d, payload = %q, test = %.20q...", i, p, test))

		c.Assert(p, DeepEquals, []byte(test),
			Commentf("in = %.20q out = %.20q", test, string(p)))
	}
}

func (s *SuiteReader) TestSkip(c *C) {
	for _, test := range [...]struct {
		input    []string
		n        int
		expected []byte
	}{
		{
			input: []string{
				"first",
				"second",
				"third"},
			n:        1,
			expected: []byte("second"),
		},
		{
			input: []string{
				"first",
				"second",
				"third"},
			n:        2,
			expected: []byte("third"),
		},
	} {
		var buf bytes.Buffer
		e := pktline.NewEncoder(&buf)
		err := e.EncodeString(test.input...)
		c.Assert(err, IsNil)

		sc := pktline.NewReader(&buf)
		for i := 0; i < test.n; i++ {
			_, p, err := sc.ReadPacket()
			c.Assert(p, NotNil,
				Commentf("scan error = %s", err))
		}
		_, p, err := sc.ReadPacket()
		c.Assert(p, NotNil,
			Commentf("scan error = %s", err))

		c.Assert(p, DeepEquals, test.expected,
			Commentf("\nin = %.20q\nout = %.20q\nexp = %.20q",
				test.input, p, test.expected))
	}
}

func (s *SuiteReader) TestEOF(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("first", "second")
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	for {
		_, _, err = sc.ReadPacket()
		if err == io.EOF {
			break
		}
	}
	c.Assert(err, ErrorMatches, "EOF")
}

type mockSuiteReader struct{}

func (r *mockSuiteReader) Read([]byte) (int, error) { return 0, errors.New("foo") }

func (s *SuiteReader) TestInternalReadError(c *C) {
	sc := pktline.NewReader(&mockSuiteReader{})
	_, p, err := sc.ReadPacket()
	c.Assert(p, IsNil)
	c.Assert(err, ErrorMatches, "foo")
}

// A section are several non flush-pkt lines followed by a flush-pkt, which
// how the git protocol sends long messages.
func (s *SuiteReader) TestReadSomeSections(c *C) {
	nSections := 2
	nLines := 4
	data, err := sectionsExample(nSections, nLines)
	c.Assert(err, IsNil)
	sc := pktline.NewReader(data)

	sectionCounter := 0
	lineCounter := 0
	var (
		p []byte
		e error
	)
	for {
		_, p, e = sc.ReadPacket()
		if e == io.EOF {
			break
		}
		if len(p) == 0 {
			sectionCounter++
		}
		lineCounter++
	}
	c.Assert(e, ErrorMatches, "EOF")
	c.Assert(sectionCounter, Equals, nSections)
	c.Assert(lineCounter, Equals, (1+nLines)*nSections)
}

func (s *SuiteReader) TestPeekReadPacket(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("first", "second")
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	p, err := sc.Peek(4)
	c.Assert(err, IsNil)
	c.Assert(p, DeepEquals, []byte("0009"))

	l, p, err := sc.ReadPacket()
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 9)
	c.Assert(p, DeepEquals, []byte("first"))

	p, err = sc.Peek(4)
	c.Assert(err, IsNil)
	c.Assert(p, DeepEquals, []byte("000a"))
}

func (s *SuiteReader) TestPeekMultiple(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("a")
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	b, err := sc.Peek(4)
	c.Assert(b, DeepEquals, []byte("0005"))
	c.Assert(err, IsNil)

	b, err = sc.Peek(5)
	c.Assert(b, DeepEquals, []byte("0005a"))
	c.Assert(err, IsNil)
}

func (s *SuiteReader) TestInvalidPeek(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("a")
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	_, err = sc.Peek(-1)
	c.Assert(err, ErrorMatches, pktline.ErrNegativeCount.Error())
}

func (s *SuiteReader) TestPeekPacket(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("first", "second")
	c.Assert(err, IsNil)
	sc := pktline.NewReader(&buf)
	l, p, err := sc.PeekPacket()
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 9)
	c.Assert(p, DeepEquals, []byte("first"))
	l, p, err = sc.PeekPacket()
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 9)
	c.Assert(p, DeepEquals, []byte("first"))
}

func (s *SuiteReader) TestPeekPacketReadPacket(c *C) {
	var buf bytes.Buffer
	e := pktline.NewEncoder(&buf)
	err := e.EncodeString("a")
	c.Assert(err, IsNil)

	sc := pktline.NewReader(&buf)
	l, p, err := sc.PeekPacket()
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 5)
	c.Assert(p, DeepEquals, []byte("a"))

	l, p, err = sc.ReadPacket()
	c.Assert(err, IsNil)
	c.Assert(l, Equals, 5)
	c.Assert(p, DeepEquals, []byte("a"))

	l, p, err = sc.PeekPacket()
	c.Assert(err, ErrorMatches, io.EOF.Error())
	c.Assert(l, Equals, -1)
	c.Assert(p, IsNil)
}

func (s *SuiteReader) TestPeekRead(c *C) {
	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	var buf bytes.Buffer
	e := pktline.NewWriter(&buf)
	e.WritePacketf(hash)

	sc := pktline.NewReader(&buf)
	b, err := sc.Peek(7)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte("002c6ec"))

	full, err := io.ReadAll(sc)
	c.Assert(err, IsNil)
	c.Assert(string(full), DeepEquals, "002c"+hash)
}

func (s *SuiteReader) TestPeekReadPart(c *C) {
	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	var buf bytes.Buffer
	e := pktline.NewWriter(&buf)
	e.WritePacketf(hash)

	sc := pktline.NewReader(&buf)
	b, err := sc.Peek(7)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte("002c6ec"))

	var part [8]byte
	n, err := sc.Read(part[:])
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 8)
	c.Assert(part[:], DeepEquals, []byte("002c6ecf"))
}

func (s *SuiteReader) TestReadPacketError(c *C) {
	var buf bytes.Buffer
	e := pktline.NewWriter(&buf)
	e.WriteError(io.EOF)

	sc := pktline.NewReader(&buf)
	l, p, err := sc.ReadPacket()
	c.Assert(err, NotNil)
	c.Assert(l, Equals, 12)
	c.Assert(string(p), DeepEquals, "ERR EOF\n")
}
