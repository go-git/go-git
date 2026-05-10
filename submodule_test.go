package git

import (
	"context"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestSubmoduleInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))

		require.False(t, sm.initialized)
		require.NoError(t, sm.Init())
		require.True(t, sm.initialized)

		cfg, err := r.Config()
		require.NoError(t, err)

		require.Len(t, cfg.Submodules, 1)
		require.NotNil(t, cfg.Submodules[primaryFixtureSubmoduleName(f)])

		status, err := sm.Status()
		require.NoError(t, err)
		require.False(t, status.IsClean())
	})
}

func TestSubmoduleUpdate(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		if testing.Short() {
			t.Skip("skipping test in short mode.")
		}

		r, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Update(&SubmoduleUpdateOptions{Init: true}))

		subRepo, err := sm.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		ref, err := subRepo.Reference(plumbing.HEAD, true)
		require.NoError(t, err)
		require.Equal(t, submoduleHashFromIndex(t, r, primaryFixtureSubmoduleName(f)), ref.Hash())

		status, err := sm.Status()
		require.NoError(t, err)
		require.True(t, status.IsClean())
	})
}

func TestSubmoduleRepositoryWithoutInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))

		r, err := sm.Repository()
		require.ErrorIs(t, err, ErrSubmoduleNotInitialized)
		require.Nil(t, r)
	})
}

func TestSubmoduleUpdateWithoutInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		err := sm.Update(&SubmoduleUpdateOptions{})
		require.ErrorIs(t, err, ErrSubmoduleNotInitialized)
	})
}

func TestSubmoduleUpdateWithNotFetch(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		err := sm.Update(&SubmoduleUpdateOptions{
			Init:    true,
			NoFetch: true,
		})

		require.ErrorIs(t, err, plumbing.ErrObjectNotFound)
	})
}

func TestSubmoduleUpdateWithRecursion(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		if testing.Short() {
			t.Skip("skipping test in short mode.")
		}

		_, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, "itself")
		err := sm.Update(&SubmoduleUpdateOptions{
			Init:              true,
			RecurseSubmodules: 2,
		})
		require.NoError(t, err)

		fs := wt.filesystem
		_, err = fs.Stat(fs.Join("itself", primaryFixtureSubmoduleName(f), "LICENSE"))
		require.NoError(t, err)
	})
}

func TestSubmoduleUpdateWithInitAndUpdate(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		if testing.Short() {
			t.Skip("skipping test in short mode.")
		}

		r, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Update(&SubmoduleUpdateOptions{Init: true}))

		subRepo, err := sm.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		log, err := subRepo.Log(&LogOptions{})
		require.NoError(t, err)

		_, err = log.Next()
		require.NoError(t, err)

		previousCommit, err := log.Next()
		require.NoError(t, err)

		subWorktree, err := subRepo.Worktree()
		require.NoError(t, err)
		require.NoError(t, subWorktree.Reset(&ResetOptions{Mode: HardReset}))

		idx, err := r.Storer.Index()
		require.NoError(t, err)

		previousHash := previousCommit.Hash
		for i, entry := range idx.Entries {
			if entry.Name == primaryFixtureSubmoduleName(f) {
				entry.Hash = previousHash
				idx.Entries[i] = entry
			}
		}

		require.NoError(t, r.Storer.SetIndex(idx))
		require.NoError(t, sm.Update(&SubmoduleUpdateOptions{}))

		ref, err := subRepo.Reference(plumbing.HEAD, true)
		require.NoError(t, err)
		require.Equal(t, previousHash, ref.Hash())
	})
}

func TestSubmodulesInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		sm, err := wt.Submodules()
		require.NoError(t, err)
		require.NoError(t, sm.Init())

		sm, err = wt.Submodules()
		require.NoError(t, err)

		for _, m := range sm {
			require.True(t, m.initialized)
		}
	})
}

func TestGitSubmodulesSymlink(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		// Plant the malicious symlink directly on the inner filesystem.
		// The worktreeFilesystem wrapper's Symlink rejects .gitmodules
		// link names by design (see validSymlinkName); the read-side
		// detection in readGitmodulesFile is the layer being exercised
		// here, so the setup goes through the unwrapped billy.Filesystem.
		fs := wt.Filesystem()
		file, err := fs.Create("badfile")
		require.NoError(t, err)
		require.NoError(t, file.Close())

		require.NoError(t, fs.Remove(gitmodulesFile))
		require.NoError(t, fs.Symlink("badfile", gitmodulesFile))

		_, err = wt.Submodules()
		require.ErrorIs(t, err, ErrGitModulesSymlink)
	})
}

func TestSubmodulesStatus(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		_, wt := cloneFixture(t, f)

		sm, err := wt.Submodules()
		require.NoError(t, err)

		status, err := sm.Status()
		require.NoError(t, err)
		require.Len(t, status, 2)
	})
}

func TestSubmodulesUpdateContext(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		if testing.Short() {
			t.Skip("skipping test in short mode.")
		}

		_, wt := cloneFixture(t, f)

		sm, err := wt.Submodules()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = sm.UpdateContext(ctx, &SubmoduleUpdateOptions{Init: true})
		require.Error(t, err)
	})
}

