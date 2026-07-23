package object_test

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

type PatchStatsSuite struct {
	suite.Suite
}

func TestPatchStatsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PatchStatsSuite))
}

func (s *PatchStatsSuite) TestStatsWithRename() {
	cm := &git.CommitOptions{
		Author: &object.Signature{Name: "Foo", Email: "foo@example.local", When: time.Now()},
	}

	ctx := s.T().Context()
	fs := memfs.New()
	r, err := git.Init(ctx, memory.NewStorage(), git.WithWorkTree(fs))
	s.NoError(err)
	defer func() { _ = r.Close() }()

	w, err := r.Worktree(ctx)
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo\nbar\n"), 0o644)

	_, err = w.Add(ctx, "foo")
	s.NoError(err)

	_, err = w.Commit(ctx, "foo\n", cm)
	s.NoError(err)

	_, err = w.Move(ctx, "foo", "bar")
	s.NoError(err)

	hash, err := w.Commit(ctx, "rename foo to bar", cm)
	s.NoError(err)

	commit, err := r.CommitObject(ctx, hash)
	s.NoError(err)

	fileStats, err := commit.Stats(ctx)
	s.NoError(err)
	s.Equal("foo => bar", fileStats[0].Name)
}
