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
