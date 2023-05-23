package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v4"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	openpgperr "github.com/ProtonMail/go-crypto/openpgp/errors"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	. "gopkg.in/check.v1"
)

type RepositorySuite struct {
	BaseSuite
}

var _ = Suite(&RepositorySuite{})

func (s *RepositorySuite) TestInit(c *C) {
	r, err := Init(memory.NewStorage(), memfs.New())
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Core.IsBare, Equals, false)

	// check the HEAD to see what the default branch is
	createCommit(c, r)
	ref, err := r.Head()
	c.Assert(err, IsNil)
	c.Assert(ref.Name().String(), Equals, plumbing.Master.String())
}

func (s *RepositorySuite) TestInitWithOptions(c *C) {
	r, err := InitWithOptions(memory.NewStorage(), memfs.New(), InitOptions{
		DefaultBranch: "refs/heads/foo",
	})
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
	createCommit(c, r)

	ref, err := r.Head()
	c.Assert(err, IsNil)
	c.Assert(ref.Name().String(), Equals, "refs/heads/foo")

}

func createCommit(c *C, r *Repository) {
	// Create a commit so there is a HEAD to check
	wt, err := r.Worktree()
	c.Assert(err, IsNil)

	rm, err := wt.Filesystem.Create("foo.txt")
	c.Assert(err, IsNil)

	_, err = rm.Write([]byte("foo text"))
	c.Assert(err, IsNil)

	_, err = wt.Add("foo.txt")
	c.Assert(err, IsNil)

	author := object.Signature{
		Name:  "go-git",
		Email: "go-git@fake.local",
		When:  time.Now(),
	}
	_, err = wt.Commit("test commit message", &CommitOptions{
		All:       true,
		Author:    &author,
		Committer: &author,
	})
	c.Assert(err, IsNil)

}

func (s *RepositorySuite) TestInitNonStandardDotGit(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	fs := osfs.New(dir)
	dot, _ := fs.Chroot("storage")
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	wt, _ := fs.Chroot("worktree")
	r, err := Init(st, wt)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	f, err := fs.Open(fs.Join("worktree", ".git"))
	c.Assert(err, IsNil)
	defer func() { _ = f.Close() }()

	all, err := io.ReadAll(f)
	c.Assert(err, IsNil)
	c.Assert(string(all), Equals, fmt.Sprintf("gitdir: %s\n", filepath.Join("..", "storage")))

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Core.Worktree, Equals, filepath.Join("..", "worktree"))
}

func (s *RepositorySuite) TestInitStandardDotGit(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	fs := osfs.New(dir)
	dot, _ := fs.Chroot(".git")
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	r, err := Init(st, fs)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	l, err := fs.ReadDir(".git")
	c.Assert(err, IsNil)
	c.Assert(len(l) > 0, Equals, true)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Core.Worktree, Equals, "")
}

func (s *RepositorySuite) TestInitBare(c *C) {
	r, err := Init(memory.NewStorage(), nil)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Core.IsBare, Equals, true)

}

func (s *RepositorySuite) TestInitAlreadyExists(c *C) {
	st := memory.NewStorage()

	r, err := Init(st, nil)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = Init(st, nil)
	c.Assert(err, Equals, ErrRepositoryAlreadyExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestOpen(c *C) {
	st := memory.NewStorage()

	r, err := Init(st, memfs.New())
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = Open(st, memfs.New())
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestOpenBare(c *C) {
	st := memory.NewStorage()

	r, err := Init(st, nil)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = Open(st, nil)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestOpenBareMissingWorktree(c *C) {
	st := memory.NewStorage()

	r, err := Init(st, memfs.New())
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = Open(st, nil)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestOpenNotExists(c *C) {
	r, err := Open(memory.NewStorage(), nil)
	c.Assert(err, Equals, ErrRepositoryNotExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestClone(c *C) {
	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
}

func (s *RepositorySuite) TestCloneContext(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r, err := CloneContext(ctx, memory.NewStorage(), nil, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(r, NotNil)
	c.Assert(err, Equals, context.Canceled)
}

func (s *RepositorySuite) TestCloneMirror(c *C) {
	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{
		URL:    fixtures.Basic().One().URL,
		Mirror: true,
	})

	c.Assert(err, IsNil)

	refs, err := r.References()
	var count int
	refs.ForEach(func(r *plumbing.Reference) error { c.Log(r); count++; return nil })
	c.Assert(err, IsNil)
	// 6 refs total from github.com/git-fixtures/basic.git:
	//  - HEAD
	//  - refs/heads/master
	//  - refs/heads/branch
	//  - refs/pull/1/head
	//  - refs/pull/2/head
	//  - refs/pull/2/merge
	c.Assert(count, Equals, 6)

	cfg, err := r.Config()
	c.Assert(err, IsNil)

	c.Assert(cfg.Core.IsBare, Equals, true)
	c.Assert(cfg.Remotes[DefaultRemoteName].Validate(), IsNil)
	c.Assert(cfg.Remotes[DefaultRemoteName].Mirror, Equals, true)
}

func (s *RepositorySuite) TestCloneWithTags(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, err := Clone(memory.NewStorage(), nil, &CloneOptions{URL: url, Tags: NoTags})
	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	i, err := r.References()
	c.Assert(err, IsNil)

	var count int
	i.ForEach(func(r *plumbing.Reference) error { count++; return nil })

	c.Assert(count, Equals, 3)
}

func (s *RepositorySuite) TestCloneSparse(c *C) {
	fs := memfs.New()
	r, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	sparseCheckoutDirectories := []string{"go", "json", "php"}
	c.Assert(w.Checkout(&CheckoutOptions{
		Branch:                    "refs/heads/master",
		SparseCheckoutDirectories: sparseCheckoutDirectories,
	}), IsNil)

	fis, err := fs.ReadDir(".")
	c.Assert(err, IsNil)
	for _, fi := range fis {
		c.Assert(fi.IsDir(), Equals, true)
		var oneOfSparseCheckoutDirs bool

		for _, sparseCheckoutDirectory := range sparseCheckoutDirectories {
			if strings.HasPrefix(fi.Name(), sparseCheckoutDirectory) {
				oneOfSparseCheckoutDirs = true
			}
		}
		c.Assert(oneOfSparseCheckoutDirs, Equals, true)
	}
}

func (s *RepositorySuite) TestCreateRemoteAndRemote(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	remote, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})

	c.Assert(err, IsNil)
	c.Assert(remote.Config().Name, Equals, "foo")

	alt, err := r.Remote("foo")
	c.Assert(err, IsNil)
	c.Assert(alt, Not(Equals), remote)
	c.Assert(alt.Config().Name, Equals, "foo")
}

func (s *RepositorySuite) TestCreateRemoteInvalid(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	remote, err := r.CreateRemote(&config.RemoteConfig{})

	c.Assert(err, Equals, config.ErrRemoteConfigEmptyName)
	c.Assert(remote, IsNil)
}

func (s *RepositorySuite) TestCreateRemoteAnonymous(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	c.Assert(err, IsNil)
	c.Assert(remote.Config().Name, Equals, "anonymous")
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalidName(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: "not_anonymous",
		URLs: []string{"http://foo/foo.git"},
	})

	c.Assert(err, Equals, ErrAnonymousRemoteName)
	c.Assert(remote, IsNil)
}

func (s *RepositorySuite) TestCreateRemoteAnonymousInvalid(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	remote, err := r.CreateRemoteAnonymous(&config.RemoteConfig{})

	c.Assert(err, Equals, config.ErrRemoteConfigEmptyName)
	c.Assert(remote, IsNil)
}

func (s *RepositorySuite) TestDeleteRemote(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})

	c.Assert(err, IsNil)

	err = r.DeleteRemote("foo")
	c.Assert(err, IsNil)

	alt, err := r.Remote("foo")
	c.Assert(err, Equals, ErrRemoteNotFound)
	c.Assert(alt, IsNil)
}

func (s *RepositorySuite) TestCreateBranchAndBranch(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err := r.CreateBranch(testBranch)

	c.Assert(err, IsNil)
	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(len(cfg.Branches), Equals, 1)
	branch := cfg.Branches["foo"]
	c.Assert(branch.Name, Equals, testBranch.Name)
	c.Assert(branch.Remote, Equals, testBranch.Remote)
	c.Assert(branch.Merge, Equals, testBranch.Merge)

	branch, err = r.Branch("foo")
	c.Assert(err, IsNil)
	c.Assert(branch.Name, Equals, testBranch.Name)
	c.Assert(branch.Remote, Equals, testBranch.Remote)
	c.Assert(branch.Merge, Equals, testBranch.Merge)
}

func (s *RepositorySuite) TestCreateBranchUnmarshal(c *C) {
	r, _ := Init(memory.NewStorage(), nil)

	expected := []byte(`[core]
	bare = true
[remote "foo"]
	url = http://foo/foo.git
	fetch = +refs/heads/*:refs/remotes/foo/*
[branch "foo"]
	remote = origin
	merge = refs/heads/foo
[branch "master"]
	remote = origin
	merge = refs/heads/master
`)

	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{"http://foo/foo.git"},
	})
	c.Assert(err, IsNil)
	testBranch1 := &config.Branch{
		Name:   "master",
		Remote: "origin",
		Merge:  "refs/heads/master",
	}
	testBranch2 := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err = r.CreateBranch(testBranch1)
	c.Assert(err, IsNil)
	err = r.CreateBranch(testBranch2)
	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	marshaled, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(string(expected), Equals, string(marshaled))
}

