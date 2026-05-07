package git

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type SubmoduleSuite struct {
	BaseSuite
	Worktree *Worktree
}

var _ = Suite(&SubmoduleSuite{})

func (s *SubmoduleSuite) SetUpTest(c *C) {
	path := fixtures.ByTag("submodule").One().Worktree().Root()

	dir := c.MkDir()

	r, err := PlainClone(filepath.Join(dir, "worktree"), false, &CloneOptions{
		URL: path,
	})

	c.Assert(err, IsNil)

	s.Repository = r
	s.Worktree, err = r.Worktree()
	c.Assert(err, IsNil)
}

func (s *SubmoduleSuite) TestInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	c.Assert(sm.initialized, Equals, false)
	err = sm.Init()
	c.Assert(err, IsNil)

	c.Assert(sm.initialized, Equals, true)

	cfg, err := s.Repository.Config()
	c.Assert(err, IsNil)

	c.Assert(cfg.Submodules, HasLen, 1)
	c.Assert(cfg.Submodules["basic"], NotNil)

	status, err := sm.Status()
	c.Assert(err, IsNil)
	c.Assert(status.IsClean(), Equals, false)
}

func (s *SubmoduleSuite) TestUpdate(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})

	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	status, err := sm.Status()
	c.Assert(err, IsNil)
	c.Assert(status.IsClean(), Equals, true)
}

func (s *SubmoduleSuite) TestRepositoryWithoutInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, Equals, ErrSubmoduleNotInitialized)
	c.Assert(r, IsNil)
}

func (s *SubmoduleSuite) TestUpdateWithoutInit(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{})
	c.Assert(err, Equals, ErrSubmoduleNotInitialized)
}

func (s *SubmoduleSuite) TestUpdateWithNotFetch(c *C) {
	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:    true,
		NoFetch: true,
	})

	// Since we are not fetching, the object is not there
	c.Assert(err, Equals, plumbing.ErrObjectNotFound)
}

func (s *SubmoduleSuite) TestUpdateWithRecursion(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("itself")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:              true,
		RecurseSubmodules: 2,
	})

	c.Assert(err, IsNil)

	fs := s.Worktree.Filesystem
	_, err = fs.Stat(fs.Join("itself", "basic", "LICENSE"))
	c.Assert(err, IsNil)
}

func (s *SubmoduleSuite) TestUpdateWithInitAndUpdate(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init: true,
	})
	c.Assert(err, IsNil)

	idx, err := s.Repository.Storer.Index()
	c.Assert(err, IsNil)

	for i, e := range idx.Entries {
		if e.Name == "basic" {
			e.Hash = plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d")
		}

		idx.Entries[i] = e
	}

	err = s.Repository.Storer.SetIndex(idx)
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{})
	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Hash().String(), Equals, "b029517f6300c2da0f4b651b8642506cd6aaf45d")

}

func (s *SubmoduleSuite) TestSubmodulesInit(c *C) {
	sm, err := s.Worktree.Submodules()
	c.Assert(err, IsNil)

	err = sm.Init()
	c.Assert(err, IsNil)

	sm, err = s.Worktree.Submodules()
	c.Assert(err, IsNil)

	for _, m := range sm {
		c.Assert(m.initialized, Equals, true)
	}
}

func (s *SubmoduleSuite) TestGitSubmodulesSymlink(c *C) {
	// Plant the malicious symlink directly on the inner filesystem.
	// The worktreeFilesystem wrapper's Symlink rejects .gitmodules
	// link names by design (see validSymlinkName); the read-side
	// detection in Submodules() is the layer being exercised here,
	// so the setup goes through the unwrapped billy.Filesystem.
	fs := s.Worktree.Filesystem
	if wfs, ok := fs.(*worktreeFilesystem); ok {
		fs = wfs.Filesystem
	}

	f, err := fs.Create("badfile")
	c.Assert(err, IsNil)
	defer func() { _ = f.Close() }()

	err = fs.Remove(gitmodulesFile)
	c.Assert(err, IsNil)

	err = fs.Symlink("badfile", gitmodulesFile)
	c.Assert(err, IsNil)

	_, err = s.Worktree.Submodules()
	c.Assert(err, Equals, ErrGitModulesSymlink)
}

