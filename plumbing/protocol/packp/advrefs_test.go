package packp

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

type AdvRefSuite struct{}

var _ = Suite(&AdvRefSuite{})

func (s *AdvRefSuite) TestAddReferenceSymbolic(c *C) {
	ref := plumbing.NewSymbolicReference("foo", "bar")

	a := NewAdvRefs()
	err := a.AddReference(ref)
	c.Assert(err, IsNil)

	values := a.Capabilities.Get(capability.SymRef)
	c.Assert(values, HasLen, 1)
	c.Assert(values[0], Equals, "foo:bar")
}

func (s *AdvRefSuite) TestAddReferenceHash(c *C) {
	ref := plumbing.NewHashReference("foo", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	a := NewAdvRefs()
	err := a.AddReference(ref)
	c.Assert(err, IsNil)

	c.Assert(a.References, HasLen, 1)
	c.Assert(a.References["foo"].String(), Equals, "5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
}

func (s *AdvRefSuite) TestAllReferences(c *C) {
	hash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")

	a := NewAdvRefs()
	err := a.AddReference(plumbing.NewSymbolicReference("foo", "bar"))
	c.Assert(err, IsNil)
	err = a.AddReference(plumbing.NewHashReference("bar", hash))
	c.Assert(err, IsNil)

	refs, err := a.AllReferences()
	c.Assert(err, IsNil)

	iter, err := refs.IterReferences()
	c.Assert(err, IsNil)

	var count int
	iter.ForEach(func(ref *plumbing.Reference) error {
		count++
		switch ref.Name() {
		case "bar":
			c.Assert(ref.Hash(), Equals, hash)
		case "foo":
			c.Assert(ref.Target().String(), Equals, "bar")
		}
		return nil
	})

	c.Assert(count, Equals, 2)
}

func (s *AdvRefSuite) TestAllReferencesBadSymref(c *C) {
	a := NewAdvRefs()
	err := a.Capabilities.Set(capability.SymRef, "foo")
	c.Assert(err, IsNil)

	_, err = a.AllReferences()
	c.Assert(err, NotNil)
}

func (s *AdvRefSuite) TestIsEmpty(c *C) {
	a := NewAdvRefs()
	c.Assert(a.IsEmpty(), Equals, true)
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToMaster(c *C) {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.Head = &headHash
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref)
	c.Assert(err, IsNil)

	storage, err := a.AllReferences()
	c.Assert(err, IsNil)

	head, err := storage.Reference(plumbing.HEAD)
	c.Assert(err, IsNil)
	c.Assert(head.Target(), Equals, ref.Name())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToOtherThanMaster(c *C) {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.Head = &headHash
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref2 := plumbing.NewHashReference("other/ref", plumbing.NewHash("0000000000000000000000000000000000000000"))

	err := a.AddReference(ref1)
	c.Assert(err, IsNil)
	err = a.AddReference(ref2)
	c.Assert(err, IsNil)

	storage, err := a.AllReferences()
	c.Assert(err, IsNil)

	head, err := storage.Reference(plumbing.HEAD)
	c.Assert(err, IsNil)
	c.Assert(head.Hash(), Equals, ref2.Hash())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoRef(c *C) {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.Head = &headHash
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref)
	c.Assert(err, IsNil)

	_, err = a.AllReferences()
	c.Assert(err, NotNil)
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoMasterAlphabeticallyOrdered(c *C) {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.Head = &headHash
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("0000000000000000000000000000000000000000"))
	ref2 := plumbing.NewHashReference("aaaaaaaaaaaaaaa", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref3 := plumbing.NewHashReference("bbbbbbbbbbbbbbb", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref1)
	c.Assert(err, IsNil)
	err = a.AddReference(ref3)
	c.Assert(err, IsNil)
	err = a.AddReference(ref2)
	c.Assert(err, IsNil)

	storage, err := a.AllReferences()
	c.Assert(err, IsNil)

	head, err := storage.Reference(plumbing.HEAD)
	c.Assert(err, IsNil)
	c.Assert(head.Target(), Equals, ref2.Name())
}

type AdvRefsDecodeEncodeSuite struct{}

var _ = Suite(&AdvRefsDecodeEncodeSuite{})

func (s *AdvRefsDecodeEncodeSuite) test(c *C, in []string, exp []string, isEmpty bool) {
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
		ar := NewAdvRefs()
		c.Assert(ar.Decode(input), IsNil)
		c.Assert(ar.IsEmpty(), Equals, isEmpty)

		var buf bytes.Buffer
		c.Assert(ar.Encode(&buf), IsNil)

		obtained = buf.Bytes()
	}

	c.Assert(string(obtained), DeepEquals, string(expected))
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHead(c *C) {
	input := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00",
		pktline.FlushString,
	}

	expected := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		pktline.FlushString,
	}

	s.test(c, input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHeadSmart(c *C) {
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

	s.test(c, input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHeadSmartBug(c *C) {
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

	s.test(c, input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestRefs(c *C) {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree",
		pktline.FlushString,
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		pktline.FlushString,
	}

	s.test(c, input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestPeeled(c *C) {
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
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		pktline.FlushString,
	}

	s.test(c, input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAll(c *C) {
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

	s.test(c, input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAllSmart(c *C) {
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

	s.test(c, input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAllSmartBug(c *C) {
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

	s.test(c, input, expected, false)
}
