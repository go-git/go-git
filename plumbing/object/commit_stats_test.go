package object_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

type CommitStatsSuite struct {
	suite.Suite
}

func TestCommitStatsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(CommitStatsSuite))
}

func (s *CommitStatsSuite) TestStats() {
	r, hash := s.writeHistory([]byte("foo\n"), []byte("foo\nbar\n"))

	aCommit, err := r.CommitObject(hash)
	s.NoError(err)

	fileStats, err := aCommit.StatsContext(context.Background())
	s.NoError(err)

	s.Equal("foo", fileStats[0].Name)
	s.Equal(1, fileStats[0].Addition)
	s.Equal(0, fileStats[0].Deletion)
	s.Equal(" foo | 1 +\n", fileStats[0].String())
}

func (s *CommitStatsSuite) TestStats_RootCommit() {
	r, hash := s.writeHistory([]byte("foo\n"))

	aCommit, err := r.CommitObject(hash)
	s.NoError(err)

	fileStats, err := aCommit.Stats()
	s.NoError(err)

	s.Len(fileStats, 1)
	s.Equal("foo", fileStats[0].Name)
	s.Equal(1, fileStats[0].Addition)
	s.Equal(0, fileStats[0].Deletion)
	s.Equal(" foo | 1 +\n", fileStats[0].String())
}

func (s *CommitStatsSuite) TestStats_WithoutNewLine() {
	r, hash := s.writeHistory([]byte("foo\nbar"), []byte("foo\nbar\n"))

	aCommit, err := r.CommitObject(hash)
	s.NoError(err)

	fileStats, err := aCommit.Stats()
	s.NoError(err)

	s.Equal("foo", fileStats[0].Name)
	s.Equal(1, fileStats[0].Addition)
	s.Equal(1, fileStats[0].Deletion)
	s.Equal(" foo | 2 +-\n", fileStats[0].String())
}

func (s *CommitStatsSuite) writeHistory(files ...[]byte) (*git.Repository, plumbing.Hash) {
	cm := &git.CommitOptions{
		Author: &object.Signature{Name: "Foo", Email: "foo@example.local", When: time.Now()},
	}

	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), git.WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	var hash plumbing.Hash
	for _, content := range files {
		util.WriteFile(fs, "foo", content, 0o644)

		_, err = w.Add("foo")
		s.NoError(err)

		hash, err = w.Commit("foo\n", cm)
		s.NoError(err)
	}

	return r, hash
}
