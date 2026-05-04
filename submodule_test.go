package git

import (
	"context"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestSubmoduleInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))

		subRepo, err := sm.Repository()
		require.ErrorIs(t, err, ErrSubmoduleNotInitialized)
		require.Nil(t, subRepo)
	})
}

func TestSubmoduleUpdateWithoutInit(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		err := sm.Update(&SubmoduleUpdateOptions{})
		require.ErrorIs(t, err, ErrSubmoduleNotInitialized)
	})
}

func TestSubmoduleUpdateWithNotFetch(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		sm := namedSubmodule(t, wt, "itself")
		err := sm.Update(&SubmoduleUpdateOptions{
			Init:              true,
			RecurseSubmodules: 2,
		})
		require.NoError(t, err)

		fs := wt.Filesystem
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
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		file, err := wt.Filesystem.Create("badfile")
		require.NoError(t, err)
		require.NoError(t, file.Close())

		require.NoError(t, wt.Filesystem.Remove(gitmodulesFile))
		require.NoError(t, wt.Filesystem.Symlink("badfile", gitmodulesFile))

		_, err = wt.Submodules()
		require.ErrorIs(t, err, ErrGitModulesSymlink)
	})
}

func TestSubmodulesStatus(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

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
			Filesystem: memfs.New(),
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

		subRepo, err := submodule.Repository()
		require.NoError(t, err)
		defer func() { _ = subRepo.Close() }()
	})
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
