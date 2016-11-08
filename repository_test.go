package git

import (
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

type RepositorySuite struct {
	BaseSuite
}

var _ = Suite(&RepositorySuite{})

func (s *RepositorySuite) TestNewRepository(c *C) {
	r := NewMemoryRepository()
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestCreateRemoteAndRemote(c *C) {
	r := NewMemoryRepository()
	remote, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URL:  "http://foo/foo.git",
	})

	c.Assert(err, IsNil)
	c.Assert(remote.Config().Name, Equals, "foo")

	alt, err := r.Remote("foo")
	c.Assert(err, IsNil)
	c.Assert(alt, Not(Equals), remote)
	c.Assert(alt.Config().Name, Equals, "foo")
}

func (s *RepositorySuite) TestCreateRemoteInvalid(c *C) {
	r := NewMemoryRepository()
	remote, err := r.CreateRemote(&config.RemoteConfig{})

	c.Assert(err, Equals, config.ErrRemoteConfigEmptyName)
	c.Assert(remote, IsNil)
}

func (s *RepositorySuite) TestDeleteRemote(c *C) {
	r := NewMemoryRepository()
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URL:  "http://foo/foo.git",
	})

	c.Assert(err, IsNil)

	err = r.DeleteRemote("foo")
	c.Assert(err, IsNil)

	alt, err := r.Remote("foo")
	c.Assert(err, Equals, ErrRemoteNotFound)
	c.Assert(alt, IsNil)
}

func (s *RepositorySuite) TestClone(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL: RepositoryFixture,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/branch", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *RepositorySuite) TestCloneNonEmpty(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
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
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL:           RepositoryFixture,
		ReferenceName: plumbing.ReferenceName("refs/heads/branch"),
		SingleBranch:  true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/branch")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	branch, err = r.Ref("refs/remotes/origin/branch", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *RepositorySuite) TestCloneSingleBranch(c *C) {
	r := NewMemoryRepository()

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.Clone(&CloneOptions{
		URL:          RepositoryFixture,
		SingleBranch: true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Ref(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Ref("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestCloneDetachedHEAD(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{
		URL:           RepositoryFixture,
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
	})

	head, err := r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.HashReference)
	c.Assert(head.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestPull(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{
		URL:          RepositoryFixture,
		SingleBranch: true,
	})

	c.Assert(err, IsNil)

	err = r.Pull(&PullOptions{})
	c.Assert(err, Equals, NoErrAlreadyUpToDate)

	branch, err := r.Ref("refs/heads/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	storage := r.s.(*memory.Storage)
	c.Assert(storage.Objects, HasLen, 31)

	r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URL:  "https://github.com/git-fixtures/tags.git",
	})

	err = r.Pull(&PullOptions{RemoteName: "foo"})
	c.Assert(err, IsNil)
	c.Assert(storage.Objects, HasLen, 38)

	branch, err = r.Ref("refs/heads/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch.Hash().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
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

	hash := plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.Commit(hash)
	c.Assert(err, IsNil)

	c.Assert(commit.Hash.IsZero(), Equals, false)
	c.Assert(commit.Hash, Equals, commit.ID())
	c.Assert(commit.Hash, Equals, hash)
	c.Assert(commit.Type(), Equals, plumbing.CommitObject)

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
		c.Assert(commit.Type(), Equals, plumbing.CommitObject)
	}

	c.Assert(count, Equals, 9)
}

func (s *RepositorySuite) TestTag(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/spinnaker/spinnaker.git"})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Tag(hash)
	c.Assert(err, IsNil)

	c.Assert(tag.Hash.IsZero(), Equals, false)
	c.Assert(tag.Hash, Equals, hash)
	c.Assert(tag.Type(), Equals, plumbing.TagObject)
}

func (s *RepositorySuite) TestTags(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/git-fixtures/tags.git"})
	c.Assert(err, IsNil)

	count := 0
	tags, err := r.Tags()
	c.Assert(err, IsNil)

	tags.ForEach(func(tag *Tag) error {
		count++
		c.Assert(tag.Hash.IsZero(), Equals, false)
		c.Assert(tag.Type(), Equals, plumbing.TagObject)

		return nil
	})

	refs, _ := r.Refs()
	refs.ForEach(func(ref *plumbing.Reference) error {
		return nil
	})

	c.Assert(count, Equals, 4)
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

	ref, err := r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.HEAD)

	ref, err = r.Ref(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.ReferenceName("refs/heads/master"))
}

func (s *RepositorySuite) TestRefs(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: RepositoryFixture})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)

	iter, err := r.Refs()
	c.Assert(err, IsNil)
	c.Assert(iter, NotNil)
}

func (s *RepositorySuite) TestObject(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/spinnaker/spinnaker.git"})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Tag(hash)
	c.Assert(err, IsNil)

	c.Assert(tag.Hash.IsZero(), Equals, false)
	c.Assert(tag.Hash, Equals, hash)
	c.Assert(tag.Type(), Equals, plumbing.TagObject)
}

func (s *RepositorySuite) TestObjectNotFound(c *C) {
	r := NewMemoryRepository()
	err := r.Clone(&CloneOptions{URL: "https://github.com/git-fixtures/basic.git"})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Object(plumbing.TagObject, hash)
	c.Assert(err, DeepEquals, plumbing.ErrObjectNotFound)
	c.Assert(tag, IsNil)
}
