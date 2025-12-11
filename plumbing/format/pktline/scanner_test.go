package pktline_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

type SuiteScanner struct {
	suite.Suite
}

func TestSuiteScanner(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteScanner))
}

func (s *SuiteScanner) TestInvalid() {
	for _, test := range [...]string{
		"0003",
		"0001asdfsadf", "0004foo",
		"fff5", "ffff",
		"gorka",
		"0", "003",
		"   5a", "5   a", "5   \n",
		"-001", "-000",
	} {
		r := strings.NewReader(test)
		sc := pktline.NewScanner(r)
		for sc.Scan() {
		}
		s.ErrorContains(sc.Err(), pktline.ErrInvalidPktLen.Error(),
			fmt.Sprintf("data = %q", test))
	}
}

func (s *SuiteScanner) TestDecodeOversizePktLines() {
	for _, test := range [...]string{
		"fff1" + strings.Repeat("a", 0xfff1),
		"fff2" + strings.Repeat("a", 0xfff2),
		"fff3" + strings.Repeat("a", 0xfff3),
		"fff4" + strings.Repeat("a", 0xfff4),
	} {
		r := strings.NewReader(test)
		sc := pktline.NewScanner(r)
		_ = sc.Scan()
		s.ErrorIs(sc.Err(), pktline.ErrInvalidPktLen)
	}
}

func (s *SuiteScanner) TestValidPktSizes() {
	for _, test := range [...]string{
		"01fe" + strings.Repeat("a", 0x01fe-4),
		"01FE" + strings.Repeat("a", 0x01fe-4),
		"00b5" + strings.Repeat("a", 0x00b5-4),
		"00B5" + strings.Repeat("a", 0x00b5-4),
	} {
		r := strings.NewReader(test)
		sc := pktline.NewScanner(r)
		hasPayload := sc.Scan()
		obtained := fmt.Sprintf("%04x%s", sc.Len(), sc.Bytes())

		s.True(hasPayload)
		s.NoError(sc.Err())
		s.Equal(strings.ToLower(test), obtained)
	}
}

func (s *SuiteScanner) TestEmptyReader() {
	r := strings.NewReader("")
	sc := pktline.NewScanner(r)
	hasPayload := sc.Scan()
	s.False(hasPayload)
	s.NoError(sc.Err())
}

func (s *SuiteScanner) TestFlush() {
	var buf bytes.Buffer
	err := pktline.WriteFlush(&buf)
	s.NoError(err)

	sc := pktline.NewScanner(&buf)
	s.True(sc.Scan())

	payload := sc.Bytes()
	s.Len(payload, 0)
}

func (s *SuiteScanner) TestPktLineTooShort() {
	r := strings.NewReader("010cfoobar")

	sc := pktline.NewScanner(r)

	s.False(sc.Scan())
	s.ErrorContains(sc.Err(), "unexpected EOF")
}

func (s *SuiteScanner) TestScanAndPayload() {
	for _, test := range [...]string{
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

		sc := pktline.NewScanner(&buf)
		s.True(sc.Scan(),
			fmt.Sprintf("test = %.20q...", test))

		obtained := sc.Bytes()
		s.Equal([]byte(test), obtained,
			fmt.Sprintf("in = %.20q out = %.20q", test, string(obtained)))
	}
}

func (s *SuiteScanner) TestSkip() {
	for _, test := range [...]struct {
		input    []string
		expected []byte
		n        int
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

		sc := pktline.NewScanner(&buf)
		for i := 0; i < test.n; i++ {
			s.True(sc.Scan(),
				fmt.Sprintf("scan error = %s", sc.Err()))
		}
		s.True(sc.Scan(),
			fmt.Sprintf("scan error = %s", sc.Err()))

		obtained := sc.Bytes()
		s.Equal(test.expected, obtained,
			fmt.Sprintf("\nin = %.20q\nout = %.20q\nexp = %.20q",
				test.input, obtained, test.expected))
	}
}

func (s *SuiteScanner) TestEOF() {
	var buf bytes.Buffer
	for _, in := range []string{"first", "second"} {
		_, err := pktline.Writef(&buf, "%s", in)
		s.NoError(err)
	}

	sc := pktline.NewScanner(&buf)
	for sc.Scan() {
	}
	s.NoError(sc.Err())
}

type mockReader struct{}

func (r *mockReader) Read([]byte) (int, error) { return 0, errors.New("foo") }

func (s *SuiteScanner) TestInternalReadError() {
	sc := pktline.NewScanner(&mockReader{})
	s.False(sc.Scan())
	s.ErrorContains(sc.Err(), "foo")
}

// A section are several non flush-pkt lines followed by a flush-pkt, which
// how the git protocol sends long messages.
func (s *SuiteScanner) TestReadSomeSections() {
	nSections := 2
	nLines := 4
	data, err := sectionsExample(nSections, nLines)
	s.NoError(err)
	sc := pktline.NewScanner(data)

	sectionCounter := 0
	lineCounter := 0
	for sc.Scan() {
		if sc.Len() == pktline.Flush {
			sectionCounter++
		}
		lineCounter++
	}
	s.NoError(sc.Err())
	s.Equal(nSections, sectionCounter)
	s.Equal((1+nLines)*nSections, lineCounter)
}
