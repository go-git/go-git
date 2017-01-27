package git

import (
	"io/ioutil"

	. "gopkg.in/check.v1"
	"srcd.works/go-billy.v1/memory"
)

type WorktreeSuite struct {
	BaseSuite
}

var _ = Suite(&WorktreeSuite{})

func (s *WorktreeSuite) TestCheckout(c *C) {
	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	fs := memory.New()
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	entries, err := fs.ReadDir("/")
	c.Assert(err, IsNil)

	c.Assert(entries, HasLen, 8)
	ch, err := fs.Open("CHANGELOG")
	c.Assert(err, IsNil)

	content, err := ioutil.ReadAll(ch)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "Initial changelog\n")
}
