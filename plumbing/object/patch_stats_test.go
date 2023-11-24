package object_test

import (
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type PatchStatsSuite struct {
	fixtures.Suite
}

var _ = Suite(&PatchStatsSuite{})

func (s *PatchStatsSuite) TestStatsWithRename(c *C) {
	cm := &git.CommitOptions{
		Author: &object.Signature{Name: "Foo", Email: "foo@example.local", When: time.Now()},
	}

	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo\nbar\n"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	_, err = w.Commit("foo\n", cm)
	c.Assert(err, IsNil)

	_, err = w.Move("foo", "bar")
	c.Assert(err, IsNil)

	hash, err := w.Commit("rename foo to bar", cm)
	c.Assert(err, IsNil)

	commit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)

	fileStats, err := commit.Stats()
	c.Assert(err, IsNil)
	c.Assert(fileStats[0].Name, Equals, "foo => bar")
}
