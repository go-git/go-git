package packp

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

type AdvRefSuite struct {
	suite.Suite
}

func TestAdvRefSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(AdvRefSuite))
}

func (s *AdvRefSuite) TestAddReferenceSymbolic() {
	ref := plumbing.NewSymbolicReference("foo", "bar")

	a := NewAdvRefs()
	err := a.AddReference(ref)
	s.NoError(err)

	values := a.Capabilities.Get(capability.SymRef)
	s.Len(values, 1)
	s.Equal("foo:bar", values[0])
}

func (s *AdvRefSuite) TestAddReferenceHash() {
	ref := plumbing.NewHashReference("foo", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	a := NewAdvRefs()
	err := a.AddReference(ref)
	s.NoError(err)

	s.Len(a.References, 1)
	s.Equal("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c", a.References["foo"].String())
}

func (s *AdvRefSuite) TestAllReferences() {
	hash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")

	a := NewAdvRefs()
	err := a.AddReference(plumbing.NewSymbolicReference("foo", "bar"))
	s.NoError(err)
	err = a.AddReference(plumbing.NewHashReference("bar", hash))
	s.NoError(err)

	refs, err := a.AllReferences()
	s.NoError(err)

	iter, err := refs.IterReferences()
	s.NoError(err)

	var count int
	iter.ForEach(func(ref *plumbing.Reference) error {
		count++
		switch ref.Name() {
		case "bar":
			s.Equal(hash, ref.Hash())
		case "foo":
			s.Equal("bar", ref.Target().String())
		}
		return nil
	})

	s.Equal(2, count)
}

func (s *AdvRefSuite) TestAllReferencesBadSymref() {
	a := NewAdvRefs()
	err := a.Capabilities.Set(capability.SymRef, "foo")
	s.NoError(err)

	_, err = a.AllReferences()
	s.NotNil(err)
}

func (s *AdvRefSuite) TestIsEmpty() {
	a := NewAdvRefs()
	s.True(a.IsEmpty())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToMaster() {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.Head = &headHash
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref)
	s.NoError(err)

	storage, err := a.AllReferences()
	s.NoError(err)

	head, err := storage.Reference(plumbing.HEAD)
	s.NoError(err)
	s.Equal(ref.Name(), head.Target())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToOtherThanMaster() {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.Head = &headHash
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref2 := plumbing.NewHashReference("other/ref", plumbing.NewHash("0000000000000000000000000000000000000000"))

	err := a.AddReference(ref1)
	s.NoError(err)
	err = a.AddReference(ref2)
	s.NoError(err)

	storage, err := a.AllReferences()
	s.NoError(err)

	head, err := storage.Reference(plumbing.HEAD)
	s.NoError(err)
	s.Equal(ref2.Hash().String(), head.Hash().String())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoRef() {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.Head = &headHash
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref)
	s.NoError(err)

	_, err = a.AllReferences()
	s.NotNil(err)
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoMasterAlphabeticallyOrdered() {
	a := NewAdvRefs()
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.Head = &headHash
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("0000000000000000000000000000000000000000"))
	ref2 := plumbing.NewHashReference("aaaaaaaaaaaaaaa", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref3 := plumbing.NewHashReference("bbbbbbbbbbbbbbb", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	err := a.AddReference(ref1)
	s.NoError(err)
	err = a.AddReference(ref3)
	s.NoError(err)
	err = a.AddReference(ref2)
	s.NoError(err)

	storage, err := a.AllReferences()
	s.NoError(err)

	head, err := storage.Reference(plumbing.HEAD)
	s.NoError(err)
	s.Equal(ref2.Name(), head.Target())
}

type AdvRefsDecodeEncodeSuite struct {
	suite.Suite
}

func TestAdvRefsDecodeEncodeSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(AdvRefsDecodeEncodeSuite))
}

func (s *AdvRefsDecodeEncodeSuite) test(in, exp []string, isEmpty bool) {
	s.T().Helper()

	var input io.Reader
	var isSmart bool
	{
		var buf bytes.Buffer
		for _, l := range in {
			if !isSmart && strings.Contains(l, "# service=") {
				isSmart = true
			}
			if l == "" {
				s.NoError(pktline.WriteFlush(&buf))
			} else {
				_, err := pktline.WriteString(&buf, l)
				s.NoError(err)
			}
		}
		input = &buf
	}

	var expected []byte
	{
		var buf bytes.Buffer
		for _, l := range exp {
			if l == "" {
				s.Nil(pktline.WriteFlush(&buf))
			} else {
				_, err := pktline.WriteString(&buf, l)
				s.NoError(err)
			}
		}

		expected = buf.Bytes()
	}

	var obtained []byte
	{
		var smart SmartReply
		if isSmart {
			// Consume the smart service line
			s.NoError(smart.Decode(input))
		}

		ar := NewAdvRefs()
		s.NoError(ar.Decode(input))
		s.Equal(isEmpty, ar.IsEmpty())

		var buf bytes.Buffer
		if isSmart {
			s.NoError(smart.Encode(&buf))
		}

		s.NoError(ar.Encode(&buf))

		obtained = buf.Bytes()
	}

	s.Equal(string(expected), string(obtained))
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHead() {
	input := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00",
		"",
	}

	expected := []string{
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"",
	}

	s.test(input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHeadSmart() {
	input := []string{
		"# service=git-upload-pack\n",
		"",
		"0000000000000000000000000000000000000000 capabilities^{}\x00",
		"",
	}

	expected := []string{
		"# service=git-upload-pack\n",
		"",
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"",
	}

	s.test(input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestNoHeadSmartBug() {
	input := []string{
		"# service=git-upload-pack\n",
		"",
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"",
	}

	expected := []string{
		"# service=git-upload-pack\n",
		"",
		"0000000000000000000000000000000000000000 capabilities^{}\x00\n",
		"",
	}

	s.test(input, expected, true)
}

func (s *AdvRefsDecodeEncodeSuite) TestRefs() {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree",
		"",
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"",
	}

	s.test(input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestPeeled() {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"",
	}

	expected := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"",
	}

	s.test(input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAll() {
	input := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}",
		"shallow 1111111111111111111111111111111111111111",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
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
		"",
	}

	s.test(input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAllSmart() {
	input := []string{
		"# service=git-upload-pack\n",
		"",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
	}

	expected := []string{
		"# service=git-upload-pack\n",
		"",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
	}

	s.test(input, expected, false)
}

func (s *AdvRefsDecodeEncodeSuite) TestAllSmartBug() {
	input := []string{
		"# service=git-upload-pack\n",
		"",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
	}

	expected := []string{
		"# service=git-upload-pack\n",
		"",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:/refs/heads/master ofs-delta multi_ack\n",
		"a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/master\n",
		"5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v2.6.11-tree\n",
		"c39ae07f393806ccf406ef966e9a15afc43cc36a refs/tags/v2.6.11-tree^{}\n",
		"7777777777777777777777777777777777777777 refs/tags/v2.6.12-tree\n",
		"8888888888888888888888888888888888888888 refs/tags/v2.6.12-tree^{}\n",
		"shallow 1111111111111111111111111111111111111111\n",
		"shallow 2222222222222222222222222222222222222222\n",
		"",
	}

	s.test(input, expected, false)
}