func (s *SubmoduleSuite) TestSubmodulesStatus(c *C) {
	sm, err := s.Worktree.Submodules()
	c.Assert(err, IsNil)

	status, err := sm.Status()
	c.Assert(err, IsNil)
	c.Assert(status, HasLen, 2)
}

func (s *SubmoduleSuite) TestSubmodulesUpdateContext(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodules()
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = sm.UpdateContext(ctx, &SubmoduleUpdateOptions{Init: true})
	c.Assert(err, NotNil)
}

func (s *SubmoduleSuite) TestSubmodulesFetchDepth(c *C) {
	if testing.Short() {
		c.Skip("skipping test in short mode.")
	}

	sm, err := s.Worktree.Submodule("basic")
	c.Assert(err, IsNil)

	err = sm.Update(&SubmoduleUpdateOptions{
		Init:  true,
		Depth: 1,
	})
	c.Assert(err, IsNil)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	lr, err := r.Log(&LogOptions{})
	c.Assert(err, IsNil)

	commitCount := 0
	for _, err := lr.Next(); err == nil; _, err = lr.Next() {
		commitCount++
	}
	c.Assert(err, IsNil)

	c.Assert(commitCount, Equals, 1)
}

func (s *SubmoduleSuite) TestSubmoduleParseScp(c *C) {
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
		Path: "child",
		URL:  "git@github.com:username/submodule_repo",
	}

	_, err := submodule.Repository()
	c.Assert(err, IsNil)
}

// newSubmoduleForRelativeURL constructs an in-memory Repository with
// the given parent remote URL configured as origin, plus a Submodule
// whose configured URL is the given submoduleURL. Returns the
// submodule for direct Repository() invocation. Pass parentRemoteURL
// = "" to omit the origin remote entirely.
func newSubmoduleForRelativeURL(c *C, parentRemoteURL, submoduleName, submoduleURL string) *Submodule {
	repo := &Repository{
		Storer: memory.NewStorage(),
		wt:     memfs.New(),
	}
	if parentRemoteURL != "" {
		_, err := repo.CreateRemote(&config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{parentRemoteURL},
		})
		c.Assert(err, IsNil)
	}
	worktree := &Worktree{
		Filesystem: memfs.New(),
		r:          repo,
	}
	return &Submodule{
		initialized: true,
		c: &config.Submodule{
			Name: submoduleName,
			Path: submoduleName,
			URL:  submoduleURL,
		},
		w: worktree,
	}
}

func (s *SubmoduleSuite) TestRepositoryRelativeURLHTTPSParent(c *C) {
	sm := newSubmoduleForRelativeURL(c,
		"https://example.invalid/group/proj.git", "basic", "../X.git")

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
	c.Assert(remotes[0].Config().URLs[0], Equals,
		"https://example.invalid/group/X.git")
}

func (s *SubmoduleSuite) TestRepositoryRelativeURLSSHParent(c *C) {
	sm := newSubmoduleForRelativeURL(c,
		"ssh://git@example.invalid/group/proj.git", "basic", "../X.git")

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
	c.Assert(remotes[0].Config().URLs[0], Equals,
		"ssh://git@example.invalid/group/X.git")
}

func (s *SubmoduleSuite) TestRepositoryRelativeURLDeepTraversal(c *C) {
	sm := newSubmoduleForRelativeURL(c,
		"https://example.invalid/group/proj.git", "basic", "../../org/X.git")

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)
	c.Assert(remotes[0].Config().URLs[0], Equals,
		"https://example.invalid/org/X.git")
}

func (s *SubmoduleSuite) TestRepositoryAbsoluteLocalURLPreserved(c *C) {
	raw := "/abs/path/X.git"
	sm := newSubmoduleForRelativeURL(c, "", "basic", raw)

	r, err := sm.Repository()
	c.Assert(err, IsNil)

	remotes, err := r.Remotes()
	c.Assert(err, IsNil)
	c.Assert(remotes, HasLen, 1)

	// transport.NewEndpoint -> parseFile normalizes via filepath.Abs;
	// on Windows this prepends a drive letter. Mirror that here so the
	// assertion is portable. The point of this test is that the
	// relative-resolution branch is skipped for absolute inputs, not
	// the exact form parseFile produces.
	expected, err := filepath.Abs(raw)
	c.Assert(err, IsNil)
	c.Assert(remotes[0].Config().URLs[0], Equals, "file://"+expected)
}

