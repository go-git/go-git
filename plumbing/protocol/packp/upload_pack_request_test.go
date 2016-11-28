package packp

import (
	"gopkg.in/src-d/go-git.v4/plumbing"

	. "gopkg.in/check.v1"
)

type UploadPackRequestSuite struct{}

var _ = Suite(&UploadPackRequestSuite{})

func (s *UploadPackRequestSuite) TestUploadPackRequest_IsEmpty(c *C) {
	r := NewUploadPackRequest()
	r.Want(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Want(plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Have(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	c.Assert(r.IsEmpty(), Equals, false)

	r = NewUploadPackRequest()
	r.Want(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Want(plumbing.NewHash("2b41ef280fdb67a9b250678686a0c3e03b0a9989"))
	r.Have(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))

	c.Assert(r.IsEmpty(), Equals, false)

	r = NewUploadPackRequest()
	r.Want(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))
	r.Have(plumbing.NewHash("d82f291cde9987322c8a0c81a325e1ba6159684c"))

	c.Assert(r.IsEmpty(), Equals, true)
}
