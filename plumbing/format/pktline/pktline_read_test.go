package pktline_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/stretchr/testify/suite"

	. "gopkg.in/check.v1"
)

type SuiteReader struct {
	suite.Suite
}

func TestSuiteReader(t *testing.T) {
	suite.Run(t, new(SuiteReader))
}

func (s *SuiteReader) TestInvalid() {
	for i, test := range [...]string{
		"0003",
		"fff5", "ffff",
		"gorka",
		"0", "003",
		"   5a", "5   a", "5   \n",
		"-001", "-000",
	} {
		r := strings.NewReader(test)
		_, _, err := pktline.ReadLine(r)
		s.ErrorContains(err, pktline.ErrInvalidPktLen.Error(),
			fmt.Sprintf("i = %d, data = %q", i, test))
	}
}

func (s *SuiteReader) TestDecodeOversizePktLines() {
	for _, test := range [...]string{
		"fff1" + strings.Repeat("a", 0xfff1),
		"fff2" + strings.Repeat("a", 0xfff2),
		"fff3" + strings.Repeat("a", 0xfff3),
		"fff4" + strings.Repeat("a", 0xfff4),
	} {
		r := strings.NewReader(test)
		_, _, err := pktline.ReadLine(r)
		s.NotNil(err)
	}
}

func (s *SuiteReader) TestEmptyReader() {
	r := strings.NewReader("")
	l, p, err := pktline.ReadLine(r)
	s.Equal(-1, l)
	s.Nil(p)
	s.ErrorContains(err, io.EOF.Error())
}

func (s *SuiteReader) TestFlush() {
	var buf bytes.Buffer
	err := pktline.WriteFlush(&buf)
	s.NoError(err)

	l, p, err := pktline.ReadLine(&buf)
	s.Equal(pktline.Flush, l)
	s.Nil(p)
	s.NoError(err)
	s.Len(p, 0)
}

func (s *SuiteReader) TestPktLineTooShort() {
	r := strings.NewReader("010cfoobar")
	_, _, err := pktline.ReadLine(r)
	s.ErrorContains(err, "unexpected EOF")
}

func (s *SuiteReader) TestScanAndPayload() {
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
		_, err := pktline.Writef(&buf, "%s", test)
		s.NoError(err,
			fmt.Sprintf("input len=%x, contents=%.10q\n", len(test), test))

		_, p, err := pktline.ReadLine(&buf)
		s.NoError(err)
		s.NotNil(p,
			fmt.Sprintf("i = %d, payload = %q, test = %.20q...", i, p, test))

		s.Equal([]byte(test), p,
			fmt.Sprintf("in = %.20q out = %.20q", test, string(p)))
	}
}

func (s *SuiteReader) TestSkip() {
	for _, test := range [...]struct {
		input    []string
		n        int
		expected []byte
	}{
		{
			input: []string{
				"first",
				"second",
				"third",
			},
			n:        1,
			expected: []byte("second"),
		},
		{
			input: []string{
				"first",
				"second",
				"third",
			},
			n:        2,
			expected: []byte("third"),
		},
	} {
		var buf bytes.Buffer
		for _, in := range test.input {
			_, err := pktline.Writef(&buf, "%s", in)
			s.NoError(err)
		}

		for i := 0; i < test.n; i++ {
			_, p, err := pktline.ReadLine(&buf)
			s.NotNil(p,
				fmt.Sprintf("scan error = %s", err))
		}
		_, p, err := pktline.ReadLine(&buf)
		s.NotNil(p,
			fmt.Sprintf("scan error = %s", err))

		s.Equal(test.expected, p,
			Commentf("\nin = %.20q\nout = %.20q\nexp = %.20q",
				test.input, p, test.expected))
	}
}

func (s *SuiteReader) TestEOF() {
	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "first")
	s.NoError(err)
	_, err = pktline.Writef(&buf, "second")
	s.NoError(err)

	for {
		_, _, err = pktline.ReadLine(&buf)
		if err == io.EOF {
			break
		}
	}
	s.ErrorContains(err, "EOF")
}

type mockSuiteReader struct{}

func (r *mockSuiteReader) Read([]byte) (int, error) { return 0, errors.New("foo") }

func (s *SuiteReader) TestInternalReadError() {
	r := &mockSuiteReader{}
	_, p, err := pktline.ReadLine(r)
	s.Nil(p)
	s.ErrorContains(err, "foo")
}

