package transactional

import (
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

var _ = Suite(&IndexSuite{})

type IndexSuite struct{}

func (s *IndexSuite) TestSetIndexBase(c *C) {
	idx := &index.Index{}
	idx.Version = 2

	base := memory.NewStorage()
	err := base.SetIndex(idx)
	c.Assert(err, IsNil)

	temporal := memory.NewStorage()
	cs := NewIndexStorage(base, temporal)

	idx, err = cs.Index()
	c.Assert(err, IsNil)
	c.Assert(idx.Version, Equals, uint32(2))
}

func (s *IndexSuite) TestCommit(c *C) {
	idx := &index.Index{}
	idx.Version = 2

	base := memory.NewStorage()
	err := base.SetIndex(idx)
	c.Assert(err, IsNil)

	temporal := memory.NewStorage()

	idx = &index.Index{}
	idx.Version = 3

	is := NewIndexStorage(base, temporal)
	err = is.SetIndex(idx)
	c.Assert(err, IsNil)

	err = is.Commit()
	c.Assert(err, IsNil)

	baseIndex, err := base.Index()
	c.Assert(err, IsNil)
	c.Assert(baseIndex.Version, Equals, uint32(3))
}
