package git

import (
	"os"
	"path/filepath"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

func TestSubmoduleRepositoryConfigIsIndependentFromParent(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		cfg, err := r.Config()
		require.NoError(t, err)
		cfg.User.Name = "repo-user"
		cfg.User.Email = "repo@example.com"
		require.NoError(t, r.SetConfig(cfg))

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Init())

		subRepo, err := sm.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		subCfg, err := subRepo.Config()
		require.NoError(t, err)
		assert.Empty(t, subCfg.User.Name)
		assert.Empty(t, subCfg.User.Email)
	})
}

func TestSubmoduleRepositoryConfigPersistsObjectFormatOnReopen(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Init())

		subRepo, err := sm.Repository()
		require.NoError(t, err)

		cfg, err := subRepo.Config()
		require.NoError(t, err)
		require.NoError(t, subRepo.Close())

		reopened, err := sm.Repository()
		require.NoError(t, err)
		defer reopened.Close()

		reopenedCfg, err := reopened.Config()
		require.NoError(t, err)
		assert.Equal(t, cfg.Core.RepositoryFormatVersion, reopenedCfg.Core.RepositoryFormatVersion)
		assert.Equal(t, cfg.Extensions.ObjectFormat, reopenedCfg.Extensions.ObjectFormat)
	})
}

func TestSubmoduleRepositoryCreateRemoteWritesModuleConfig(t *testing.T) {
	t.Parallel()

	fixtures.ByTag("submodule").Run(t, func(t *testing.T, f *fixtures.Fixture) {
		t.Parallel()

		r, wt := cloneFixture(t, f)
		defer func() { _ = r.Close() }()

		sm := namedSubmodule(t, wt, primaryFixtureSubmoduleName(f))
		require.NoError(t, sm.Init())

		subRepo, err := sm.Repository()
		require.NoError(t, err)
		defer subRepo.Close()

		_, err = subRepo.CreateRemote(&config.RemoteConfig{
			Name: "module-only",
			URLs: []string{"https://example.com/submodule.git"},
		})
		require.NoError(t, err)

		parentCfg, err := r.Config()
		require.NoError(t, err)
		_, ok := parentCfg.Remotes["module-only"]
		assert.False(t, ok)

		repoPath := wt.Filesystem().Root()
		moduleCfg := readConfigFile(t, filepath.Join(repoPath, ".git", "modules", sm.c.Name, "config"))
		require.Contains(t, moduleCfg.Remotes, "module-only")
		assert.Equal(t, []string{"https://example.com/submodule.git"}, moduleCfg.Remotes["module-only"].URLs)
	})
}

func cloneFixture(t *testing.T, f *fixtures.Fixture) (*Repository, *Worktree) {
	t.Helper()

	dotgit, err := f.DotGit(fixtures.WithTargetDir(t.TempDir))
	require.NoError(t, err)

	r, err := PlainClone(t.TempDir(), &CloneOptions{URL: dotgit.Root()})
	require.NoError(t, err)

	wt, err := r.Worktree()
	require.NoError(t, err)

	return r, wt
}

func namedSubmodule(t *testing.T, wt *Worktree, name string) *Submodule {
	t.Helper()

	sm, err := wt.Submodule(name)
	require.NoError(t, err)
	return sm
}

func readConfigFile(t *testing.T, path string) *config.Config {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	cfg, err := config.ReadConfig(file)
	require.NoError(t, err)

	return cfg
}
