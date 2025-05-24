package object_test

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type PatchStatsFixtureSuite struct {
	fixtures.Suite
}

type PatchStatsSuite struct {
	suite.Suite
	PatchStatsFixtureSuite
}

func TestPatchStatsSuite(t *testing.T) {
	suite.Run(t, new(PatchStatsSuite))
}

func (s *PatchStatsSuite) TestStatsWithRename() {
	cm := &git.CommitOptions{
		Author: &object.Signature{Name: "Foo", Email: "foo@example.local", When: time.Now()},
	}

	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), git.WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo\nbar\n"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	_, err = w.Commit("foo\n", cm)
	s.NoError(err)

	_, err = w.Move("foo", "bar")
	s.NoError(err)

	hash, err := w.Commit("rename foo to bar", cm)
	s.NoError(err)

	commit, err := r.CommitObject(hash)
	s.NoError(err)

	fileStats, err := commit.Stats()
	s.NoError(err)
	s.Equal("foo => bar", fileStats[0].Name)
}
