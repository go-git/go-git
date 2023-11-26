package pktline_test

import (
	"bytes"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"

	. "gopkg.in/check.v1"
)

type SuiteWriter struct{}

var _ = Suite(&SuiteWriter{})

func (s *SuiteWriter) TestFlush(c *C) {
	var buf bytes.Buffer
	err := pktline.WriteFlush(&buf)
	c.Assert(err, IsNil)

	obtained := buf.Bytes()
	c.Assert(obtained, DeepEquals, pktline.FlushPkt)
}

func (s *SuiteWriter) TestEncode(c *C) {
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
				pktline.Empty,
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
				pktline.Empty,
				[]byte("world!\n"),
				[]byte("foo"),
				pktline.Empty,
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
		comment := Commentf("input %d = %s\n", i, test.input)

		var buf bytes.Buffer

		for _, p := range test.input {
			var err error
			if bytes.Equal(p, pktline.Empty) {
				err = pktline.WriteFlush(&buf)
			} else {
				_, err = pktline.WritePacket(&buf, p)
			}
			c.Assert(err, IsNil, comment)
		}

		c.Assert(buf.String(), DeepEquals, string(test.expected), comment)
	}
}

func (s *SuiteWriter) TestEncodeErrPayloadTooLong(c *C) {
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
		comment := Commentf("input %d = %v\n", i, input)

		var buf bytes.Buffer
		_, err := pktline.WritePacket(&buf, bytes.Join(input, nil))
		c.Assert(err, Equals, pktline.ErrPayloadTooLong, comment)
	}
}

func (s *SuiteWriter) TestWritePacketStrings(c *C) {
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
		comment := Commentf("input %d = %v\n", i, test.input)

		var buf bytes.Buffer
		for _, p := range test.input {
			var err error
			if p == "" {
				err = pktline.WriteFlush(&buf)
			} else {
				_, err = pktline.WritePacketString(&buf, p)
			}
			c.Assert(err, IsNil, comment)
		}
		c.Assert(buf.String(), DeepEquals, string(test.expected), comment)
	}
}

func (s *SuiteWriter) TestWritePacketStringErrPayloadTooLong(c *C) {
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
		comment := Commentf("input %d = %v\n", i, input)

		var buf bytes.Buffer
		_, err := pktline.WritePacketString(&buf, strings.Join(input, ""))
		c.Assert(err, Equals, pktline.ErrPayloadTooLong, comment)
	}
}

func (s *SuiteWriter) TestFormatString(c *C) {
	format := " %s %d\n"
	str := "foo"
	d := 42

	var buf bytes.Buffer
	_, err := pktline.WritePacketf(&buf, format, str, d)
	c.Assert(err, IsNil)

	expected := []byte("000c foo 42\n")
	c.Assert(buf.Bytes(), DeepEquals, expected)
}
