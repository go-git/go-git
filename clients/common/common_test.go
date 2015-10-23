package common

import (
	"bytes"
	"encoding/base64"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/formats/pktline"
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
	c.Assert(info.Refs[ref].Id, Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	c.Assert(info.Refs[ref].Name, Equals, "refs/heads/master")
}

func (s *SuiteCommon) TestGitUploadPackRequest(c *C) {
	r := &GitUploadPackRequest{
		Want: []string{"foo", "qux"},
		Have: []string{"bar"},
	}

	c.Assert(r.String(), Equals, "000dwant foo\n000dwant qux\n000dhave bar\n00000009done\n")
}
