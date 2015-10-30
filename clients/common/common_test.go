package common

import (
	"bytes"
	"encoding/base64"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/formats/pktline"
	"gopkg.in/src-d/go-git.v2/internal"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

func (s *SuiteCommon) TestNewEndpoint(c *C) {
	e, err := NewEndpoint("git@github.com:user/repository.git")
	c.Assert(err, IsNil)
	c.Assert(e, Equals, Endpoint("https://github.com/user/repository.git"))
}

func (s *SuiteCommon) TestNewEndpointWrongForgat(c *C) {
	e, err := NewEndpoint("foo")
	c.Assert(err, Not(IsNil))
	c.Assert(e, Equals, Endpoint(""))
}

func (s *SuiteCommon) TestEndpointService(c *C) {
	e, _ := NewEndpoint("git@github.com:user/repository.git")
	c.Assert(e.Service("foo"), Equals, "https://github.com/user/repository.git/info/refs?service=foo")
}

const CapabilitiesFixture = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADmulti_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag multi_ack_detailed no-done symref=HEAD:refs/heads/master agent=git/2:2.4.8~dbussink-fix-enterprise-tokens-compilation-1167-gc7006cf"

func (s *SuiteCommon) TestCapabilitiesSymbolicReference(c *C) {
	cap := parseCapabilities(CapabilitiesFixture)
	c.Assert(cap.SymbolicReference("HEAD"), Equals, "refs/heads/master")
}

const GitUploadPackInfoFixture = "MDAxZSMgc2VydmljZT1naXQtdXBsb2FkLXBhY2sKMDAwMDAxMGM2ZWNmMGVmMmMyZGZmYjc5NjAzM2U1YTAyMjE5YWY4NmVjNjU4NGU1IEhFQUQAbXVsdGlfYWNrIHRoaW4tcGFjayBzaWRlLWJhbmQgc2lkZS1iYW5kLTY0ayBvZnMtZGVsdGEgc2hhbGxvdyBuby1wcm9ncmVzcyBpbmNsdWRlLXRhZyBtdWx0aV9hY2tfZGV0YWlsZWQgbm8tZG9uZSBzeW1yZWY9SEVBRDpyZWZzL2hlYWRzL21hc3RlciBhZ2VudD1naXQvMjoyLjQuOH5kYnVzc2luay1maXgtZW50ZXJwcmlzZS10b2tlbnMtY29tcGlsYXRpb24tMTE2Ny1nYzcwMDZjZgowMDNmZThkM2ZmYWI1NTI4OTVjMTliOWZjZjdhYTI2NGQyNzdjZGUzMzg4MSByZWZzL2hlYWRzL2JyYW5jaAowMDNmNmVjZjBlZjJjMmRmZmI3OTYwMzNlNWEwMjIxOWFmODZlYzY1ODRlNSByZWZzL2hlYWRzL21hc3RlcgowMDNlYjhlNDcxZjU4YmNiY2E2M2IwN2JkYTIwZTQyODE5MDQwOWMyZGI0NyByZWZzL3B1bGwvMS9oZWFkCjAwMDA="

func (s *SuiteCommon) TestGitUploadPackInfo(c *C) {
	b, _ := base64.StdEncoding.DecodeString(GitUploadPackInfoFixture)
	info, err := NewGitUploadPackInfo(pktline.NewDecoder(bytes.NewBuffer(b)))
	c.Assert(err, IsNil)

	ref := info.Capabilities.SymbolicReference("HEAD")
	c.Assert(ref, Equals, "refs/heads/master")
	c.Assert(info.Refs[ref].String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *SuiteCommon) TestGitUploadPackInfoEmpty(c *C) {
	b := bytes.NewBuffer(nil)
	_, err := NewGitUploadPackInfo(pktline.NewDecoder(b))
	c.Assert(err, ErrorMatches, "permanent.*empty.*")
}

func (s *SuiteCommon) TestCapabilitiesDecode(c *C) {
	cap := Capabilities{}
	cap.Decode("symref=foo symref=qux thin-pack")

	c.Assert(cap, HasLen, 2)
	c.Assert(cap["symref"], DeepEquals, []string{"foo", "qux"})
	c.Assert(cap["thin-pack"], DeepEquals, []string{""})
}

func (s *SuiteCommon) TestCapabilitiesString(c *C) {
	cap := Capabilities{
		"symref": []string{"foo", "qux"},
	}

	c.Assert(cap.String(), Equals, "symref=foo symref=qux")
}

func (s *SuiteCommon) TestGitUploadPackEncode(c *C) {
	info := &GitUploadPackInfo{}
	info.Capabilities = map[string][]string{
		"symref": []string{"HEAD:refs/heads/master"},
	}

	info.Head = "refs/heads/master"
	info.Refs = map[string]internal.Hash{
		"refs/heads/master": internal.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	c.Assert(info.String(), Equals,
		"001e# service=git-upload-pack\n"+
			"0000004f6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADsymref=HEAD:refs/heads/master\n"+
			"003f6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
			"0000",
	)
}

func (s *SuiteCommon) TestGitUploadPackRequest(c *C) {
	r := &GitUploadPackRequest{
		Want: []internal.Hash{
			internal.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"),
			internal.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"),
		},
		Have: []internal.Hash{
			internal.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		},
	}

	c.Assert(r.String(), Equals,
		"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n"+
			"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"+
			"0009done\n",
	)
}