func TestSubmodulesFetchDepth(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		if testing.Short() {
			t.Skip("skipping test in short mode.")
		}

		if f.ObjectFormat == "sha256" {
			t.Skip("shallow submodule updates do not yet support SHA-256 shallow-update parsing")
		}

		_, wt := cloneFixture(t, f)

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Update(&SubmoduleUpdateOptions{
			Init:  true,
			Depth: 1,
		}))

		subRepo, err := sm.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		lr, err := subRepo.Log(&LogOptions{})
		require.NoError(t, err)

		commitCount := 0
		for _, err := lr.Next(); err == nil; _, err = lr.Next() {
			commitCount++
		}
		require.NoError(t, err)
		require.Equal(t, 1, commitCount)
	})
}

func TestSubmoduleParseScp(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, _ *fixtures.Fixture) {
		t.Parallel()

		repo := &Repository{
			Storer: memory.NewStorage(),
			wt:     memfs.New(),
		}
		worktree := &Worktree{
			filesystem: newWorktreeFilesystem(memfs.New(), true, true),
			r:          repo,
		}
		submodule := &Submodule{
			initialized: true,
			w:           worktree,
		}

		submodule.c = &config.Submodule{
			Name: "submodule_repo",
			Path: "deps/submodule_repo",
			URL:  "git@github.com:username/submodule_repo",
		}

		_, err := submodule.Repository()
		require.NoError(t, err)
	})
}

func TestDefaultRemote(t *testing.T) {
	t.Parallel()

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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := &Repository{Storer: memory.NewStorage()}
			cfg, err := r.Config()
			require.NoError(t, err)
			for name, url := range tc.remotes {
				cfg.Remotes[name] = &config.RemoteConfig{
					Name: name,
					URLs: []string{url},
				}
			}
			for name, remote := range tc.branches {
				cfg.Branches[name] = &config.Branch{Name: name, Remote: remote}
			}
			require.NoError(t, r.Storer.SetConfig(cfg))

			if tc.head != nil {
				require.NoError(t, r.Storer.SetReference(tc.head))
			}

			got, err := defaultRemote(r)
			if tc.wantErrIn != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErrIn)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Name)
		})
	}
}

func TestSubmoduleRelativeURLPicksOrigin(t *testing.T) {
	t.Parallel()

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
		require.NoError(t, err)
		cfg.Remotes["origin"] = &config.RemoteConfig{
			Name: "origin",
			URLs: []string{"file:///parent/origin"},
		}
		cfg.Remotes["upstream"] = &config.RemoteConfig{
			Name: "upstream",
			URLs: []string{"file:///parent/upstream"},
		}
		require.NoError(t, parent.Storer.SetConfig(cfg))

		sub := &Submodule{
			initialized: true,
			w:           &Worktree{filesystem: newWorktreeFilesystem(memfs.New(), true, true), r: parent},
			c: &config.Submodule{
				Name: "child",
				Path: "child",
				URL:  "../child",
			},
		}

		subRepo, err := sub.Repository()
		require.NoError(t, err, "iteration %d", i)

		remotes, err := subRepo.Remotes()
		require.NoError(t, err)
		require.Len(t, remotes, 1, "iteration %d", i)
		require.Equal(t,
			"file:///parent/child",
			remotes[0].Config().URLs[0],
			"iteration %d: expected URL resolved against origin", i,
		)
	}
}

func TestSubmoduleRelativeURLRemoteWithoutURLs(t *testing.T) {
	t.Parallel()

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
	require.NoError(t, err)
	cfg.Remotes["origin"] = &config.RemoteConfig{Name: "origin", URLs: nil}

	sub := &Submodule{
		initialized: true,
		w:           &Worktree{filesystem: newWorktreeFilesystem(memfs.New(), defaultProtectNTFS(), defaultProtectHFS()), r: parent},
		c: &config.Submodule{
			Name: "child",
			Path: "child",
			URL:  "../child",
		},
	}

	subRepo, err := sub.Repository()
	require.Error(t, err)
	require.Nil(t, subRepo)
	require.ErrorContains(t, err, `remote "origin" has no configured URL`)
}

func submoduleHashFromIndex(t *testing.T, r *Repository, name string) plumbing.Hash {
	t.Helper()

	idx, err := r.Storer.Index()
	require.NoError(t, err)

	for _, entry := range idx.Entries {
		if entry.Name == name {
			return entry.Hash
		}
	}

	t.Fatalf("submodule %q not found in index", name)
	return plumbing.ZeroHash
}

func primaryFixtureSubmoduleName(f *fixtures.Fixture) string {
	if f.ObjectFormat == "sha256" {
		return "sha256-basic"
	}

	return "basic"
}