// A section are several non flush-pkt lines followed by a flush-pkt, which
// how the git protocol sends long messages.
func (s *SuiteReader) TestReadSomeSections() {
	nSections := 2
	nLines := 4
	data, err := sectionsExample(nSections, nLines)
	s.NoError(err)

	sectionCounter := 0
	lineCounter := 0
	var (
		p []byte
		e error
	)
	for {
		_, p, e = pktline.ReadLine(data)
		if e == io.EOF {
			break
		}
		if len(p) == 0 {
			sectionCounter++
		}
		lineCounter++
	}
	s.ErrorContains(e, "EOF")
	s.Equal(nSections, sectionCounter)
	s.Equal((1+nLines)*nSections, lineCounter)
}

func (s *SuiteReader) TestPeekReadPacket() {
	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "first")
	s.NoError(err)
	_, err = pktline.Writef(&buf, "second")
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	p, err := sc.Peek(4)
	s.NoError(err)
	s.Equal([]byte("0009"), p)

	l, p, err := pktline.ReadLine(sc)
	s.NoError(err)
	s.Equal(9, l)
	s.Equal([]byte("first"), p)

	p, err = sc.Peek(4)
	s.NoError(err)
	s.Equal([]byte("000a"), p)
}

func (s *SuiteReader) TestPeekMultiple() {
	var buf bytes.Buffer
	_, err := pktline.WriteString(&buf, "a")
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	b, err := sc.Peek(4)
	s.Equal([]byte("0005"), b)
	s.NoError(err)

	b, err = sc.Peek(5)
	s.Equal([]byte("0005a"), b)
	s.NoError(err)
}

func (s *SuiteReader) TestInvalidPeek() {
	var buf bytes.Buffer
	_, err := pktline.WriteString(&buf, "a")
	s.NoError(err)
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	_, err = sc.Peek(-1)
	s.ErrorContains(err, bufio.ErrNegativeCount.Error())
}

func (s *SuiteReader) TestPeekPacket() {
	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "first")
	s.NoError(err)
	_, err = pktline.Writef(&buf, "second")
	s.NoError(err)
	sc := bufio.NewReader(&buf)
	l, p, err := pktline.PeekLine(sc)
	s.NoError(err)
	s.Equal(9, l)
	s.Equal([]byte("first"), p)
	l, p, err = pktline.PeekLine(sc)
	s.NoError(err)
	s.Equal(9, l)
	s.Equal([]byte("first"), p)
}

func (s *SuiteReader) TestPeekPacketReadPacket() {
	var buf bytes.Buffer
	_, err := pktline.WriteString(&buf, "a")
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	l, p, err := pktline.PeekLine(sc)
	s.NoError(err)
	s.Equal(5, l)
	s.Equal([]byte("a"), p)

	l, p, err = pktline.ReadLine(sc)
	s.NoError(err)
	s.Equal(5, l)
	s.Equal([]byte("a"), p)

	l, p, err = pktline.PeekLine(sc)
	s.ErrorContains(err, io.EOF.Error())
	s.Equal(-1, l)
	s.Nil(p)
}

func (s *SuiteReader) TestPeekRead() {
	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "%s", hash)
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	b, err := sc.Peek(7)
	s.NoError(err)
	s.Equal([]byte("002c6ec"), b)

	full, err := io.ReadAll(sc)
	s.NoError(err)
	s.Equal("002c"+hash, string(full))
}

func (s *SuiteReader) TestPeekReadPart() {
	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, "%s", hash)
	s.NoError(err)

	sc := bufio.NewReader(&buf)
	b, err := sc.Peek(7)
	s.NoError(err)
	s.Equal([]byte("002c6ec"), b)

	var part [8]byte
	n, err := sc.Read(part[:])
	s.NoError(err)
	s.Equal(8, n)
	s.Equal([]byte("002c6ecf"), part[:])
}

func (s *SuiteReader) TestReadPacketError() {
	var buf bytes.Buffer
	_, err := pktline.WriteError(&buf, io.EOF)
	s.NoError(err)

	l, p, err := pktline.ReadLine(&buf)
	s.NotNil(err)
	s.Equal(12, l)
	s.Equal("ERR EOF\n", string(p))
}

// returns nSection sections, each of them with nLines pkt-lines (not
// counting the flush-pkt:
//
// 0009 0.0\n
// 0009 0.1\n
// ...
// 0000
// and so on
func sectionsExample(nSections, nLines int) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	for section := 0; section < nSections; section++ {
		for line := 0; line < nLines; line++ {
			line := fmt.Sprintf(" %d.%d\n", section, line)
			_, err := pktline.WriteString(&buf, line)
			if err != nil {
				return nil, err
			}
		}
		if err := pktline.WriteFlush(&buf); err != nil {
			return nil, err
		}
	}

	return &buf, nil
}
