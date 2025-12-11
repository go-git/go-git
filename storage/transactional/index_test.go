package transactional

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestIndexSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(IndexSuite))
}

type IndexSuite struct {
	suite.Suite
}

func (s *IndexSuite) TestSetIndexBase() {
	idx := &index.Index{}
	idx.Version = 2

	base := memory.NewStorage()
	err := base.SetIndex(idx)
	s.NoError(err)

	temporal := memory.NewStorage()
	cs := NewIndexStorage(base, temporal)

	idx, err = cs.Index()
	s.NoError(err)
	s.Equal(uint32(2), idx.Version)
}

func (s *IndexSuite) TestCommit() {
	idx := &index.Index{}
	idx.Version = 2

	base := memory.NewStorage()
	err := base.SetIndex(idx)
	s.NoError(err)

	temporal := memory.NewStorage()

	idx = &index.Index{}
	idx.Version = 3

	is := NewIndexStorage(base, temporal)
	err = is.SetIndex(idx)
	s.NoError(err)

	err = is.Commit()
	s.NoError(err)

	baseIndex, err := base.Index()
	s.NoError(err)
	s.Equal(uint32(3), baseIndex.Version)
}
