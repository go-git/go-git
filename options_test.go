package git

import (
	"github.com/go-git/go-git/v5/plumbing/object"
	. "gopkg.in/check.v1"
)

type OptionsSuite struct {
	BaseSuite
}

var _ = Suite(&OptionsSuite{})

func (s *OptionsSuite) TestCommitOptionsParentsFromHEAD(c *C) {
	o := CommitOptions{Author: &object.Signature{}}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)
	c.Assert(o.Parents, HasLen, 1)
}

func (s *OptionsSuite) TestCommitOptionsCommitter(c *C) {
	sig := &object.Signature{}

	o := CommitOptions{Author: sig}
	err := o.Validate(s.Repository)
	c.Assert(err, IsNil)

	c.Assert(o.Committer, Equals, o.Author)
}
