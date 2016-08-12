package git

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4/core"

	"github.com/alcortesm/tgz"
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

func (s *RepositorySuite) SetUpSuite(c *C) {
	s.repos = unpackFixtures(c, tagFixtures, treeWalkerFixtures)

	s.dirFixtures = make(map[string]dirFixture, len(dirFixturesInit))
	for _, fix := range dirFixturesInit {
		com := Commentf("fixture name = %s\n", fix.name)

		path, err := tgz.Extract(fix.tgz)
		c.Assert(err, IsNil, com)

		s.dirFixtures[fix.name] = dirFixture{
			path: path,
			head: core.NewHash(fix.head),
		}
	}
}

func (s *RepositorySuite) TearDownSuite(c *C) {
	for name, fix := range s.dirFixtures {
		err := os.RemoveAll(fix.path)
		c.Assert(err, IsNil, Commentf("cannot delete tmp dir for fixture %s: %s\n",
			name, fix.path))
	}
}

/*
func (s *RepositorySuite) TestNewRepository(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	c.Assert(err, IsNil)
	c.Assert(r.Remotes["origin"].Auth, IsNil)
}

func (s *RepositorySuite) TestNewRepositoryWithAuth(c *C) {
	auth := &http.BasicAuth{}
	r, err := NewRepository(RepositoryFixture, auth)
	c.Assert(err, IsNil)
	c.Assert(r.Remotes["origin"].Auth, Equals, auth)
}


func (s *RepositorySuite) TestNewRepositoryFromFS(c *C) {
	for name, fix := range s.dirFixtures {
		fs := fs.NewOS()
		gitPath := fs.Join(fix.path, ".git/")
		com := Commentf("dir fixture %q → %q\n", name, gitPath)
		repo, err := NewRepositoryFromFS(fs, gitPath)
		c.Assert(err, IsNil, com)

		err = repo.PullDefault()
		c.Assert(err, ErrorMatches, `unable to find remote "origin"`)

		c.Assert(repo.Storage, NotNil, com)
		c.Assert(repo.Storage, FitsTypeOf, &filesystem.ObjectStorage{}, com)
	}
}


func (s *RepositorySuite) TestClone(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	mock, ok := (r.Remotes["origin"].upSrv).(*MockGitUploadPackService)
	c.Assert(ok, Equals, true)
	err = mock.RC.Close()
	c.Assert(err, Not(IsNil), Commentf("pull leaks an open fd from the fetch"))
}


func (s *RepositorySuite) TestCommit(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	hash := core.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.Commit(hash)
	c.Assert(err, IsNil)

	c.Assert(commit.Hash.IsZero(), Equals, false)
	c.Assert(commit.Hash, Equals, commit.ID())
	c.Assert(commit.Hash, Equals, hash)
	c.Assert(commit.Type(), Equals, core.CommitObject)
	c.Assert(commit.Tree().Hash.IsZero(), Equals, false)
	c.Assert(commit.Author.Email, Equals, "daniel@lordran.local")
}
*/

func (s *RepositorySuite) TestCommits(c *C) {
	r, err := NewMemoryRepository()
	c.Assert(err, IsNil)

	err = r.Clone(&CloneOptions{URL: RepositoryFixture})
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

	c.Assert(count, Equals, 8)
}

func (s *RepositorySuite) TestTag(c *C) {
	for i, t := range tagTests {
		r, ok := s.repos[t.repo]
		c.Assert(ok, Equals, true)
		k := 0
		for hashString, exp := range t.tags {
			hash := core.NewHash(hashString)
			tag, err := r.Tag(hash)
			c.Assert(err, IsNil)
			testTagExpected(c, tag, hash, exp, fmt.Sprintf("subtest %d, tag %d: ", i, k))
			k++
		}
	}
}

func (s *RepositorySuite) TestTags(c *C) {
	for i, t := range tagTests {
		r, ok := s.repos[t.repo]
		c.Assert(ok, Equals, true)
		tagsIter, err := r.Tags()
		c.Assert(err, IsNil)
		testTagIter(c, tagsIter, t.tags, fmt.Sprintf("subtest %d, ", i))
	}
}

func (s *RepositorySuite) TestObject(c *C) {
	for i, t := range treeWalkerTests {
		r, ok := s.repos[t.repo]
		c.Assert(ok, Equals, true)
		for k := 0; k < len(t.objs); k++ {
			com := fmt.Sprintf("subtest %d, tag %d", i, k)
			info := t.objs[k]
			hash := core.NewHash(info.Hash)
			obj, err := r.Object(hash)
			c.Assert(err, IsNil, Commentf(com))
			c.Assert(obj.Type(), Equals, info.Kind, Commentf(com))
			c.Assert(obj.ID(), Equals, hash, Commentf(com))
		}
	}
}

/*
func (s *RepositorySuite) TestCommitIterClosePanic(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	r.Remotes["origin"].upSrv = &MockGitUploadPackService{}

	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	commits, err := r.Commits()
	c.Assert(err, IsNil)
	commits.Close()
}


func (s *RepositorySuite) TestHeadFromFs(c *C) {
	for name, fix := range s.dirFixtures {
		fs := fs.NewOS()
		gitPath := fs.Join(fix.path, ".git/")
		com := Commentf("dir fixture %q → %q\n", name, gitPath)
		repo, err := NewRepositoryFromFS(fs, gitPath)
		c.Assert(err, IsNil, com)

		head, err := repo.Head("")
		c.Assert(err, IsNil)

		c.Assert(head, Equals, fix.head)
	}
}

func (s *RepositorySuite) TestHeadFromRemote(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	c.Assert(err, IsNil)

	upSrv := &MockGitUploadPackService{}
	r.Remotes[DefaultRemoteName].upSrv = upSrv
	err = r.Remotes[DefaultRemoteName].Connect()
	c.Assert(err, IsNil)

	info, err := upSrv.Info()
	c.Assert(err, IsNil)
	expected := info.Head

	obtained, err := r.Head(DefaultRemoteName)
	c.Assert(err, IsNil)

	c.Assert(obtained, Equals, expected)
}

func (s *RepositorySuite) TestHeadErrors(c *C) {
	r, err := NewRepository(RepositoryFixture, nil)
	c.Assert(err, IsNil)

	upSrv := &MockGitUploadPackService{}
	r.Remotes[DefaultRemoteName].upSrv = upSrv

	remote := "not found"
	_, err = r.Head(remote)
	c.Assert(err, ErrorMatches, fmt.Sprintf("unable to find remote %q", remote))

	remote = ""
	_, err = r.Head(remote)
	c.Assert(err, ErrorMatches, "cannot retrieve local head: no local data found")
}
*/
