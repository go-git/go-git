package pktline_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/stretchr/testify/suite"
)

type SuiteWriter struct {
	suite.Suite
}

func TestSuiteWriter(t *testing.T) {
	suite.Run(t, new(SuiteWriter))
}

func (s *SuiteWriter) TestFlush() {
	var buf bytes.Buffer
	err := pktline.WriteFlush(&buf)
	s.NoError(err)

	obtained := buf.Bytes()
	s.Equal([]byte("0000"), obtained)
}

func (s *SuiteWriter) TestEncode() {
	for i, test := range [...]struct {
		input    [][]byte
		expected []byte
	}{
		{
			input: [][]byte{
				[]byte("hello\n"),
			},
			expected: []byte("000ahello\n"),
		}, {
			input: [][]byte{
				[]byte("hello\n"),
				{},
			},
			expected: []byte("000ahello\n0000"),
		}, {
			input: [][]byte{
				[]byte("hello\n"),
				[]byte("world!\n"),
				[]byte("foo"),
			},
			expected: []byte("000ahello\n000bworld!\n0007foo"),
		}, {
			input: [][]byte{
				[]byte("hello\n"),
				{},
				[]byte("world!\n"),
				[]byte("foo"),
				{},
			},
			expected: []byte("000ahello\n0000000bworld!\n0007foo0000"),
		}, {
			input: [][]byte{
				[]byte(strings.Repeat("a", pktline.MaxPayloadSize)),
			},
			expected: []byte(
				"fff0" + strings.Repeat("a", pktline.MaxPayloadSize)),
		}, {
			input: [][]byte{
				[]byte(strings.Repeat("a", pktline.MaxPayloadSize)),
				[]byte(strings.Repeat("b", pktline.MaxPayloadSize)),
			},
			expected: []byte(
				"fff0" + strings.Repeat("a", pktline.MaxPayloadSize) +
					"fff0" + strings.Repeat("b", pktline.MaxPayloadSize)),
		},
	} {
		comment := fmt.Sprintf("input %d = %s\n", i, test.input)

		var buf bytes.Buffer

		for _, p := range test.input {
			var err error
			if len(p) == 0 {
				err = pktline.WriteFlush(&buf)
			} else {
				_, err = pktline.Write(&buf, p)
			}
			s.NoError(err, comment)
		}

		s.Equal(string(test.expected), comment, buf.String())
	}
}

func (s *SuiteWriter) TestEncodeErrPayloadTooLong() {
	for i, input := range [...][][]byte{
		{
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
		},
		{
			[]byte("hello world!"),
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
		},
		{
			[]byte("hello world!"),
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
			[]byte("foo"),
		},
	} {
		comment := fmt.Sprintf("input %d = %v\n", i, input)

		var buf bytes.Buffer
		_, err := pktline.Write(&buf, bytes.Join(input, nil))
		s.Equal(pktline.ErrPayloadTooLong, err, comment)
	}
}

func (s *SuiteWriter) TestWritePacketStrings() {
	for i, test := range [...]struct {
		input    []string
		expected []byte
	}{
		{
			input: []string{
				"hello\n",
			},
			expected: []byte("000ahello\n"),
		}, {
			input: []string{
				"hello\n",
				"",
			},
			expected: []byte("000ahello\n0000"),
		}, {
			input: []string{
				"hello\n",
				"world!\n",
				"foo",
			},
			expected: []byte("000ahello\n000bworld!\n0007foo"),
		}, {
			input: []string{
				"hello\n",
				"",
				"world!\n",
				"foo",
				"",
			},
			expected: []byte("000ahello\n0000000bworld!\n0007foo0000"),
		}, {
			input: []string{
				strings.Repeat("a", pktline.MaxPayloadSize),
			},
			expected: []byte(
				"fff0" + strings.Repeat("a", pktline.MaxPayloadSize)),
		}, {
			input: []string{
				strings.Repeat("a", pktline.MaxPayloadSize),
				strings.Repeat("b", pktline.MaxPayloadSize),
			},
			expected: []byte(
				"fff0" + strings.Repeat("a", pktline.MaxPayloadSize) +
					"fff0" + strings.Repeat("b", pktline.MaxPayloadSize)),
		},
	} {
		comment := fmt.Sprintf("input %d = %v\n", i, test.input)

		var buf bytes.Buffer
		for _, p := range test.input {
			var err error
			if p == "" {
				err = pktline.WriteFlush(&buf)
			} else {
				_, err = pktline.WriteString(&buf, p)
			}
			s.NoError(err, comment)
		}
		s.Equal(string(test.expected), comment, buf.String())
	}
}

func (s *SuiteWriter) TestWritePacketStringErrPayloadTooLong() {
	for i, input := range [...][]string{
		{
			strings.Repeat("a", pktline.MaxPayloadSize+1),
		},
		{
			"hello world!",
			strings.Repeat("a", pktline.MaxPayloadSize+1),
		},
		{
			"hello world!",
			strings.Repeat("a", pktline.MaxPayloadSize+1),
			"foo",
		},
	} {
		comment := fmt.Sprintf("input %d = %v\n", i, input)

		var buf bytes.Buffer
		_, err := pktline.WriteString(&buf, strings.Join(input, ""))
		s.Equal(pktline.ErrPayloadTooLong, err, comment)
	}
}

func (s *SuiteWriter) TestFormatString() {
	format := " %s %d\n"
	str := "foo"
	d := 42

	var buf bytes.Buffer
	_, err := pktline.Writef(&buf, format, str, d)
	s.NoError(err)

	expected := []byte("000c foo 42\n")
	s.Equal(expected, buf.Bytes())
}
