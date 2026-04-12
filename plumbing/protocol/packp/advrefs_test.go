package packp

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

type AdvRefSuite struct {
	suite.Suite
}

func TestAdvRefSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(AdvRefSuite))
}

func refByName(refs []*plumbing.Reference, name plumbing.ReferenceName) *plumbing.Reference {
	for _, ref := range refs {
		if ref.Name() == name {
			return ref
		}
	}
	return nil
}

func (s *AdvRefSuite) TestResolvedReferencesSymref() {
	hash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")

	a := &AdvRefs{}
	a.Capabilities.Add(capability.SymRef, "foo:bar")
	a.References = append(a.References, plumbing.NewHashReference("bar", hash))

	refs, err := a.ResolvedReferences()
	s.NoError(err)

	s.Len(refs, 2)
	for _, ref := range refs {
		switch ref.Name() {
		case "bar":
			s.Equal(hash, ref.Hash())
		case "foo":
			s.Equal("bar", ref.Target().String())
		}
	}
}

func (s *AdvRefSuite) TestResolvedReferencesBadSymref() {
	a := &AdvRefs{}
	a.Capabilities.Set(capability.SymRef, "foo")

	_, err := a.ResolvedReferences()
	s.NotNil(err)
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToMaster() {
	a := &AdvRefs{}
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.References = append(a.References, plumbing.NewHashReference(plumbing.HEAD, headHash))
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	a.References = append(a.References, ref)

	refs, err := a.ResolvedReferences()
	s.NoError(err)

	head := refByName(refs, plumbing.HEAD)
	s.NotNil(head)
	s.Equal(ref.Name(), head.Target())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToOtherThanMaster() {
	a := &AdvRefs{}
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.References = append(a.References, plumbing.NewHashReference(plumbing.HEAD, headHash))
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref2 := plumbing.NewHashReference("other/ref", plumbing.NewHash("0000000000000000000000000000000000000000"))

	a.References = append(a.References, ref1)
	a.References = append(a.References, ref2)

	refs, err := a.ResolvedReferences()
	s.NoError(err)

	head := refByName(refs, plumbing.HEAD)
	s.NotNil(head)
	s.Equal(ref2.Hash().String(), head.Hash().String())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoRef() {
	a := &AdvRefs{}
	headHash := plumbing.NewHash("0000000000000000000000000000000000000000")
	a.References = append(a.References, plumbing.NewHashReference(plumbing.HEAD, headHash))
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	a.References = append(a.References, ref)

	refs, err := a.ResolvedReferences()
	s.NoError(err)

	head := refByName(refs, plumbing.HEAD)
	s.NotNil(head)
	s.Equal(plumbing.HashReference, head.Type())
}

func (s *AdvRefSuite) TestNoSymRefCapabilityHeadToNoMasterAlphabeticallyOrdered() {
	a := &AdvRefs{}
	headHash := plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")
	a.References = append(a.References, plumbing.NewHashReference(plumbing.HEAD, headHash))
	ref1 := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash("0000000000000000000000000000000000000000"))
	ref2 := plumbing.NewHashReference("aaaaaaaaaaaaaaa", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))
	ref3 := plumbing.NewHashReference("bbbbbbbbbbbbbbb", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"))

	a.References = append(a.References, ref1)
	a.References = append(a.References, ref3)
	a.References = append(a.References, ref2)

	refs, err := a.ResolvedReferences()
	s.NoError(err)

	head := refByName(refs, plumbing.HEAD)
	s.NotNil(head)
	s.Equal(plumbing.SymbolicReference, head.Type())
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

		ar := &AdvRefs{}
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
