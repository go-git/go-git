package pktline_test

import (
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuitePktLine struct {
}

var _ = Suite(&SuitePktLine{})

func (s *SuitePktLine) TestNewIsEmpty(c *C) {
	p := pktline.New()

	b, err := ioutil.ReadAll(p)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte{})
}

func (s *SuitePktLine) TestAddFlush(c *C) {
	p := pktline.New()
	p.AddFlush()

	b, err := ioutil.ReadAll(p)
	c.Assert(err, IsNil)
	c.Assert(string(b), DeepEquals, "0000")
}

func (s *SuitePktLine) TestAdd(c *C) {
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
				[]byte("world!\n"),
				[]byte("foo"),
			},
			expected: []byte("000ahello\n000bworld!\n0007foo"),
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
		p := pktline.New()
		err := p.Add(test.input...)
		c.Assert(err, IsNil, Commentf("input %d = %v", i, test.input))

		obtained, err := ioutil.ReadAll(p)
		c.Assert(err, IsNil, Commentf("input %d = %v", i, test.input))

		c.Assert(obtained, DeepEquals, test.expected,
			Commentf("input %d = %v", i, test.input))
	}
}

func (s *SuitePktLine) TestAddErrEmptyPayload(c *C) {
	for _, input := range [...][][]byte{
		[][]byte{
			[]byte{},
		},
		[][]byte{
			[]byte(nil),
		},
		[][]byte{
			[]byte("hello world!"),
			[]byte{},
		},
		[][]byte{
			[]byte{},
			[]byte("hello world!"),
		},
	} {
		p := pktline.New()
		err := p.Add(input...)
		c.Assert(err, Equals, pktline.ErrEmptyPayload)
	}
}

func (s *SuitePktLine) TestAddErrPayloadTooLong(c *C) {
	for _, input := range [...][][]byte{
		[][]byte{
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
		},
		[][]byte{
			[]byte("hello world!"),
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
		},
		[][]byte{
			[]byte("hello world!"),
			[]byte(strings.Repeat("a", pktline.MaxPayloadSize+1)),
			[]byte("foo"),
		},
	} {
		p := pktline.New()
		err := p.Add(input...)
		c.Assert(err, Equals, pktline.ErrPayloadTooLong,
			Commentf("%v\n", input))
	}
}

func (s *SuitePktLine) TestAddString(c *C) {
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
				"world!\n",
				"foo",
			},
			expected: []byte("000ahello\n000bworld!\n0007foo"),
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
		p := pktline.New()
		err := p.AddString(test.input...)
		c.Assert(err, IsNil, Commentf("input %d = %v", i, test.input))

		obtained, err := ioutil.ReadAll(p)
		c.Assert(err, IsNil, Commentf("input %d = %v", i, test.input))

		c.Assert(obtained, DeepEquals, test.expected,
			Commentf("input %d = %v", i, test.input))
	}
}

func (s *SuitePktLine) TestAddStringErrEmptyPayload(c *C) {
	for _, input := range [...][]string{
		[]string{""},
		[]string{"hello world!", ""},
		[]string{"", "hello world!"},
	} {
		p := pktline.New()
		err := p.AddString(input...)
		c.Assert(err, Equals, pktline.ErrEmptyPayload)
	}
}

func (s *SuitePktLine) TestAddStringErrPayloadTooLong(c *C) {
	for _, input := range [...][]string{
		[]string{
			strings.Repeat("a", pktline.MaxPayloadSize+1),
		},
		[]string{
			"hello world!",
			strings.Repeat("a", pktline.MaxPayloadSize+1),
		},
		[]string{
			"hello world!",
			strings.Repeat("a", pktline.MaxPayloadSize+1),
			"foo",
		},
	} {
		p := pktline.New()
		err := p.AddString(input...)
		c.Assert(err, Equals, pktline.ErrPayloadTooLong,
			Commentf("%v\n", input))
	}
}

func ExamplePktLines() {
	// Create an empty collection of pktlines.
	p := pktline.New()

	// Add two strings as payloads ("foo\n" and "bar\n"), they will
	// end up as two consecutive pktlines.
	p.AddString("foo\n", "bar\n") // error checks removed for brevity

	// You can also add byte slices as payloads...
	p.Add([]byte("hello\n"), []byte("world!\n"))

	// Add a flush-pkt.
	p.AddFlush()

	// PktLines are Readers, so you can directly read the full sequence.
	io.Copy(os.Stdout, p)

	// Output:
	// 0008foo
	// 0008bar
	// 000ahello
	// 000bworld!
	// 0000
}