func (s *RepositorySuite) TestBranchInvalid(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	branch, err := r.Branch("foo")

	c.Assert(err, NotNil)
	c.Assert(branch, IsNil)
}

func (s *RepositorySuite) TestCreateBranchInvalid(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.CreateBranch(&config.Branch{})

	c.Assert(err, NotNil)

	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err = r.CreateBranch(testBranch)
	c.Assert(err, IsNil)
	err = r.CreateBranch(testBranch)
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestDeleteBranch(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	testBranch := &config.Branch{
		Name:   "foo",
		Remote: "origin",
		Merge:  "refs/heads/foo",
	}
	err := r.CreateBranch(testBranch)

	c.Assert(err, IsNil)

	err = r.DeleteBranch("foo")
	c.Assert(err, IsNil)

	b, err := r.Branch("foo")
	c.Assert(err, Equals, ErrBranchNotFound)
	c.Assert(b, IsNil)

	err = r.DeleteBranch("foo")
	c.Assert(err, Equals, ErrBranchNotFound)
}

func (s *RepositorySuite) TestPlainInit(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Core.IsBare, Equals, true)
}

func (s *RepositorySuite) TestPlainInitAlreadyExists(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = PlainInit(dir, true)
	c.Assert(err, Equals, ErrRepositoryAlreadyExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainOpen(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, false)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = PlainOpen(dir)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestPlainOpenBare(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = PlainOpen(dir)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestPlainOpenNotBare(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, false)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = PlainOpen(filepath.Join(dir, ".git"))
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) testPlainOpenGitFile(c *C, f func(string, string) string) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir, err := util.TempDir(fs, "", "plain-open")
	c.Assert(err, IsNil)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	altDir, err := util.TempDir(fs, "", "plain-open")
	c.Assert(err, IsNil)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		[]byte(f(fs.Join(fs.Root(), dir), fs.Join(fs.Root(), altDir))),
		0644,
	)

	c.Assert(err, IsNil)

	r, err = PlainOpen(fs.Join(fs.Root(), altDir))
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}

