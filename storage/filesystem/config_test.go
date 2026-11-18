package filesystem

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

type ConfigSuite struct {
	suite.Suite

	dir  *dotgit.DotGit
	path string
}

func TestConfigSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ConfigSuite))
}

func (s *ConfigSuite) SetupTest() {
	tmp := s.T().TempDir()
	s.dir = dotgit.New(osfs.New(tmp))
	s.path = tmp
}

func (s *ConfigSuite) TestRemotes() {
	dotgitFs, err := fixtures.Basic().ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	dir := dotgit.New(dotgitFs)
	storer := &ConfigStorage{dir: dir}

	cfg, err := storer.Config()
	s.Require().NoError(err)

	remotes := cfg.Remotes
	s.Len(remotes, 1)
	remote := remotes["origin"]
	s.Equal("origin", remote.Name)
	s.Equal([]string{"https://github.com/git-fixtures/basic"}, remote.URLs)
	s.Equal([]config.RefSpec{config.RefSpec("+refs/heads/*:refs/remotes/origin/*")}, remote.Fetch)
}

func (s *ConfigSuite) TearDownTest() {
	defer os.RemoveAll(s.path)
}

// newDualStorer builds a ConfigStorage backed by a RepositoryFilesystem that
// mirrors a linked-worktree layout:
//
//	commonFs  →  the shared .git/ directory  (holds "config", objects, …)
//	wtFs      →  .git/worktrees/<name>/       (holds "config.worktree", …)
//
// Path routing follows the same rules as a real linked worktree: "config"
// resolves to commonFs and "config.worktree" resolves to wtFs.
func newDualStorer(commonFs, wtFs billy.Filesystem) *ConfigStorage {
	repoFs := dotgit.NewRepositoryFilesystem(wtFs, commonFs)
	return &ConfigStorage{dir: dotgit.New(repoFs)}
}

func TestWorktreeConfigRead(t *testing.T) {
	t.Parallel()

	const baseConfig = "[core]\n\tbare = false\n\tfilemode = true\n" +
		"[extensions]\n\tworktreeConfig = true\n" +
		"[user]\n\tname = base-user\n\temail = base@example.com\n"
	const wtConfig = "[user]\n\tname = wt-user\n"

	t.Run("overlays config.worktree on top of base config", func(t *testing.T) {
		t.Parallel()

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(baseConfig), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(wtConfig), 0o644))

		cfg, err := newDualStorer(commonFs, wtFs).Config()
		require.NoError(t, err)

		assert.Equal(t, "wt-user", cfg.User.Name)
		assert.Equal(t, "base@example.com", cfg.User.Email)
		assert.True(t, cfg.Extensions.WorktreeConfig)
	})

	t.Run("config.worktree values merge with base config by key", func(t *testing.T) {
		t.Parallel()

		const base = baseConfig +
			"[remote \"origin\"]\n\turl = https://example.com/a.git\n" +
			"\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
		const wt = "[core]\n\tfilemode = false\n\tsparseCheckout = true\n" +
			"[remote \"origin\"]\n\tfetch = +refs/tags/*:refs/tags/*\n"

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(base), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(wt), 0o644))

		cfg, err := newDualStorer(commonFs, wtFs).Config()
		require.NoError(t, err)

		assert.False(t, cfg.Core.FileMode)
		require.NotNil(t, cfg.Raw)
		assert.Equal(t, "true", cfg.Raw.Section("core").Option("sparseCheckout"))
		require.Contains(t, cfg.Remotes, "origin")
		assert.Equal(t, []string{"https://example.com/a.git"}, cfg.Remotes["origin"].URLs)
		assert.Equal(t, []config.RefSpec{
			"+refs/heads/*:refs/remotes/origin/*",
			"+refs/tags/*:refs/tags/*",
		}, cfg.Remotes["origin"].Fetch)
	})

	t.Run("returns base config when config.worktree is absent", func(t *testing.T) {
		t.Parallel()

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(baseConfig), 0o644))

		cfg, err := newDualStorer(commonFs, wtFs).Config()
		require.NoError(t, err)

		assert.Equal(t, "base-user", cfg.User.Name)
		assert.Equal(t, "base@example.com", cfg.User.Email)
		assert.True(t, cfg.Extensions.WorktreeConfig)
	})

	t.Run("ignores config.worktree when extension is disabled", func(t *testing.T) {
		t.Parallel()

		const noExtBase = "[user]\n\tname = base-user\n"

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(noExtBase), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(wtConfig), 0o644))

		cfg, err := newDualStorer(commonFs, wtFs).Config()
		require.NoError(t, err)

		assert.Equal(t, "base-user", cfg.User.Name)
		assert.False(t, cfg.Extensions.WorktreeConfig)
	})
}