func (s *SubmoduleSuite) TestRepositoryRelativeURLNoParentRemote(c *C) {
	sm := newSubmoduleForRelativeURL(c, "", "basic", "../X.git")

	_, err := sm.Repository()
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `resolving relative submodule URL: remote "origin" not found`)
}

func (s *SubmoduleSuite) TestDefaultRemote(c *C) {
	type testCase struct {
		name      string
		remotes   map[string]string // remote name → URL
		branches  map[string]string // branch name → branch.<name>.remote value
		head      *plumbing.Reference
		want      string // expected remote name
		wantErrIn string // substring required in error message; "" means no error
	}

	hashRef := plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash("0000000000000000000000000000000000000001"))
	mainSym := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	tagSym := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.ReferenceName("refs/tags/v1"))

	cases := []testCase{
		{
			name:     "branch-override-wins",
			remotes:  map[string]string{"origin": "file:///o", "upstream": "file:///u"},
			branches: map[string]string{"main": "upstream"},
			head:     mainSym,
			want:     "upstream",
		},
		{
			name:      "branch-override-with-bogus-remote",
			remotes:   map[string]string{"origin": "file:///o", "upstream": "file:///u"},
			branches:  map[string]string{"main": "bogus"},
			head:      mainSym,
			wantErrIn: `remote "bogus" not found`,
		},
		{
			name:    "single-remote-wins-over-origin-fallback",
			remotes: map[string]string{"upstream": "file:///u"},
			head:    hashRef,
			want:    "upstream",
		},
		{
			name:     "single-remote-with-empty-branch-remote",
			remotes:  map[string]string{"upstream": "file:///u"},
			branches: map[string]string{"main": ""},
			head:     mainSym,
			want:     "upstream",
		},
		{
			name:    "origin-fallback-among-multiple",
			remotes: map[string]string{"origin": "file:///o", "upstream": "file:///u"},
			head:    hashRef,
			want:    "origin",
		},
		{
			name:      "origin-fallback-not-present",
			remotes:   map[string]string{"upstream": "file:///u", "fork": "file:///f"},
			head:      hashRef,
			wantErrIn: `remote "origin" not found`,
		},
		{
			name:      "no-remotes",
			wantErrIn: `remote "origin" not found`,
		},
		{
			name:     "unborn-branch",
			remotes:  map[string]string{"origin": "file:///o", "upstream": "file:///u"},
			branches: map[string]string{"main": "upstream"},
			head:     mainSym,
			want:     "upstream",
		},
		{
			name:    "head-on-tag-falls-through",
			remotes: map[string]string{"origin": "file:///o", "upstream": "file:///u"},
			head:    tagSym,
			want:    "origin",
		},
	}

	for _, tc := range cases {
		c.Logf("case: %s", tc.name)

		r := &Repository{Storer: memory.NewStorage()}
		cfg, err := r.Config()
		c.Assert(err, IsNil)
		for name, url := range tc.remotes {
			cfg.Remotes[name] = &config.RemoteConfig{
				Name: name,
				URLs: []string{url},
			}
		}
		for name, remote := range tc.branches {
			cfg.Branches[name] = &config.Branch{Name: name, Remote: remote}
		}
		c.Assert(r.Storer.SetConfig(cfg), IsNil)

		if tc.head != nil {
			c.Assert(r.Storer.SetReference(tc.head), IsNil)
		}

		got, err := defaultRemote(r)
		if tc.wantErrIn != "" {
			c.Assert(err, NotNil)
			c.Assert(err, ErrorMatches, ".*"+tc.wantErrIn+".*")
			continue
		}
		c.Assert(err, IsNil)
		c.Assert(got.Name, Equals, tc.want)
	}
}