// newSubmoduleForRelativeURL constructs an in-memory Repository with
// the given parent remote URL configured as origin, plus a Submodule
// whose configured URL is the given submoduleURL. Pass parentRemoteURL
// = "" to omit the origin remote entirely.
func newSubmoduleForRelativeURL(t *testing.T, parentRemoteURL, submoduleName, submoduleURL string) *Submodule {
	t.Helper()

	repo := &Repository{
		Storer: memory.NewStorage(),
		wt:     memfs.New(),
	}
	if parentRemoteURL != "" {
		_, err := repo.CreateRemote(&config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{parentRemoteURL},
		})
		require.NoError(t, err)
	}
	worktree := &Worktree{
		filesystem: newWorktreeFilesystem(memfs.New(), true, true),
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

func TestSubmoduleRepositoryURLResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		parentURL    string
		submoduleURL string
		wantRemote   string
		wantErr      string
	}{
		{
			name:         "relative URL against HTTPS parent",
			parentURL:    "https://example.invalid/group/proj.git",
			submoduleURL: "../X.git",
			wantRemote:   "https://example.invalid/group/X.git",
		},
		{
			name:         "relative URL against SSH parent",
			parentURL:    "ssh://git@example.invalid/group/proj.git",
			submoduleURL: "../X.git",
			wantRemote:   "ssh://git@example.invalid/group/X.git",
		},
		{
			name:         "relative URL with deep traversal",
			parentURL:    "https://example.invalid/group/proj.git",
			submoduleURL: "../../org/X.git",
			wantRemote:   "https://example.invalid/org/X.git",
		},
		{
			name:         "absolute local URL preserved",
			submoduleURL: "/abs/path/X.git",
			wantRemote:   "file:///abs/path/X.git",
		},
		{
			name:         "relative URL with no parent remote",
			submoduleURL: "../X.git",
			wantErr:      `resolving relative submodule URL: remote "origin" not found`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sm := newSubmoduleForRelativeURL(t, tc.parentURL, "basic", tc.submoduleURL)

			r, err := sm.Repository()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			remotes, err := r.Remotes()
			require.NoError(t, err)
			require.Len(t, remotes, 1)
			require.Equal(t, tc.wantRemote, remotes[0].Config().URLs[0])
		})
	}
}

func (s *SubmoduleSuite) TestAdaptHashForSubmoduleWhenParentIsSHA256AndSubmoduleIsSHA1() {
	sha1Hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	paddedSha256Hash := sha1Hash + "000000000000000000000000"

	parentHash, ok := plumbing.FromHex(paddedSha256Hash)
	s.Require().True(ok)
	s.Equal(64, len(parentHash.String()), "Parent hash should be 64 chars (SHA-256)")

	submoduleRepo, err := Init(memory.NewStorage(), nil)
	s.Require().NoError(err)

	sm := &Submodule{
		initialized: true,
		c:           &config.Submodule{Name: "test"},
		w:           s.Worktree,
	}

	adaptedHash := sm.adaptHashForSubmodule(submoduleRepo, parentHash)

	s.Equal(40, len(adaptedHash.String()), "Adapted hash should be 40 chars (SHA-1)")
	s.Equal(sha1Hash, adaptedHash.String(), "Adapted hash should match original SHA-1 hash")
}

func (s *SubmoduleSuite) TestAdaptHashForSubmoduleWhenParentIsSHA1AndSubmoduleIsSHA1() {
	sha1Hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	parentHash, ok := plumbing.FromHex(sha1Hash)
	s.Require().True(ok)
	s.Equal(40, len(parentHash.String()), "Parent hash should be 40 chars (SHA-1)")

	submoduleRepo, err := Init(memory.NewStorage(), nil)
	s.Require().NoError(err)

	sm := &Submodule{
		initialized: true,
		c:           &config.Submodule{Name: "test"},
		w:           s.Worktree,
	}

	adaptedHash := sm.adaptHashForSubmodule(submoduleRepo, parentHash)

	s.Equal(40, len(adaptedHash.String()), "Adapted hash should remain 40 chars (SHA-1)")
	s.Equal(sha1Hash, adaptedHash.String(), "Hash should not be modified when formats match")
}

func (s *SubmoduleSuite) TestAdaptHashForSubmoduleWhenParentIsSHA256AndSubmoduleIsSHA256() {
	sha256Hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5a1b2c3d4e5f67890abcdef12"

	parentHash, ok := plumbing.FromHex(sha256Hash)
	s.Require().True(ok)
	s.Equal(64, len(parentHash.String()), "Parent hash should be 64 chars (SHA-256)")

	submoduleRepo, err := Init(memory.NewStorage(), nil)
	s.Require().NoError(err)

	cfg, err := submoduleRepo.Config()
	s.Require().NoError(err)
	cfg.Core.RepositoryFormatVersion = format.Version1
	cfg.Extensions.ObjectFormat = format.SHA256
	err = submoduleRepo.Storer.SetConfig(cfg)
	s.Require().NoError(err)

	sm := &Submodule{
		initialized: true,
		c:           &config.Submodule{Name: "test"},
		w:           s.Worktree,
	}

	adaptedHash := sm.adaptHashForSubmodule(submoduleRepo, parentHash)

	s.Equal(64, len(adaptedHash.String()), "Adapted hash should remain 64 chars (SHA-256)")
	s.Equal(sha256Hash, adaptedHash.String(), "Hash should not be truncated when submodule is SHA-256")
}
