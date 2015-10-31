package git

import (
	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/core"
)

type SuiteRepository struct{}

var _ = Suite(&SuiteRepository{})

func (s *SuiteRepository) TestPull(c *C) {
	r, err := NewRepository(RepositoryFixture)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)
}

func (s *SuiteRepository) TestCommit(c *C) {
	r, err := NewRepository(RepositoryFixture)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	hash := core.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.Commit(hash)
	c.Assert(err, IsNil)

	c.Assert(commit.Hash.IsZero(), Equals, false)
	c.Assert(commit.Tree().Hash.IsZero(), Equals, false)
	c.Assert(commit.Author.Email, Equals, "daniel@lordran.local")
}

func (s *SuiteRepository) TestCommits(c *C) {
	r, err := NewRepository(RepositoryFixture)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	count := 0
	commits := r.Commits()
	for {
		commit, err := commits.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(commit.Hash.IsZero(), Equals, false)
		//c.Assert(commit.Tree.IsZero(), Equals, false)
	}

	c.Assert(count, Equals, 8)
}
