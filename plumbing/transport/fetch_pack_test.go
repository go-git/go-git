package transport

import (
	"bytes"
	"encoding/base64"

	"gopkg.in/src-d/go-git.v4/plumbing"

	. "gopkg.in/check.v1"
)

type UploadPackSuite struct{}

var _ = Suite(&UploadPackSuite{})

const UploadPackInfoFixture = "MDAxZSMgc2VydmljZT1naXQtdXBsb2FkLXBhY2sKMDAwMDAxMGM2ZWNmMGVmMmMyZGZmYjc5NjAzM2U1YTAyMjE5YWY4NmVjNjU4NGU1IEhFQUQAbXVsdGlfYWNrIHRoaW4tcGFjayBzaWRlLWJhbmQgc2lkZS1iYW5kLTY0ayBvZnMtZGVsdGEgc2hhbGxvdyBuby1wcm9ncmVzcyBpbmNsdWRlLXRhZyBtdWx0aV9hY2tfZGV0YWlsZWQgbm8tZG9uZSBzeW1yZWY9SEVBRDpyZWZzL2hlYWRzL21hc3RlciBhZ2VudD1naXQvMjoyLjQuOH5kYnVzc2luay1maXgtZW50ZXJwcmlzZS10b2tlbnMtY29tcGlsYXRpb24tMTE2Ny1nYzcwMDZjZgowMDNmZThkM2ZmYWI1NTI4OTVjMTliOWZjZjdhYTI2NGQyNzdjZGUzMzg4MSByZWZzL2hlYWRzL2JyYW5jaAowMDNmNmVjZjBlZjJjMmRmZmI3OTYwMzNlNWEwMjIxOWFmODZlYzY1ODRlNSByZWZzL2hlYWRzL21hc3RlcgowMDNlYjhlNDcxZjU4YmNiY2E2M2IwN2JkYTIwZTQyODE5MDQwOWMyZGI0NyByZWZzL3B1bGwvMS9oZWFkCjAwMDA="

func (s *UploadPackSuite) TestUploadPackInfo(c *C) {
	b, _ := base64.StdEncoding.DecodeString(UploadPackInfoFixture)

	i := NewUploadPackInfo()
	err := i.Decode(bytes.NewBuffer(b))
	c.Assert(err, IsNil)

	name := i.Capabilities.SymbolicReference("HEAD")
	c.Assert(name, Equals, "refs/heads/master")
	c.Assert(i.Refs, HasLen, 4)

	ref := i.Refs[plumbing.ReferenceName(name)]
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	ref = i.Refs[plumbing.HEAD]
	c.Assert(ref, NotNil)
	c.Assert(ref.Target(), Equals, plumbing.ReferenceName(name))
}

const UploadPackInfoNoHEADFixture = "MDAxZSMgc2VydmljZT1naXQtdXBsb2FkLXBhY2sKMDAwMDAwYmNkN2UxZmVlMjYxMjM0YmIzYTQzYzA5NmY1NTg3NDhhNTY5ZDc5ZWZmIHJlZnMvaGVhZHMvdjQAbXVsdGlfYWNrIHRoaW4tcGFjayBzaWRlLWJhbmQgc2lkZS1iYW5kLTY0ayBvZnMtZGVsdGEgc2hhbGxvdyBuby1wcm9ncmVzcyBpbmNsdWRlLXRhZyBtdWx0aV9hY2tfZGV0YWlsZWQgbm8tZG9uZSBhZ2VudD1naXQvMS45LjEKMDAwMA=="

func (s *UploadPackSuite) TestUploadPackInfoNoHEAD(c *C) {
	b, _ := base64.StdEncoding.DecodeString(UploadPackInfoNoHEADFixture)

	i := NewUploadPackInfo()
	err := i.Decode(bytes.NewBuffer(b))
	c.Assert(err, IsNil)

	name := i.Capabilities.SymbolicReference("HEAD")
	c.Assert(name, Equals, "")
	c.Assert(i.Refs, HasLen, 1)

	ref := i.Refs["refs/heads/v4"]
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "d7e1fee261234bb3a43c096f558748a569d79eff")
}

func (s *UploadPackSuite) TestUploadPackInfoEmpty(c *C) {
	b := bytes.NewBuffer(nil)

	i := NewUploadPackInfo()
	err := i.Decode(b)
	c.Assert(err, ErrorMatches, "permanent.*empty.*")
}

func (s *UploadPackSuite) TestUploadPackEncode(c *C) {
	info := NewUploadPackInfo()
	info.Capabilities.Add("symref", "HEAD:refs/heads/master")

	ref := plumbing.ReferenceName("refs/heads/master")
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	info.Refs = map[plumbing.ReferenceName]*plumbing.Reference{
		plumbing.HEAD: plumbing.NewSymbolicReference(plumbing.HEAD, ref),
		ref:           plumbing.NewHashReference(ref, hash),
	}

	c.Assert(info.Head(), NotNil)
	c.Assert(info.String(), Equals,
		"001e# service=git-upload-pack\n"+
			"000000506ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00symref=HEAD:refs/heads/master\n"+
			"003f6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
			"0000",
	)
}

func (s *UploadPackSuite) TestUploadPackRequest(c *C) {
	r := &UploadPackRequest{}
	r.Want(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Want(plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Have(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	c.Assert(r.String(), Equals,
		"0032want d82f291cde9987322c8a0c81a325e1ba6159684c\n"+
			"0032want 2b41ef280fdb67a9b250678686a0c3e03b0a9989\n"+
			"0032have 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"+
			"0009done\n",
	)
}