func (s *SubmoduleSuite) TestSubmoduleRelativeURLPicksOrigin(c *C) {
	// Two remotes plus a relative submodule URL. With the prior code,
	// remotes[0] from map iteration could be either origin or upstream;
	// the resolved submodule URL therefore differed across runs. Loop
	// 20× to exercise different map orderings within a single test run
	// — every iteration must resolve against origin.
	for i := range 20 {
		parent := &Repository{
			Storer: memory.NewStorage(),
			wt:     memfs.New(),
		}
		cfg, err := parent.Config()
		c.Assert(err, IsNil)
		cfg.Remotes["origin"] = &config.RemoteConfig{
			Name: "origin",
			URLs: []string{"file:///parent/origin"},
		}
		cfg.Remotes["upstream"] = &config.RemoteConfig{
			Name: "upstream",
			URLs: []string{"file:///parent/upstream"},
		}
		c.Assert(parent.Storer.SetConfig(cfg), IsNil)

		sub := &Submodule{
			initialized: true,
			w:           &Worktree{Filesystem: memfs.New(), r: parent},
			c: &config.Submodule{
				Name: "child",
				Path: "child",
				URL:  "../child",
			},
		}

		subRepo, err := sub.Repository()
		c.Assert(err, IsNil, Commentf("iteration %d", i))

		remotes, err := subRepo.Remotes()
		c.Assert(err, IsNil)
		c.Assert(remotes, HasLen, 1, Commentf("iteration %d", i))
		c.Assert(remotes[0].Config().URLs[0], Equals,
			"file:///parent/child",
			Commentf("iteration %d: expected URL resolved against origin", i))
	}
}

func (s *SubmoduleSuite) TestSubmoduleRelativeURLRemoteWithoutURLs(c *C) {
	// Defense in depth: a relative submodule URL must be joined onto
	// the chosen parent remote. If that remote has no configured URL,
	// earlier code panicked on `base.URLs[0]`. Mutating the in-memory
	// config directly bypasses SetConfig's validation, mirroring the
	// on-disk case where a `[remote "origin"]` section with no
	// `url =` entry could be loaded.
	parent := &Repository{
		Storer: memory.NewStorage(),
		wt:     memfs.New(),
	}
	cfg, err := parent.Config()
	c.Assert(err, IsNil)
	cfg.Remotes["origin"] = &config.RemoteConfig{Name: "origin", URLs: nil}

	sub := &Submodule{
		initialized: true,
		w:           &Worktree{Filesystem: memfs.New(), r: parent},
		c: &config.Submodule{
			Name: "child",
			Path: "child",
			URL:  "../child",
		},
	}

	subRepo, err := sub.Repository()
	c.Assert(err, NotNil)
	c.Assert(subRepo, IsNil)
	c.Assert(err, ErrorMatches,
		`resolving relative submodule URL: remote "origin" has no configured URL`)
}

// TestSubmoduleRepositoryRejectsEscapingName covers the storage-layer
// defence against submodule name path traversal. Constructing a
// Submodule with `Name = ".."` programmatically (bypassing the
// .gitmodules parser) must not result in `Repository()` opening a
// storer rooted in the parent's `.git/` directory, and must leave the
// parent's HEAD reference untouched.
func (s *SubmoduleSuite) TestSubmoduleRepositoryRejectsEscapingName(c *C) {
	dotfs := memfs.New()
	wtfs := memfs.New()
	// Use filesystem.NewStorage rather than memory.NewStorage for the
	// parent because only the filesystem-backed ModuleStorer exercises
	// the dotgit defence; the in-memory implementation maps names to
	// storage units directly without traversing a path.
	storer := filesystem.NewStorage(dotfs, cache.NewObjectLRUDefault())
	r, err := Init(storer, wtfs)
	c.Assert(err, IsNil)

	wt, err := r.Worktree()
	c.Assert(err, IsNil)

	headBefore, err := storer.Reference(plumbing.HEAD)
	c.Assert(err, IsNil)

	sm := &Submodule{
		initialized: true,
		c: &config.Submodule{
			Name: "..",
			Path: "deps/x",
			URL:  "https://example.com/",
		},
		w: wt,
	}

	repo, err := sm.Repository()
	c.Assert(err, NotNil)
	c.Assert(errors.Is(err, dotgit.ErrModuleNameEscape), Equals, true)
	c.Assert(repo, IsNil)

	headAfter, err := storer.Reference(plumbing.HEAD)
	c.Assert(err, IsNil)
	c.Assert(headBefore.Target(), Equals, headAfter.Target(),
		Commentf("parent HEAD must not be overwritten"))
}
