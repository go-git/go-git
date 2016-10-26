package advrefs_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp/advrefs"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteDecodeEncode struct{}

var _ = Suite(&SuiteDecodeEncode{})

func (s *SuiteDecodeEncode) test(c *C, in []string, exp []string) {
	var err error
	var input io.Reader
	{
		var buf bytes.Buffer
		p := pktline.NewEncoder(&buf)
		err = p.EncodeString(in...)
		c.Assert(err, IsNil)
		input = &buf
	}

	var expected []byte
	{
		var buf bytes.Buffer
		p := pktline.NewEncoder(&buf)
		err = p.EncodeString(exp...)
		c.Assert(err, IsNil)

		expected = buf.Bytes()
	}

	var obtained []byte
	{
		ar := advrefs.New()
		d := advrefs.NewDecoder(input)
		err = d.Decode(ar)
		c.Assert(err, IsNil)

		var buf bytes.Buffer
		e := advrefs.NewEncoder(&buf)
		err := e.Encode(ar)
		c.Assert(err, IsNil)

		obtained = buf.Bytes()
	}

	c.Assert(obtained, DeepEquals, expected,
		Commentf("input = %v\nobtained = %q\nexpected = %q\n",
			in, string(obtained), string(expected)))
}

func (s *SuiteDecodeEncode) TestNoHead(c *C) {
	input := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00",
		pktline.FlushString,
	}

	expected := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestNoHeadSmart(c *C) {
	input := []string{
		"# service=git-upload-pack\n",
		"0000000000000000000000000000000000000000 capabilities^{}\x00",
		pktline.FlushString,
	}

	expected := []string{
		"# service=git-upload-pack\n",
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestNoHeadSmartBug(c *C) {
	input := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		pktline.FlushString,
	}

	expected := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestRefs(c *C) {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree",
		pktline.FlushString,
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestPeeled(c *C) {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		pktline.FlushString,
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestAll(c *C) {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}",
		"shallow 1111111111111111111111111111111111111111",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestAllSmart(c *C) {
	input := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	expected := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func (s *SuiteDecodeEncode) TestAllSmartBug(c *C) {
	input := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	expected := []string{
		"# service=git-upload-pack\n",
		pktline.FlushString,
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		pktline.FlushString,
	}

	s.test(c, input, expected)
}

func ExampleDecoder_Decode() {
	// Here is a raw advertised-ref message.
	raw := "" +
		"0065a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n" +
		"003fa6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n" +
		"00441111111111111111111111111111111111111111 refs/tags/v2.6.11-tree\n" +
		"00475555555555555555555555555555555555555555 refs/tags/v2.6.11-tree^{}\n" +
		"0035shallow 5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c\n" +
		"0000"

	// Use the raw message as our input.
	input := strings.NewReader(raw)

	// Create a advref.Decoder reading from our input.
	d := advrefs.NewDecoder(input)

	// Decode the input into a newly allocated AdvRefs value.
	ar := advrefs.New()
	_ = d.Decode(ar) // error check ignored for brevity

	// Do something interesting with the AdvRefs, e.g. print its contents.
	fmt.Println("head =", ar.Head)
	fmt.Println("capabilities =", ar.Capabilities.String())
	fmt.Println("...")
	fmt.Println("shallows =", ar.Shallows)
	// Output: head = a6930aaee06755d1bdcfd943fbf614e4d92bb0c7
	// capabilities = multi_ack ofs-delta symref=HEAD:/refs/heads/master
	// ...
	// shallows = [5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c]
}

func ExampleEncoder_Encode() {
	// Create an AdvRefs with the contents you want...
	ar := advrefs.New()

	// ...add a hash for the HEAD...
	head := core.NewHash("1111111111111111111111111111111111111111")
	ar.Head = &head

	// ...add some server capabilities...
	ar.Capabilities.Add("symref", "HEAD:/refs/heads/master")
	ar.Capabilities.Add("ofs-delta")
	ar.Capabilities.Add("multi_ack")

	// ...add a couple of references...
	ar.References["refs/heads/master"] = core.NewHash("2222222222222222222222222222222222222222")
	ar.References["refs/tags/v1"] = core.NewHash("3333333333333333333333333333333333333333")

	// ...including a peeled ref...
	ar.Peeled["refs/tags/v1"] = core.NewHash("4444444444444444444444444444444444444444")

	// ...and finally add a shallow
	ar.Shallows = append(ar.Shallows, core.NewHash("5555555555555555555555555555555555555555"))

	// Encode the advrefs.Contents to a bytes.Buffer.
	// You can encode into stdout too, but you will not be able
	// see the '\x00' after "HEAD".
	var buf bytes.Buffer
	e := advrefs.NewEncoder(&buf)
	_ = e.Encode(ar) // error checks ignored for brevity

	// Print the contents of the buffer as a quoted string.
	// Printing is as a non-quoted string will be prettier but you
	// will miss the '\x00' after "HEAD".
	fmt.Printf("%q", buf.String())
	// Output:
	// "00651111111111111111111111111111111111111111 HEAD\x00multi_ack ofs-delta symref=HEAD:/refs/heads/master\n003f2222222222222222222222222222222222222222 refs/heads/master\n003a3333333333333333333333333333333333333333 refs/tags/v1\n003d4444444444444444444444444444444444444444 refs/tags/v1^{}\n0035shallow 5555555555555555555555555555555555555555\n0000"
}
