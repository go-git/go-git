package transactional

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"
)

func TestShallowSuite(t *testing.T) {
	suite.Run(t, new(ShallowSuite))
}

type ShallowSuite struct {
	suite.Suite
}

func (s *ShallowSuite) TestShallow() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	err := base.SetShallow([]plumbing.Hash{commitA})
	s.NoError(err)

	err = rs.SetShallow([]plumbing.Hash{commitB})
	s.NoError(err)

	commits, err := rs.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])

	commits, err = base.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitA, commits[0])
}

func (s *ShallowSuite) TestCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()

	rs := NewShallowStorage(base, temporal)

	commitA := plumbing.NewHash("bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	commitB := plumbing.NewHash("aa9968d75e48de59f0870ffb71f5e160bbbdcf52")

	s.Nil(base.SetShallow([]plumbing.Hash{commitA}))
	s.Nil(rs.SetShallow([]plumbing.Hash{commitB}))

	s.Nil(rs.Commit())

	commits, err := rs.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])

	commits, err = base.Shallow()
	s.NoError(err)
	s.Len(commits, 1)
	s.Equal(commitB, commits[0])
}
