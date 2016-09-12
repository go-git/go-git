package git

import (
	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

var dirFixturesInit = [...]struct {
	name string
	tgz  string
	head string
}{
	{
		name: "binrels",
		tgz:  "storage/filesystem/internal/dotgit/fixtures/alcortesm-binary-relations.tgz",
		head: "c44b5176e99085c8fe36fa27b045590a7b9d34c9",
	},
}

type dirFixture struct {
	path string
	head core.Hash
}

type RepositorySuite struct {
	BaseSuite
	repos       map[string]*Repository
	dirFixtures map[string]dirFixture
}

var _ = Suite(&RepositorySuite{})

func (s *RepositorySuite) TestNewRepository(c *C) {
	r := NewMemoryRepository()
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestClone(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL: RepositoryFixture,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, core.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, core.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/branch", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, core.HashReference)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *RepositorySuite) TestCloneNonEmpty(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	o := &CloneOptions{URL: RepositoryFixture}
	err = r.Clone(o)
	c.Assert(err, IsNil)

	err = r.Clone(o)
	c.Assert(err, Equals, ErrRepositoryNonEmpty)
}

func (s *RepositorySuite) TestCloneSingleBranchAndNonHEAD(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL:           RepositoryFixture,
		ReferenceName: core.ReferenceName("refs/heads/branch"),
		SingleBranch:  true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, core.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/branch")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	branch, err = r.Ref("refs/remotes/origin/branch", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, core.HashReference)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *RepositorySuite) TestCloneSingleBranch(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, core.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL:          RepositoryFixture,
		SingleBranch: true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, core.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, core.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestCloneDetachedHEAD(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{
		URL:           RepositoryFixture,
		ReferenceName: core.ReferenceName("refs/tags/v1.0.0"),
	})

	head, err := r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, core.HashReference)
	c.Assert(head.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestIsEmpty(c *C) {
	r := NewMemoryRepository()

	empty, err := r.IsEmpty()
	c.Assert(err, IsNil)
	c.Assert(empty, Equals, true)

	err = r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	empty, err = r.IsEmpty()
	c.Assert(err, IsNil)
	c.Assert(empty, Equals, false)
}

func (s *RepositorySuite) TestCommit(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{
		URL: RepositoryFixture,
	})

	c.Assert(err, IsNil)

	hash := core.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.Commit(hash)
	c.Assert(err, IsNil)

	c.Assert(commit.Hash.IsZero(), Equals, false)
	c.Assert(commit.Hash, Equals, commit.ID())
	c.Assert(commit.Hash, Equals, hash)
	c.Assert(commit.Type(), Equals, core.CommitObject)

	tree, err := commit.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.Hash.IsZero(), Equals, false)

	c.Assert(commit.Author.Email, Equals, "daniel@lordran.local")
}

func (s *RepositorySuite) TestCommits(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	count := 0
	commits, err := r.Commits()
	c.Assert(err, IsNil)
	for {
		commit, err := commits.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(commit.Hash.IsZero(), Equals, false)
		c.Assert(commit.Hash, Equals, commit.ID())
		c.Assert(commit.Type(), Equals, core.CommitObject)
	}

	c.Assert(count, Equals, 9)
}

func (s *RepositorySuite) TestTag(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/spinnaker/spinnaker.git"})
	c.Assert(err, IsNil)

	hash := core.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Tag(hash)
	c.Assert(err, IsNil)

	c.Assert(tag.Hash.IsZero(), Equals, false)
	c.Assert(tag.Hash, Equals, hash)
	c.Assert(tag.Type(), Equals, core.TagObject)

}

func (s *RepositorySuite) TestTags(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/spinnaker/spinnaker.git"})
	c.Assert(err, IsNil)

	count := 0
	tags, err := r.Tags()
	c.Assert(err, IsNil)
	for {
		tag, err := tags.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(tag.Hash.IsZero(), Equals, false)
		c.Assert(tag.Type(), Equals, core.TagObject)
	}

	c.Assert(count, Equals, 11)
}

func (s *RepositorySuite) TestCommitIterClosePanic(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	commits, err := r.Commits()
	c.Assert(err, IsNil)
	commits.Close()
}

func (s *RepositorySuite) TestRef(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	ref, err := r.Ref(core.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, core.HEAD)

	ref, err = r.Ref(core.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, core.ReferenceName("refs/heads/master"))
}

func (s *RepositorySuite) TestRefs(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)
	c.Assert(r.Refs(), NotNil)
}