func TestWorktreeConfigSetConfig(t *testing.T) {
	t.Parallel()

	const baseConfig = "[core]\n\tbare = false\n\tfilemode = true\n" +
		"[extensions]\n\tworktreeConfig = true\n" +
		"[user]\n\tname = base-user\n\temail = base@example.com\n"

	t.Run("no config.worktree: SetConfig writes to base config", func(t *testing.T) {
		t.Parallel()

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(baseConfig), 0o644))

		cs := newDualStorer(commonFs, wtFs)
		cfg, err := cs.Config()
		require.NoError(t, err)

		cfg.User.Email = "written@example.com"
		require.NoError(t, cs.SetConfig(cfg))

		data, err := util.ReadFile(commonFs, "config")
		require.NoError(t, err)
		assert.Contains(t, string(data), "written@example.com")

		_, err = wtFs.Stat("config.worktree")
		assert.True(t, os.IsNotExist(err), "config.worktree should not be created by SetConfig")
	})

	t.Run("config.worktree exists: SetConfig writes only delta to config.worktree", func(t *testing.T) {
		t.Parallel()

		const existingWTConfig = "[core]\n\tworktree = /old/path\n"

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(baseConfig), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(existingWTConfig), 0o644))

		cs := newDualStorer(commonFs, wtFs)
		cfg, err := cs.Config()
		require.NoError(t, err)

		cfg.Core.Worktree = "/new/path"
		cfg.Author.Name = "Worktree Author"

		require.NoError(t, cs.SetConfig(cfg))

		data, err := util.ReadFile(wtFs, "config.worktree")
		require.NoError(t, err)
		wtContent := string(data)

		data, err = util.ReadFile(commonFs, "config")
		require.NoError(t, err)
		baseContent := string(data)

		assert.Contains(t, wtContent, "/new/path", "updated worktree path must be in config.worktree")
		assert.Contains(t, wtContent, "Worktree Author", "new author must be in config.worktree")
		assert.NotContains(t, wtContent, "base-user", "base user.name must not be duplicated in config.worktree")
		assert.NotContains(t, wtContent, "base@example.com", "base user.email must not be duplicated in config.worktree")

		assert.NotContains(t, baseContent, "/new/path", "worktree path must not be in config")
		assert.NotContains(t, baseContent, "Worktree Author", "new author must not be in config")
		assert.Contains(t, baseContent, "base-user", "base user.name must still be in config")
		assert.Contains(t, baseContent, "base@example.com", "base user.email must still be in config")
	})

	t.Run("config.worktree exists: round trip keeps multi-valued keys", func(t *testing.T) {
		t.Parallel()

		const base = baseConfig +
			"[remote \"origin\"]\n\turl = https://example.com/a.git\n\tfetch = +refs/heads/a:refs/a\n" +
			"[custom]\n\tmulti = a\n"
		const existingWTConfig = "[remote \"origin\"]\n" +
			"\tfetch = +refs/heads/b:refs/b\n\tfetch = +refs/heads/c:refs/c\n" +
			"[custom]\n\tmulti = b\n\tmulti = c\n"

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(base), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(existingWTConfig), 0o644))

		cs := newDualStorer(commonFs, wtFs)
		for range 2 {
			cfg, err := cs.Config()
			require.NoError(t, err)
			require.NoError(t, cs.SetConfig(cfg))
		}

		cfg, err := cs.Config()
		require.NoError(t, err)
		assert.Equal(t, []config.RefSpec{
			"+refs/heads/a:refs/a",
			"+refs/heads/b:refs/b",
			"+refs/heads/c:refs/c",
		}, cfg.Remotes["origin"].Fetch)
		assert.Equal(t, []string{"a", "b", "c"}, cfg.Raw.Section("custom").OptionAll("multi"))

		data, err := util.ReadFile(commonFs, "config")
		require.NoError(t, err)
		assert.Equal(t, base, string(data))
	})

	t.Run("config.worktree exists: base config is not modified", func(t *testing.T) {
		t.Parallel()

		const existingWTConfig = "[core]\n\tworktree = /old/path\n"

		commonFs, wtFs := memfs.New(), memfs.New()
		require.NoError(t, util.WriteFile(commonFs, "config", []byte(baseConfig), 0o644))
		require.NoError(t, util.WriteFile(wtFs, "config.worktree", []byte(existingWTConfig), 0o644))

		cs := newDualStorer(commonFs, wtFs)
		cfg, err := cs.Config()
		require.NoError(t, err)

		cfg.Core.Worktree = "/new/path"
		require.NoError(t, cs.SetConfig(cfg))

		data, err := util.ReadFile(commonFs, "config")
		require.NoError(t, err)

		assert.Equal(t, baseConfig, string(data))
	})
}

func TestSetConfigRefusedNameKeepsConfig(t *testing.T) {
	t.Parallel()

	const existing = "[core]\n\tbare = false\n"
	fs := memfs.New()
	require.NoError(t, util.WriteFile(fs, "config", []byte(existing), 0o644))

	cfg := config.NewConfig()
	cfg.Raw.AddOption("core", "", "fö", "1")

	err := NewStorage(fs, nil).SetConfig(cfg)
	require.ErrorIs(t, err, formatcfg.ErrInvalidKey)

	data, err := util.ReadFile(fs, "config")
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}