func (s *RepositorySuite) TestPlainOpenBareAbsoluteGitDirFile(c *C) {
	s.testPlainOpenGitFile(c, func(dir, altDir string) string {
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareAbsoluteGitDirFileNoEOL(c *C) {
	s.testPlainOpenGitFile(c, func(dir, altDir string) string {
		return fmt.Sprintf("gitdir: %s", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFile(c *C) {
	s.testPlainOpenGitFile(c, func(dir, altDir string) string {
		dir, err := filepath.Rel(altDir, dir)
		c.Assert(err, IsNil)
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileNoEOL(c *C) {
	s.testPlainOpenGitFile(c, func(dir, altDir string) string {
		dir, err := filepath.Rel(altDir, dir)
		c.Assert(err, IsNil)
		return fmt.Sprintf("gitdir: %s\n", dir)
	})
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileTrailingGarbage(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	r, err := PlainInit(dir, true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	altDir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"),
		[]byte(fmt.Sprintf("gitdir: %s\nTRAILING", fs.Join(fs.Root(), altDir))),
		0644,
	)
	c.Assert(err, IsNil)

	r, err = PlainOpen(altDir)
	c.Assert(err, Equals, ErrRepositoryNotExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainOpenBareRelativeGitDirFileBadPrefix(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	r, err := PlainInit(fs.Join(fs.Root(), dir), true)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	altDir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	err = util.WriteFile(fs, fs.Join(altDir, ".git"), []byte(
		fmt.Sprintf("xgitdir: %s\n", fs.Join(fs.Root(), dir)),
	), 0644)

	c.Assert(err, IsNil)

	r, err = PlainOpen(fs.Join(fs.Root(), altDir))
	c.Assert(err, ErrorMatches, ".*gitdir.*")
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainOpenNotExists(c *C) {
	r, err := PlainOpen("/not-exists/")
	c.Assert(err, Equals, ErrRepositoryNotExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainOpenDetectDotGit(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	subdir := filepath.Join(dir, "a", "b")
	err = fs.MkdirAll(subdir, 0755)
	c.Assert(err, IsNil)

	file := fs.Join(subdir, "file.txt")
	f, err := fs.Create(file)
	c.Assert(err, IsNil)
	f.Close()

	r, err := PlainInit(fs.Join(fs.Root(), dir), false)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	opt := &PlainOpenOptions{DetectDotGit: true}
	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), subdir), opt)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), file), opt)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	optnodetect := &PlainOpenOptions{DetectDotGit: false}
	r, err = PlainOpenWithOptions(fs.Join(fs.Root(), file), optnodetect)
	c.Assert(err, NotNil)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainOpenNotExistsDetectDotGit(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	opt := &PlainOpenOptions{DetectDotGit: true}
	r, err := PlainOpenWithOptions(dir, opt)
	c.Assert(err, Equals, ErrRepositoryNotExists)
	c.Assert(r, IsNil)
}

func (s *RepositorySuite) TestPlainClone(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, false, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 1)
	c.Assert(cfg.Branches["master"].Name, Equals, "master")
}

func (s *RepositorySuite) TestPlainCloneWithRemoteName(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, false, &CloneOptions{
		URL:        s.GetBasicLocalRepositoryURL(),
		RemoteName: "test",
	})

	c.Assert(err, IsNil)

	remote, err := r.Remote("test")
	c.Assert(err, IsNil)
	c.Assert(remote, NotNil)
}

func (s *RepositorySuite) TestPlainCloneOverExistingGitDirectory(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, false)
	c.Assert(r, NotNil)
	c.Assert(err, IsNil)

	r, err = PlainClone(dir, false, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(r, IsNil)
	c.Assert(err, Equals, ErrRepositoryAlreadyExists)
}

func (s *RepositorySuite) TestPlainCloneContextCancel(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainCloneContext(ctx, dir, false, &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(r, NotNil)
	c.Assert(err, Equals, context.Canceled)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithExistentDir(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	r, err := PlainCloneContext(ctx, dir, false, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	c.Assert(r, NotNil)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)

	_, err = fs.Stat(dir)
	c.Assert(os.IsNotExist(err), Equals, false)

	names, err := fs.ReadDir(dir)
	c.Assert(err, IsNil)
	c.Assert(names, HasLen, 0)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithNonExistentDir(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs, clean := s.TemporalFilesystem()
	defer clean()

	tmpDir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	repoDir := filepath.Join(tmpDir, "repoDir")

	r, err := PlainCloneContext(ctx, repoDir, false, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	c.Assert(r, NotNil)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)

	_, err = fs.Stat(repoDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithNotDir(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs, clean := s.TemporalFilesystem()
	defer clean()

	tmpDir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	repoDir := fs.Join(tmpDir, "repoDir")

	f, err := fs.Create(repoDir)
	c.Assert(err, IsNil)
	c.Assert(f.Close(), IsNil)

	r, err := PlainCloneContext(ctx, fs.Join(fs.Root(), repoDir), false, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	c.Assert(r, IsNil)
	c.Assert(err, ErrorMatches, ".*not a directory.*")

	fi, err := fs.Stat(repoDir)
	c.Assert(err, IsNil)
	c.Assert(fi.IsDir(), Equals, false)
}

func (s *RepositorySuite) TestPlainCloneContextNonExistentWithNotEmptyDir(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fs, clean := s.TemporalFilesystem()
	defer clean()

	tmpDir, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	repoDir := filepath.Join(tmpDir, "repoDir")
	err = fs.MkdirAll(repoDir, 0777)
	c.Assert(err, IsNil)

	dummyFile := filepath.Join(repoDir, "dummyFile")
	err = util.WriteFile(fs, dummyFile, []byte("dummyContent"), 0644)
	c.Assert(err, IsNil)

	r, err := PlainCloneContext(ctx, fs.Join(fs.Root(), repoDir), false, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	c.Assert(r, NotNil)
	c.Assert(err, Equals, transport.ErrRepositoryNotFound)

	_, err = fs.Stat(dummyFile)
	c.Assert(err, IsNil)

}

func (s *RepositorySuite) TestPlainCloneContextNonExistingOverExistingGitDirectory(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainInit(dir, false)
	c.Assert(r, NotNil)
	c.Assert(err, IsNil)

	r, err = PlainCloneContext(ctx, dir, false, &CloneOptions{
		URL: "incorrectOnPurpose",
	})
	c.Assert(r, IsNil)
	c.Assert(err, Equals, ErrRepositoryAlreadyExists)
}

func (s *RepositorySuite) TestPlainCloneWithRecurseSubmodules(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	dir, clean := s.TemporalDir()
	defer clean()

	path := fixtures.ByTag("submodule").One().Worktree().Root()
	r, err := PlainClone(dir, false, &CloneOptions{
		URL:               path,
		RecurseSubmodules: DefaultSubmoduleRecursionDepth,
	})

	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Remotes, HasLen, 1)
	c.Assert(cfg.Branches, HasLen, 1)
	c.Assert(cfg.Submodules, HasLen, 2)
}

func (s *RepositorySuite) TestPlainCloneNoCheckout(c *C) {
	dir, clean := s.TemporalDir()
	defer clean()

	path := fixtures.ByTag("submodule").One().Worktree().Root()
	r, err := PlainClone(dir, false, &CloneOptions{
		URL:               path,
		NoCheckout:        true,
		RecurseSubmodules: DefaultSubmoduleRecursionDepth,
	})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)
	c.Assert(h.Hash().String(), Equals, "b685400c1f9316f350965a5993d350bc746b0bf4")

	fi, err := osfs.New(dir).ReadDir("")
	c.Assert(err, IsNil)
	c.Assert(fi, HasLen, 1) // .git
}

func (s *RepositorySuite) TestFetch(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	c.Assert(err, IsNil)
	c.Assert(r.Fetch(&FetchOptions{}), IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	_, err = r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)

	branch, err := r.Reference("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestFetchContext(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.Assert(r.FetchContext(ctx, &FetchOptions{}), NotNil)
}

func (s *RepositorySuite) TestCloneWithProgress(c *C) {
	fs := memfs.New()

	buf := bytes.NewBuffer(nil)
	_, err := Clone(memory.NewStorage(), fs, &CloneOptions{
		URL:      s.GetBasicLocalRepositoryURL(),
		Progress: buf,
	})

	c.Assert(err, IsNil)
	c.Assert(buf.Len(), Not(Equals), 0)
}

func (s *RepositorySuite) TestCloneDeep(c *C) {
	fs := memfs.New()
	r, _ := Init(memory.NewStorage(), fs)

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	head, err = r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Reference(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Reference("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	fi, err := fs.ReadDir("")
	c.Assert(err, IsNil)
	c.Assert(fi, HasLen, 8)
}

func (s *RepositorySuite) TestCloneConfig(c *C) {
	r, _ := Init(memory.NewStorage(), nil)

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)

	c.Assert(cfg.Core.IsBare, Equals, true)
	c.Assert(cfg.Remotes, HasLen, 1)
	c.Assert(cfg.Remotes["origin"].Name, Equals, "origin")
	c.Assert(cfg.Remotes["origin"].URLs, HasLen, 1)
	c.Assert(cfg.Branches, HasLen, 1)
	c.Assert(cfg.Branches["master"].Name, Equals, "master")
}

func (s *RepositorySuite) TestCloneSingleBranchAndNonHEAD(c *C) {
	s.testCloneSingleBranchAndNonHEADReference(c, "refs/heads/branch")
}

func (s *RepositorySuite) TestCloneSingleBranchAndNonHEADAndNonFull(c *C) {
	s.testCloneSingleBranchAndNonHEADReference(c, "branch")
}

func (s *RepositorySuite) testCloneSingleBranchAndNonHEADReference(c *C, ref string) {
	r, _ := Init(memory.NewStorage(), nil)

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 1)
	c.Assert(cfg.Branches["branch"].Name, Equals, "branch")
	c.Assert(cfg.Branches["branch"].Remote, Equals, "origin")
	c.Assert(cfg.Branches["branch"].Merge, Equals, plumbing.ReferenceName("refs/heads/branch"))

	head, err = r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/branch")

	branch, err := r.Reference(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	branch, err = r.Reference("refs/remotes/origin/branch", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *RepositorySuite) TestCloneSingleBranch(c *C) {
	r, _ := Init(memory.NewStorage(), nil)

	head, err := r.Head()
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
	c.Assert(head, IsNil)

	err = r.clone(context.Background(), &CloneOptions{
		URL:          s.GetBasicLocalRepositoryURL(),
		SingleBranch: true,
	})

	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 1)
	c.Assert(cfg.Branches["master"].Name, Equals, "master")
	c.Assert(cfg.Branches["master"].Remote, Equals, "origin")
	c.Assert(cfg.Branches["master"].Merge, Equals, plumbing.ReferenceName("refs/heads/master"))

	head, err = r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(head.Target().String(), Equals, "refs/heads/master")

	branch, err := r.Reference(head.Target(), false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	branch, err = r.Reference("refs/remotes/origin/master", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)
	c.Assert(branch.Type(), Equals, plumbing.HashReference)
	c.Assert(branch.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
}

func (s *RepositorySuite) TestCloneSingleTag(c *C) {
	r, _ := Init(memory.NewStorage(), nil)

	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	err := r.clone(context.Background(), &CloneOptions{
		URL:           url,
		SingleBranch:  true,
		ReferenceName: plumbing.ReferenceName("refs/tags/commit-tag"),
	})
	c.Assert(err, IsNil)

	branch, err := r.Reference("refs/tags/commit-tag", false)
	c.Assert(err, IsNil)
	c.Assert(branch, NotNil)

	conf, err := r.Config()
	c.Assert(err, IsNil)
	originRemote := conf.Remotes["origin"]
	c.Assert(originRemote, NotNil)
	c.Assert(originRemote.Fetch, HasLen, 1)
	c.Assert(originRemote.Fetch[0].String(), Equals, "+refs/tags/commit-tag:refs/tags/commit-tag")
}

func (s *RepositorySuite) TestCloneDetachedHEAD(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
	})
	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.HashReference)
	c.Assert(head.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	count := 0
	objects, err := r.Objects()
	c.Assert(err, IsNil)
	objects.ForEach(func(object.Object) error { count++; return nil })
	c.Assert(count, Equals, 28)
}

func (s *RepositorySuite) TestCloneDetachedHEADAndSingle(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
		SingleBranch:  true,
	})
	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.HashReference)
	c.Assert(head.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	count := 0
	objects, err := r.Objects()
	c.Assert(err, IsNil)
	objects.ForEach(func(object.Object) error { count++; return nil })
	c.Assert(count, Equals, 28)
}

func (s *RepositorySuite) TestCloneDetachedHEADAndShallow(c *C) {
	r, _ := Init(memory.NewStorage(), memfs.New())
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetBasicLocalRepositoryURL(),
		ReferenceName: plumbing.ReferenceName("refs/tags/v1.0.0"),
		Depth:         1,
	})

	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.HashReference)
	c.Assert(head.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	count := 0
	objects, err := r.Objects()
	c.Assert(err, IsNil)
	objects.ForEach(func(object.Object) error { count++; return nil })
	c.Assert(count, Equals, 15)
}

func (s *RepositorySuite) TestCloneDetachedHEADAnnotatedTag(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL:           s.GetLocalRepositoryURL(fixtures.ByTag("tags").One()),
		ReferenceName: plumbing.ReferenceName("refs/tags/annotated-tag"),
	})
	c.Assert(err, IsNil)

	cfg, err := r.Config()
	c.Assert(err, IsNil)
	c.Assert(cfg.Branches, HasLen, 0)

	head, err := r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(head, NotNil)
	c.Assert(head.Type(), Equals, plumbing.HashReference)
	c.Assert(head.Hash().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")

	count := 0
	objects, err := r.Objects()
	c.Assert(err, IsNil)
	objects.ForEach(func(object.Object) error { count++; return nil })
	c.Assert(count, Equals, 7)
}

func (s *RepositorySuite) TestPush(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainInit(url, true)
	c.Assert(err, IsNil)

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "test",
		URLs: []string{url},
	})
	c.Assert(err, IsNil)

	err = s.Repository.Push(&PushOptions{
		RemoteName: "test",
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})

	AssertReferences(c, s.Repository, map[string]string{
		"refs/remotes/test/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/test/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})
}

func (s *RepositorySuite) TestPushContext(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainInit(url, true)
	c.Assert(err, IsNil)

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "foo",
		URLs: []string{url},
	})
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = s.Repository.PushContext(ctx, &PushOptions{
		RemoteName: "foo",
	})
	c.Assert(err, NotNil)
}

// installPreReceiveHook installs a pre-receive hook in the .git
// directory at path which prints message m before exiting
// successfully.
func installPreReceiveHook(c *C, fs billy.Filesystem, path, m string) {
	hooks := fs.Join(path, "hooks")
	err := fs.MkdirAll(hooks, 0777)
	c.Assert(err, IsNil)

	err = util.WriteFile(fs, fs.Join(hooks, "pre-receive"), preReceiveHook(m), 0777)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestPushWithProgress(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	path, err := util.TempDir(fs, "", "")
	c.Assert(err, IsNil)

	url := fs.Join(fs.Root(), path)

	server, err := PlainInit(url, true)
	c.Assert(err, IsNil)

	m := "Receiving..."
	installPreReceiveHook(c, fs, path, m)

	_, err = s.Repository.CreateRemote(&config.RemoteConfig{
		Name: "bar",
		URLs: []string{url},
	})
	c.Assert(err, IsNil)

	var p bytes.Buffer
	err = s.Repository.Push(&PushOptions{
		RemoteName: "bar",
		Progress:   &p,
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/branch": "e8d3ffab552895c19b9fcf7aa264d277cde33881",
	})

	c.Assert((&p).Bytes(), DeepEquals, []byte(m))
}

func (s *RepositorySuite) TestPushDepth(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainClone(url, true, &CloneOptions{
		URL: fixtures.Basic().One().DotGit().Root(),
	})

	c.Assert(err, IsNil)

	r, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL:   url,
		Depth: 1,
	})
	c.Assert(err, IsNil)

	err = util.WriteFile(r.wt, "foo", nil, 0755)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	hash, err := w.Commit("foo", &CommitOptions{
		Author:    defaultSignature(),
		Committer: defaultSignature(),
	})
	c.Assert(err, IsNil)

	err = r.Push(&PushOptions{})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/master": hash.String(),
	})

	AssertReferences(c, r, map[string]string{
		"refs/remotes/origin/master": hash.String(),
	})
}

func (s *RepositorySuite) TestPushNonExistentRemote(c *C) {
	srcFs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	r, err := Open(sto, srcFs)
	c.Assert(err, IsNil)

	err = r.Push(&PushOptions{RemoteName: "myremote"})
	c.Assert(err, ErrorMatches, ".*remote not found.*")
}

func (s *RepositorySuite) TestLog(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	cIter, err := r.Log(&LogOptions{
		From: plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	})

	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogAll(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	rIter, err := r.Storer.IterReferences()
	c.Assert(err, IsNil)

	refCount := 0
	err = rIter.ForEach(func(ref *plumbing.Reference) error {
		refCount++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(refCount, Equals, 5)

	cIter, err := r.Log(&LogOptions{
		All: true,
	})
	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogAllMissingReferences(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)
	err = r.Storer.RemoveReference(plumbing.HEAD)
	c.Assert(err, IsNil)

	rIter, err := r.Storer.IterReferences()
	c.Assert(err, IsNil)

	refCount := 0
	err = rIter.ForEach(func(ref *plumbing.Reference) error {
		refCount++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(refCount, Equals, 4)

	err = r.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("DUMMY"), plumbing.NewHash("DUMMY")))
	c.Assert(err, IsNil)

	rIter, err = r.Storer.IterReferences()
	c.Assert(err, IsNil)

	refCount = 0
	err = rIter.ForEach(func(ref *plumbing.Reference) error {
		refCount++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(refCount, Equals, 5)

	cIter, err := r.Log(&LogOptions{
		All: true,
	})
	c.Assert(cIter, NotNil)
	c.Assert(err, IsNil)

	cCount := 0
	cIter.ForEach(func(c *object.Commit) error {
		cCount++
		return nil
	})
	c.Assert(cCount, Equals, 9)

	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogAllOrderByTime(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		All:   true,
	})
	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
	cIter.Close()
}

func (s *RepositorySuite) TestLogHead(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	cIter, err := r.Log(&LogOptions{})

	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		plumbing.NewHash("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"),
		plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogError(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	_, err = r.Log(&LogOptions{
		From: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestLogFileNext(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	fileName := "vendor/foo.go"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})

	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogFileForEach(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	fileName := "php/crappy.php"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		c.Assert(commit.Hash.String(), Equals, expectedCommitHash.String())
		expectedIndex++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(expectedIndex, Equals, 1)
}

func (s *RepositorySuite) TestLogNonHeadFile(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	fileName := "README"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	c.Assert(err, IsNil)
	defer cIter.Close()

	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogAllFileForEach(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	fileName := "README"
	cIter, err := r.Log(&LogOptions{FileName: &fileName, All: true})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		c.Assert(commit.Hash.String(), Equals, expectedCommitHash.String())
		expectedIndex++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(expectedIndex, Equals, 1)
}

func (s *RepositorySuite) TestLogInvalidFile(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	// Throwing in a file that does not exist
	fileName := "vendor/foo12.go"
	cIter, err := r.Log(&LogOptions{FileName: &fileName})
	// Not raising an error since `git log -- vendor/foo12.go` responds silently
	c.Assert(err, IsNil)
	defer cIter.Close()

	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogFileInitialCommit(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	fileName := "LICENSE"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
	})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		c.Assert(commit.Hash.String(), Equals, expectedCommitHash.String())
		expectedIndex++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(expectedIndex, Equals, 1)
}

func (s *RepositorySuite) TestLogFileWithOtherParamsFail(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	fileName := "vendor/foo.go"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
		From:     plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	c.Assert(err, IsNil)
	defer cIter.Close()

	_, iterErr := cIter.Next()
	c.Assert(iterErr, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogFileWithOtherParamsPass(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	fileName := "LICENSE"
	cIter, err := r.Log(&LogOptions{
		Order:    LogOrderCommitterTime,
		FileName: &fileName,
		From:     plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	c.Assert(err, IsNil)
	commitVal, iterErr := cIter.Next()
	c.Assert(iterErr, Equals, nil)
	c.Assert(commitVal.Hash.String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")

	_, iterErr = cIter.Next()
	c.Assert(iterErr, Equals, io.EOF)
}

type mockErrCommitIter struct{}

func (m *mockErrCommitIter) Next() (*object.Commit, error) {
	return nil, errors.New("mock next error")
}
func (m *mockErrCommitIter) ForEach(func(*object.Commit) error) error {
	return errors.New("mock foreach error")
}

func (m *mockErrCommitIter) Close() {}

func (s *RepositorySuite) TestLogFileWithError(c *C) {
	fileName := "README"
	cIter := object.NewCommitFileIterFromIter(fileName, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(commit *object.Commit) error {
		return nil
	})
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestLogPathWithError(c *C) {
	fileName := "README"
	pathIter := func(path string) bool {
		return path == fileName
	}
	cIter := object.NewCommitPathIterFromIter(pathIter, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(commit *object.Commit) error {
		return nil
	})
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestLogPathRegexpWithError(c *C) {
	pathRE := regexp.MustCompile("R.*E")
	pathIter := func(path string) bool {
		return pathRE.MatchString(path)
	}
	cIter := object.NewCommitPathIterFromIter(pathIter, &mockErrCommitIter{}, false)
	defer cIter.Close()

	err := cIter.ForEach(func(commit *object.Commit) error {
		return nil
	})
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestLogPathFilterRegexp(c *C) {
	pathRE := regexp.MustCompile(`.*\.go`)
	pathIter := func(path string) bool {
		return pathRE.MatchString(path)
	}

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	expectedCommitIDs := []string{
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"918c48b83bd081e863dbe1b80f8998f058cd8294",
	}
	commitIDs := []string{}

	cIter, err := r.Log(&LogOptions{
		PathFilter: pathIter,
		From:       plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	c.Assert(err, IsNil)
	defer cIter.Close()

	cIter.ForEach(func(commit *object.Commit) error {
		commitIDs = append(commitIDs, commit.ID().String())
		return nil
	})
	c.Assert(
		strings.Join(commitIDs, ", "),
		Equals,
		strings.Join(expectedCommitIDs, ", "),
	)
}

func (s *RepositorySuite) TestLogLimitNext(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	since := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since})

	c.Assert(err, IsNil)

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	for _, o := range commitOrder {
		commit, err := cIter.Next()
		c.Assert(err, IsNil)
		c.Assert(commit.Hash, Equals, o)
	}
	_, err = cIter.Next()
	c.Assert(err, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogLimitForEach(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	until := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since, Until: &until})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		c.Assert(commit.Hash.String(), Equals, expectedCommitHash.String())
		expectedIndex++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(expectedIndex, Equals, 1)
}

func (s *RepositorySuite) TestLogAllLimitForEach(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	until := time.Date(2015, 4, 1, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{Since: &since, Until: &until, All: true})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitOrder := []plumbing.Hash{
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
	}

	expectedIndex := 0
	err = cIter.ForEach(func(commit *object.Commit) error {
		expectedCommitHash := commitOrder[expectedIndex]
		c.Assert(commit.Hash.String(), Equals, expectedCommitHash.String())
		expectedIndex++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(expectedIndex, Equals, 2)
}

func (s *RepositorySuite) TestLogLimitWithOtherParamsFail(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	since := time.Date(2015, 3, 31, 11, 54, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		Since: &since,
		From:  plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	c.Assert(err, IsNil)
	defer cIter.Close()

	_, iterErr := cIter.Next()
	c.Assert(iterErr, Equals, io.EOF)
}

func (s *RepositorySuite) TestLogLimitWithOtherParamsPass(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	until := time.Date(2015, 3, 31, 11, 43, 0, 0, time.UTC)
	cIter, err := r.Log(&LogOptions{
		Order: LogOrderCommitterTime,
		Until: &until,
		From:  plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
	c.Assert(err, IsNil)
	defer cIter.Close()

	commitVal, iterErr := cIter.Next()
	c.Assert(iterErr, Equals, nil)
	c.Assert(commitVal.Hash.String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")

	_, iterErr = cIter.Next()
	c.Assert(iterErr, Equals, io.EOF)
}

func (s *RepositorySuite) TestConfigScoped(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	cfg, err := r.ConfigScoped(config.LocalScope)
	c.Assert(err, IsNil)
	c.Assert(cfg.User.Email, Equals, "")

	cfg, err = r.ConfigScoped(config.SystemScope)
	c.Assert(err, IsNil)
	c.Assert(cfg.User.Email, Not(Equals), "")
}

func (s *RepositorySuite) TestCommit(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	hash := plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47")
	commit, err := r.CommitObject(hash)
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
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	count := 0
	commits, err := r.CommitObjects()
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

func (s *RepositorySuite) TestBlob(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})

	c.Assert(err, IsNil)

	blob, err := r.BlobObject(plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	c.Assert(err, NotNil)
	c.Assert(blob, IsNil)

	blobHash := plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err = r.BlobObject(blobHash)
	c.Assert(err, IsNil)

	c.Assert(blob.Hash.IsZero(), Equals, false)
	c.Assert(blob.Hash, Equals, blob.ID())
	c.Assert(blob.Hash, Equals, blobHash)
	c.Assert(blob.Type(), Equals, plumbing.BlobObject)
}

func (s *RepositorySuite) TestBlobs(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	count := 0
	blobs, err := r.BlobObjects()
	c.Assert(err, IsNil)
	for {
		blob, err := blobs.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(blob.Hash.IsZero(), Equals, false)
		c.Assert(blob.Hash, Equals, blob.ID())
		c.Assert(blob.Type(), Equals, plumbing.BlobObject)
	}

	c.Assert(count, Equals, 10)
}

func (s *RepositorySuite) TestTagObject(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc")
	tag, err := r.TagObject(hash)
	c.Assert(err, IsNil)

	c.Assert(tag.Hash.IsZero(), Equals, false)
	c.Assert(tag.Hash, Equals, hash)
	c.Assert(tag.Type(), Equals, plumbing.TagObject)
}

func (s *RepositorySuite) TestTags(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	count := 0
	tags, err := r.Tags()
	c.Assert(err, IsNil)

	tags.ForEach(func(tag *plumbing.Reference) error {
		count++
		c.Assert(tag.Hash().IsZero(), Equals, false)
		c.Assert(tag.Name().IsTag(), Equals, true)
		return nil
	})

	c.Assert(count, Equals, 5)
}

func (s *RepositorySuite) TestCreateTagLightweight(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	expected, err := r.Head()
	c.Assert(err, IsNil)

	ref, err := r.CreateTag("foobar", expected.Hash(), nil)
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)

	actual, err := r.Tag("foobar")
	c.Assert(err, IsNil)

	c.Assert(expected.Hash(), Equals, actual.Hash())
}

func (s *RepositorySuite) TestCreateTagLightweightExists(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	expected, err := r.Head()
	c.Assert(err, IsNil)

	ref, err := r.CreateTag("lightweight-tag", expected.Hash(), nil)
	c.Assert(ref, IsNil)
	c.Assert(err, Equals, ErrTagExists)
}

func (s *RepositorySuite) TestCreateTagAnnotated(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	c.Assert(err, IsNil)

	tag, err := r.Tag("foobar")
	c.Assert(err, IsNil)

	obj, err := r.TagObject(tag.Hash())
	c.Assert(err, IsNil)

	c.Assert(ref, DeepEquals, tag)
	c.Assert(obj.Hash, Equals, ref.Hash())
	c.Assert(obj.Type(), Equals, plumbing.TagObject)
	c.Assert(obj.Target, Equals, expectedHash)
}

func (s *RepositorySuite) TestCreateTagAnnotatedBadOpts(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{})
	c.Assert(ref, IsNil)
	c.Assert(err, Equals, ErrMissingMessage)
}

func (s *RepositorySuite) TestCreateTagAnnotatedBadHash(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	ref, err := r.CreateTag("foobar", plumbing.ZeroHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	c.Assert(ref, IsNil)
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestCreateTagSigned(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)

	key := commitSignKey(c, true)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
		SignKey: key,
	})
	c.Assert(err, IsNil)

	tag, err := r.Tag("foobar")
	c.Assert(err, IsNil)

	obj, err := r.TagObject(tag.Hash())
	c.Assert(err, IsNil)

	// Verify the tag.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	c.Assert(err, IsNil)

	err = key.Serialize(pkw)
	c.Assert(err, IsNil)
	err = pkw.Close()
	c.Assert(err, IsNil)

	actual, err := obj.Verify(pks.String())
	c.Assert(err, IsNil)
	c.Assert(actual.PrimaryKey, DeepEquals, key.PrimaryKey)
}

func (s *RepositorySuite) TestCreateTagSignedBadKey(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)

	key := commitSignKey(c, false)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
		SignKey: key,
	})
	c.Assert(err, Equals, openpgperr.InvalidArgumentError("signing key is encrypted"))
}

func (s *RepositorySuite) TestCreateTagCanonicalize(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	h, err := r.Head()
	c.Assert(err, IsNil)

	key := commitSignKey(c, true)
	_, err = r.CreateTag("foobar", h.Hash(), &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "\n\nfoo bar baz qux\n\nsome message here",
		SignKey: key,
	})
	c.Assert(err, IsNil)

	tag, err := r.Tag("foobar")
	c.Assert(err, IsNil)

	obj, err := r.TagObject(tag.Hash())
	c.Assert(err, IsNil)

	// Assert the new canonicalized message.
	c.Assert(obj.Message, Equals, "foo bar baz qux\n\nsome message here\n")

	// Verify the tag.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	c.Assert(err, IsNil)

	err = key.Serialize(pkw)
	c.Assert(err, IsNil)
	err = pkw.Close()
	c.Assert(err, IsNil)

	actual, err := obj.Verify(pks.String())
	c.Assert(err, IsNil)
	c.Assert(actual.PrimaryKey, DeepEquals, key.PrimaryKey)
}

func (s *RepositorySuite) TestTagLightweight(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	expected := plumbing.NewHash("f7b877701fbf855b44c0a9e86f3fdce2c298b07f")

	tag, err := r.Tag("lightweight-tag")
	c.Assert(err, IsNil)

	actual := tag.Hash()
	c.Assert(expected, Equals, actual)
}

func (s *RepositorySuite) TestTagLightweightMissingTag(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	tag, err := r.Tag("lightweight-tag-tag")
	c.Assert(tag, IsNil)
	c.Assert(err, Equals, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTag(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	err = r.DeleteTag("lightweight-tag")
	c.Assert(err, IsNil)

	_, err = r.Tag("lightweight-tag")
	c.Assert(err, Equals, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTagMissingTag(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	err = r.DeleteTag("lightweight-tag-tag")
	c.Assert(err, Equals, ErrTagNotFound)
}

func (s *RepositorySuite) TestDeleteTagAnnotated(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	fs, clean := s.TemporalFilesystem()
	defer clean()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, _ := Init(fss, nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	ref, err := r.Tag("annotated-tag")
	c.Assert(ref, NotNil)
	c.Assert(err, IsNil)

	obj, err := r.TagObject(ref.Hash())
	c.Assert(obj, NotNil)
	c.Assert(err, IsNil)

	err = r.DeleteTag("annotated-tag")
	c.Assert(err, IsNil)

	_, err = r.Tag("annotated-tag")
	c.Assert(err, Equals, ErrTagNotFound)

	// Run a prune (and repack, to ensure that we are GCing everything regardless
	// of the fixture in use) and try to get the tag object again.
	//
	// The repo needs to be re-opened after the repack.
	err = r.Prune(PruneOptions{Handler: r.DeleteObject})
	c.Assert(err, IsNil)

	err = r.RepackObjects(&RepackConfig{})
	c.Assert(err, IsNil)

	r, err = PlainOpen(fs.Root())
	c.Assert(r, NotNil)
	c.Assert(err, IsNil)

	// Now check to see if the GC was effective in removing the tag object.
	obj, err = r.TagObject(ref.Hash())
	c.Assert(obj, IsNil)
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestDeleteTagAnnotatedUnpacked(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	fs, clean := s.TemporalFilesystem()
	defer clean()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, _ := Init(fss, nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	// Create a tag for the deletion test. This ensures that the ultimate loose
	// object will be unpacked (as we aren't doing anything that should pack it),
	// so that we can effectively test that a prune deletes it, without having to
	// resort to a repack.
	h, err := r.Head()
	c.Assert(err, IsNil)

	expectedHash := h.Hash()

	ref, err := r.CreateTag("foobar", expectedHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "foo bar baz qux",
	})
	c.Assert(err, IsNil)

	tag, err := r.Tag("foobar")
	c.Assert(err, IsNil)

	obj, err := r.TagObject(tag.Hash())
	c.Assert(obj, NotNil)
	c.Assert(err, IsNil)

	err = r.DeleteTag("foobar")
	c.Assert(err, IsNil)

	_, err = r.Tag("foobar")
	c.Assert(err, Equals, ErrTagNotFound)

	// As mentioned, only run a prune. We are not testing for packed objects
	// here.
	err = r.Prune(PruneOptions{Handler: r.DeleteObject})
	c.Assert(err, IsNil)

	// Now check to see if the GC was effective in removing the tag object.
	obj, err = r.TagObject(ref.Hash())
	c.Assert(obj, IsNil)
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *RepositorySuite) TestBranches(c *C) {
	f := fixtures.ByURL("https://github.com/git-fixtures/root-references.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	c.Assert(err, IsNil)

	count := 0
	branches, err := r.Branches()
	c.Assert(err, IsNil)

	branches.ForEach(func(branch *plumbing.Reference) error {
		count++
		c.Assert(branch.Hash().IsZero(), Equals, false)
		c.Assert(branch.Name().IsBranch(), Equals, true)
		return nil
	})

	c.Assert(count, Equals, 8)
}

func (s *RepositorySuite) TestNotes(c *C) {
	// TODO add fixture with Notes
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	count := 0
	notes, err := r.Notes()
	c.Assert(err, IsNil)

	notes.ForEach(func(note *plumbing.Reference) error {
		count++
		c.Assert(note.Hash().IsZero(), Equals, false)
		c.Assert(note.Name().IsNote(), Equals, true)
		return nil
	})

	c.Assert(count, Equals, 0)
}

func (s *RepositorySuite) TestTree(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{
		URL: s.GetBasicLocalRepositoryURL(),
	})
	c.Assert(err, IsNil)

	invalidHash := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	tree, err := r.TreeObject(invalidHash)
	c.Assert(tree, IsNil)
	c.Assert(err, NotNil)

	hash := plumbing.NewHash("dbd3641b371024f44d0e469a9c8f5457b0660de1")
	tree, err = r.TreeObject(hash)
	c.Assert(err, IsNil)

	c.Assert(tree.Hash.IsZero(), Equals, false)
	c.Assert(tree.Hash, Equals, tree.ID())
	c.Assert(tree.Hash, Equals, hash)
	c.Assert(tree.Type(), Equals, plumbing.TreeObject)
	c.Assert(len(tree.Entries), Not(Equals), 0)
}

func (s *RepositorySuite) TestTrees(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	count := 0
	trees, err := r.TreeObjects()
	c.Assert(err, IsNil)
	for {
		tree, err := trees.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(tree.Hash.IsZero(), Equals, false)
		c.Assert(tree.Hash, Equals, tree.ID())
		c.Assert(tree.Type(), Equals, plumbing.TreeObject)
		c.Assert(len(tree.Entries), Not(Equals), 0)
	}

	c.Assert(count, Equals, 12)
}

func (s *RepositorySuite) TestTagObjects(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/tags.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	count := 0
	tags, err := r.TagObjects()
	c.Assert(err, IsNil)

	tags.ForEach(func(tag *object.Tag) error {
		count++

		c.Assert(tag.Hash.IsZero(), Equals, false)
		c.Assert(tag.Type(), Equals, plumbing.TagObject)
		return nil
	})

	refs, _ := r.References()
	refs.ForEach(func(ref *plumbing.Reference) error {
		return nil
	})

	c.Assert(count, Equals, 4)
}

func (s *RepositorySuite) TestCommitIterClosePanic(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	commits, err := r.CommitObjects()
	c.Assert(err, IsNil)
	commits.Close()
}

func (s *RepositorySuite) TestRef(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.HEAD)

	ref, err = r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.ReferenceName("refs/heads/master"))
}

func (s *RepositorySuite) TestRefs(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)

	iter, err := r.References()
	c.Assert(err, IsNil)
	c.Assert(iter, NotNil)
}

func (s *RepositorySuite) TestObject(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	o, err := r.Object(plumbing.CommitObject, hash)
	c.Assert(err, IsNil)

	c.Assert(o.ID().IsZero(), Equals, false)
	c.Assert(o.Type(), Equals, plumbing.CommitObject)
}

func (s *RepositorySuite) TestObjects(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	count := 0
	objects, err := r.Objects()
	c.Assert(err, IsNil)
	for {
		o, err := objects.Next()
		if err != nil {
			break
		}

		count++
		c.Assert(o.ID().IsZero(), Equals, false)
		c.Assert(o.Type(), Not(Equals), plumbing.AnyObject)
	}

	c.Assert(count, Equals, 31)
}

func (s *RepositorySuite) TestObjectNotFound(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	c.Assert(err, IsNil)

	hash := plumbing.NewHash("0a3fb06ff80156fb153bcdcc58b5e16c2d27625c")
	tag, err := r.Object(plumbing.TagObject, hash)
	c.Assert(err, DeepEquals, plumbing.ErrObjectNotFound)
	c.Assert(tag, IsNil)
}

func (s *RepositorySuite) TestWorktree(c *C) {
	def := memfs.New()
	r, _ := Init(memory.NewStorage(), def)
	w, err := r.Worktree()
	c.Assert(err, IsNil)
	c.Assert(w.Filesystem, Equals, def)
}

func (s *RepositorySuite) TestWorktreeBare(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	w, err := r.Worktree()
	c.Assert(err, Equals, ErrIsBareRepository)
	c.Assert(w, IsNil)
}

func (s *RepositorySuite) TestResolveRevision(c *C) {
	f := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	c.Assert(err, IsNil)

	datas := map[string]string{
		"HEAD":                       "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"heads/master":               "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"heads/master~1":             "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"refs/heads/master":          "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/heads/master~2^^~":     "b029517f6300c2da0f4b651b8642506cd6aaf45d",
		"refs/tags/v1.0.0":           "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/origin/master": "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"refs/remotes/origin/HEAD":   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"HEAD~2^^~":                  "b029517f6300c2da0f4b651b8642506cd6aaf45d",
		"HEAD~3^2":                   "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"HEAD~3^2^0":                 "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"HEAD~2^{/binary file}":      "35e85108805c84807bc66a02d91535e1e24b38b9",
		"HEAD~^{/!-some}":            "1669dce138d9b841a518c64b10914d88f5e488ea",
		"master":                     "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"branch":                     "e8d3ffab552895c19b9fcf7aa264d277cde33881",
		"v1.0.0":                     "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"branch~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"v1.0.0~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"master~1":                   "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"918c48b83bd081e863dbe1b80f8998f058cd8294": "918c48b83bd081e863dbe1b80f8998f058cd8294",
		"918c48b": "918c48b83bd081e863dbe1b80f8998f058cd8294", // odd number of hex digits
	}

	for rev, hash := range datas {
		h, err := r.ResolveRevision(plumbing.Revision(rev))

		c.Assert(err, IsNil, Commentf("while checking %s", rev))
		c.Check(h.String(), Equals, hash, Commentf("while checking %s", rev))
	}
}

func (s *RepositorySuite) TestResolveRevisionAnnotated(c *C) {
	f := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
	r, err := Open(sto, f.DotGit())
	c.Assert(err, IsNil)

	datas := map[string]string{
		"refs/tags/annotated-tag":                  "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		"b742a2a9fa0afcfa9a6fad080980fbc26b007c69": "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
	}

	for rev, hash := range datas {
		h, err := r.ResolveRevision(plumbing.Revision(rev))

		c.Assert(err, IsNil, Commentf("while checking %s", rev))
		c.Check(h.String(), Equals, hash, Commentf("while checking %s", rev))
	}
}

func (s *RepositorySuite) TestResolveRevisionWithErrors(c *C) {
	url := s.GetLocalRepositoryURL(
		fixtures.ByURL("https://github.com/git-fixtures/basic.git").One(),
	)

	r, _ := Init(memory.NewStorage(), nil)
	err := r.clone(context.Background(), &CloneOptions{URL: url})
	c.Assert(err, IsNil)

	headRef, err := r.Head()
	c.Assert(err, IsNil)

	ref := plumbing.NewHashReference("refs/heads/918c48b83bd081e863dbe1b80f8998f058cd8294", headRef.Hash())
	err = r.Storer.SetReference(ref)
	c.Assert(err, IsNil)

	datas := map[string]string{
		"efs/heads/master~": "reference not found",
		"HEAD^3":            `Revision invalid : "3" found must be 0, 1 or 2 after "^"`,
		"HEAD^{/whatever}":  `no commit message match regexp: "whatever"`,
		"4e1243bd22c66e76c2ba9eddc1f91394e57f9f83": "reference not found",
	}

	for rev, rerr := range datas {
		_, err := r.ResolveRevision(plumbing.Revision(rev))
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, rerr)
	}
}

func (s *RepositorySuite) testRepackObjects(
	c *C, deleteTime time.Time, expectedPacks int) {
	srcFs := fixtures.ByTag("unpacked").One().DotGit()
	var sto storage.Storer
	var err error
	sto = filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	los := sto.(storer.LooseObjectStorer)
	c.Assert(los, NotNil)

	numLooseStart := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		numLooseStart++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(numLooseStart > 0, Equals, true)

	pos := sto.(storer.PackedObjectStorer)
	c.Assert(los, NotNil)

	packs, err := pos.ObjectPacks()
	c.Assert(err, IsNil)
	numPacksStart := len(packs)
	c.Assert(numPacksStart > 1, Equals, true)

	r, err := Open(sto, srcFs)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	err = r.RepackObjects(&RepackConfig{
		OnlyDeletePacksOlderThan: deleteTime,
	})
	c.Assert(err, IsNil)

	numLooseEnd := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		numLooseEnd++
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(numLooseEnd, Equals, 0)

	packs, err = pos.ObjectPacks()
	c.Assert(err, IsNil)
	numPacksEnd := len(packs)
	c.Assert(numPacksEnd, Equals, expectedPacks)
}

func (s *RepositorySuite) TestRepackObjects(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	s.testRepackObjects(c, time.Time{}, 1)
}

func (s *RepositorySuite) TestRepackObjectsWithNoDelete(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	s.testRepackObjects(c, time.Unix(0, 1), 3)
}

func ExecuteOnPath(c *C, path string, cmds ...string) error {
	for _, cmd := range cmds {
		err := executeOnPath(path, cmd)
		c.Assert(err, IsNil)
	}

	return nil
}

func executeOnPath(path, cmd string) error {
	args := strings.Split(cmd, " ")
	c := exec.Command(args[0], args[1:]...)
	c.Dir = path
	c.Env = os.Environ()

	buf := bytes.NewBuffer(nil)
	c.Stderr = buf
	c.Stdout = buf

	return c.Run()
}

func (s *RepositorySuite) TestBrokenMultipleShallowFetch(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})
	c.Assert(err, IsNil)

	c.Assert(r.Fetch(&FetchOptions{
		Depth:    2,
		RefSpecs: []config.RefSpec{config.RefSpec("refs/heads/master:refs/heads/master")},
	}), IsNil)

	shallows, err := r.Storer.Shallow()
	c.Assert(err, IsNil)
	c.Assert(len(shallows), Equals, 1)

	ref, err := r.Reference("refs/heads/master", true)
	c.Assert(err, IsNil)
	cobj, err := r.CommitObject(ref.Hash())
	c.Assert(err, IsNil)
	c.Assert(cobj, NotNil)
	err = object.NewCommitPreorderIter(cobj, nil, nil).ForEach(func(c *object.Commit) error {
		for _, ph := range c.ParentHashes {
			for _, h := range shallows {
				if ph == h {
					return storer.ErrStop
				}
			}
		}

		return nil
	})
	c.Assert(err, IsNil)

	c.Assert(r.Fetch(&FetchOptions{
		Depth:    5,
		RefSpecs: []config.RefSpec{config.RefSpec("refs/heads/*:refs/heads/*")},
	}), IsNil)

	shallows, err = r.Storer.Shallow()
	c.Assert(err, IsNil)
	c.Assert(len(shallows), Equals, 3)

	ref, err = r.Reference("refs/heads/master", true)
	c.Assert(err, IsNil)
	cobj, err = r.CommitObject(ref.Hash())
	c.Assert(err, IsNil)
	c.Assert(cobj, NotNil)
	err = object.NewCommitPreorderIter(cobj, nil, nil).ForEach(func(c *object.Commit) error {
		for _, ph := range c.ParentHashes {
			for _, h := range shallows {
				if ph == h {
					return storer.ErrStop
				}
			}
		}

		return nil
	})
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDotGitToOSFilesystemsInvalidPath(c *C) {
	_, _, err := dotGitToOSFilesystems("\000", false)
	c.Assert(err, NotNil)
}

func (s *RepositorySuite) TestIssue674(c *C) {
	r, _ := Init(memory.NewStorage(), nil)
	h, err := r.ResolveRevision(plumbing.Revision(""))

	c.Assert(err, NotNil)
	c.Assert(h, NotNil)
	c.Check(h.IsZero(), Equals, true)
}

func BenchmarkObjects(b *testing.B) {
	defer fixtures.Clean()

	for _, f := range fixtures.ByTag("packfile") {
		if f.DotGitHash == "" {
			continue
		}

		b.Run(f.URL, func(b *testing.B) {
			fs := f.DotGit()
			st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

			worktree, err := fs.Chroot(filepath.Dir(fs.Root()))
			if err != nil {
				b.Fatal(err)
			}

			repo, err := Open(st, worktree)
			if err != nil {
				b.Fatal(err)
			}

			for i := 0; i < b.N; i++ {
				iter, err := repo.Objects()
				if err != nil {
					b.Fatal(err)
				}

				for {
					_, err := iter.Next()
					if err == io.EOF {
						break
					}

					if err != nil {
						b.Fatal(err)
					}
				}

				iter.Close()
			}
		})
	}
}

func BenchmarkPlainClone(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t, err := os.MkdirTemp("", "")
		if err != nil {
			b.Fatal(err)
		}
		_, err = PlainClone(t, false, &CloneOptions{
			URL:   "https://github.com/knqyf263/vuln-list",
			Depth: 1,
		})
		if err != nil {
			b.Error(err)
		}
		b.StopTimer()
		os.RemoveAll(t)
		b.StartTimer()
	}
}
