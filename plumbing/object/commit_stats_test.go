package object_test

import (
	"context"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type CommitStatsSuite struct {
	fixtures.Suite
}

var _ = Suite(&CommitStatsSuite{})

func (s *CommitStatsSuite) TestStats(c *C) {
	r, hash := s.writeHistory(c, []byte("foo\n"), []byte("foo\nbar\n"))

	aCommit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)

	fileStats, err := aCommit.StatsContext(context.Background())
	c.Assert(err, IsNil)

	c.Assert(fileStats[0].Name, Equals, "foo")
	c.Assert(fileStats[0].Addition, Equals, 1)
	c.Assert(fileStats[0].Deletion, Equals, 0)
	c.Assert(fileStats[0].String(), Equals, " foo | 1 +\n")
}

func (s *CommitStatsSuite) TestStats_RootCommit(c *C) {
	r, hash := s.writeHistory(c, []byte("foo\n"))

	aCommit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)

	fileStats, err := aCommit.Stats()
	c.Assert(err, IsNil)

	c.Assert(fileStats, HasLen, 1)
	c.Assert(fileStats[0].Name, Equals, "foo")
	c.Assert(fileStats[0].Addition, Equals, 1)
	c.Assert(fileStats[0].Deletion, Equals, 0)
	c.Assert(fileStats[0].String(), Equals, " foo | 1 +\n")
}

func (s *CommitStatsSuite) TestStats_WithoutNewLine(c *C) {
	r, hash := s.writeHistory(c, []byte("foo\nbar"), []byte("foo\nbar\n"))

	aCommit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)

	fileStats, err := aCommit.Stats()
	c.Assert(err, IsNil)

	c.Assert(fileStats[0].Name, Equals, "foo")
	c.Assert(fileStats[0].Addition, Equals, 1)
	c.Assert(fileStats[0].Deletion, Equals, 1)
	c.Assert(fileStats[0].String(), Equals, " foo | 2 +-\n")
}

func (s *CommitStatsSuite) writeHistory(c *C, files ...[]byte) (*git.Repository, plumbing.Hash) {
	cm := &git.CommitOptions{
		Author: &object.Signature{Name: "Foo", Email: "foo@example.local", When: time.Now()},
	}

	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	var hash plumbing.Hash
	for _, content := range files {
		util.WriteFile(fs, "foo", content, 0644)

		_, err = w.Add("foo")
		c.Assert(err, IsNil)

		hash, err = w.Commit("foo\n", cm)
		c.Assert(err, IsNil)

	}

	return r, hash
}
