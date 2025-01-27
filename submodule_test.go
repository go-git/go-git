package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type SubmoduleSuite struct {
	suite.Suite
	BaseSuite
	Worktree *Worktree
}

func TestSubmoduleSuite(t *testing.T) {
	suite.Run(t, new(SubmoduleSuite))
}

func (s *SubmoduleSuite) SetupTest() {
	path := fixtures.ByTag("submodule").One().Worktree().Root()

	dir, err := os.MkdirTemp("", "")
	s.NoError(err)

	r, err := PlainClone(filepath.Join(dir, "worktree"), &CloneOptions{
		URL: path,
	})

	s.NoError(err)

	s.Repository = r
	s.Worktree, err = r.Worktree()
	s.NoError(err)
}

func (s *SubmoduleSuite) TestInit() {
	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	s.False(sm.initialized)
	err = sm.Init()
	s.NoError(err)

	s.True(sm.initialized)

	cfg, err := s.Repository.Config()
	s.NoError(err)

	s.Len(cfg.Submodules, 1)
	s.NotNil(cfg.Submodules["basic"])

	status, err := sm.Status()
	s.NoError(err)
	s.False(status.IsClean())
}

func (s *SubmoduleSuite) TestUpdate() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})

	s.NoError(err)

	r, err := sm.Repository()
	s.NoError(err)

	ref, err := r.Reference(plumbing.HEAD, true)
	s.NoError(err)
	s.Equal("6ecf0ef2c2dffb796033e5a02219af86ec6584e5", ref.Hash().String())

	status, err := sm.Status()
	s.NoError(err)
	s.True(status.IsClean())
}

func (s *SubmoduleSuite) TestRepositoryWithoutInit() {
	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	r, err := sm.Repository()
	s.ErrorIs(err, ErrSubmoduleNotInitialized)
	s.Nil(r)
}

func (s *SubmoduleSuite) TestUpdateWithoutInit() {
	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{})
	s.ErrorIs(err, ErrSubmoduleNotInitialized)
}

func (s *SubmoduleSuite) TestUpdateWithNotFetch() {
	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:    true,
		NoFetch: true,
	})

	// Since we are not fetching, the object is not there
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *SubmoduleSuite) TestUpdateWithRecursion() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("itself")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:              true,
		RecurseSubmodules: 2,
	})

	s.NoError(err)

	fs := s.Worktree.Filesystem
	_, err = fs.Stat(fs.Join("itself", "basic", "LICENSE"))
	s.NoError(err)
}

func (s *SubmoduleSuite) TestUpdateWithInitAndUpdate() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})
	s.NoError(err)

	idx, err := s.Repository.Storer.Index()
	s.NoError(err)

	for i, e := range idx.Entries {
		if e.Name == "basic" {
			e.Hash = plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")
		}

		idx.Entries[i] = e
	}

	err = s.Repository.Storer.SetIndex(idx)
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{})
	s.NoError(err)

	r, err := sm.Repository()
	s.NoError(err)

	ref, err := r.Reference(plumbing.HEAD, true)
	s.NoError(err)
	s.Equal("b029517f6300c2da0f4b651b8642506cd6aaf45d", ref.Hash().String())

}

func (s *SubmoduleSuite) TestSubmodulesInit() {
	sm, err := s.Worktree.Submodules()
	s.NoError(err)

	err = sm.Init()
	s.NoError(err)

	sm, err = s.Worktree.Submodules()
	s.NoError(err)

	for _, m := range sm {
		s.True(m.initialized)
	}
}

func (s *SubmoduleSuite) TestGitSubmodulesSymlink() {
	f, err := s.Worktree.Filesystem.Create("badfile")
	s.NoError(err)
	defer func() { _ = f.Close() }()

	err = s.Worktree.Filesystem.Remove(gitmodulesFile)
	s.NoError(err)

	err = s.Worktree.Filesystem.Symlink("badfile", gitmodulesFile)
	s.NoError(err)

	_, err = s.Worktree.Submodules()
	s.ErrorIs(err, ErrGitModulesSymlink)
}

func (s *SubmoduleSuite) TestSubmodulesStatus() {
	sm, err := s.Worktree.Submodules()
	s.NoError(err)

	status, err := sm.Status()
	s.NoError(err)
	s.Len(status, 2)
}

func (s *SubmoduleSuite) TestSubmodulesUpdateContext() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodules()
	s.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = sm.UpdateContext(ctx, &SubmoduleUpdateOptions{Init: true})
	s.NotNil(err)
}

func (s *SubmoduleSuite) TestSubmodulesFetchDepth() {
	if testing.Short() {
		s.T().Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	s.NoError(err)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:  true,
		Depth: 1,
	})
	s.NoError(err)

	r, err := sm.Repository()
	s.NoError(err)

	lr, err := r.Log(&LogOptions{})
	s.NoError(err)

	commitCount := 0
	for _, err := lr.Next(); err == nil; _, err = lr.Next() {
		commitCount++
	}
	s.NoError(err)

	s.Equal(1, commitCount)
}

func (s *SubmoduleSuite) TestSubmoduleParseScp() {
	repo := &Repository{
		Storer: memory.NewStorage(),
		wt:     memfs.New(),
	}
	worktree := &Worktree{
		Filesystem: memfs.New(),
		r:          repo,
	}
	submodule := &Submodule{
		initialized: true,
		c:           nil,
		w:           worktree,
	}

	submodule.c = &config.Submodule{
		URL: "git@github.com:username/submodule_repo",
	}

	_, err := submodule.Repository()
	s.NoError(err)
}
