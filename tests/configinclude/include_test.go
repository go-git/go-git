package configinclude_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// These tests run in their own package so that the plugin registry is
// fresh: the root package's tests register a static ConfigSource, while
// these need the default "auto" source that honours GIT_CONFIG_GLOBAL.

// includeFixture is a repository on disk together with the paths of the
// config files that surround it.
type includeFixture struct {
	repo   *git.Repository
	root   string
	gitDir string
	local  string
	global string
	branch string
}

// newIncludeFixture initialises a real on-disk repository and points the
// global and system scopes at files under a temporary directory, so the
// developer's own git configuration cannot influence the result.
func newIncludeFixture(t *testing.T) *includeFixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	worktree := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(worktree, 0o755))

	repo, err := git.PlainInit(worktree, false)
	require.NoError(t, err)

	gitDir := filepath.Join(worktree, ".git")
	global := filepath.Join(root, "global")

	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", "")

	head, err := repo.Storer.Reference(plumbing.HEAD)
	require.NoError(t, err)

	return &includeFixture{
		repo:   repo,
		root:   root,
		gitDir: gitDir,
		local:  filepath.Join(gitDir, "config"),
		global: global,
		branch: head.Target().Short(),
	}
}

func (f *includeFixture) write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// appendLocal adds to the repository's own config, keeping whatever
// PlainInit wrote.
func (f *includeFixture) appendLocal(t *testing.T, content string) {
	t.Helper()

	existing, err := os.ReadFile(f.local)
	require.NoError(t, err)
	f.write(t, f.local, string(existing)+content)
}

func slashPath(p string) string {
	return filepath.ToSlash(p)
}

//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedResolvesLocalInclude(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "extra"), "[user]\n\tname = FromInclude\n")
	f.appendLocal(t, "[include]\n\tpath = "+slashPath(filepath.Join(f.root, "extra"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.LocalScope)
	require.NoError(t, err)
	assert.Equal(t, "FromInclude", cfg.User.Name)
}

//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedResolvesGlobalInclude(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "extra"), "[user]\n\temail = inc@example.com\n")
	f.write(t, f.global, "[include]\n\tpath = "+slashPath(filepath.Join(f.root, "extra"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)
	assert.Equal(t, "inc@example.com", cfg.User.Email)
}

// A relative include path is resolved against the directory of the file
// that contains it, not the working directory.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedResolvesRelativeInclude(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.gitDir, "extra"), "[user]\n\tname = Relative\n")
	f.appendLocal(t, "[include]\n\tpath = extra\n")

	cfg, err := f.repo.ConfigScoped(config.LocalScope)
	require.NoError(t, err)
	assert.Equal(t, "Relative", cfg.User.Name)
}

// The whole point of resolving includes in place: an included value
// overrides one set before the directive, and loses to one set after it.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludePrecedenceIsPositional(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "extra"), "[user]\n\tname = FromInclude\n")
	include := "[include]\n\tpath = " + slashPath(filepath.Join(f.root, "extra")) + "\n"

	t.Run("include wins over earlier value", func(t *testing.T) {
		f.appendLocal(t, "[user]\n\tname = Before\n"+include)

		cfg, err := f.repo.ConfigScoped(config.LocalScope)
		require.NoError(t, err)
		assert.Equal(t, "FromInclude", cfg.User.Name)
	})

	t.Run("later value wins over include", func(t *testing.T) {
		f.appendLocal(t, "[user]\n\tname = After\n")

		cfg, err := f.repo.ConfigScoped(config.LocalScope)
		require.NoError(t, err)
		assert.Equal(t, "After", cfg.User.Name)
	})
}

//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludeIfGitDir(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "matched"), "[user]\n\temail = matched@example.com\n")
	f.write(t, filepath.Join(f.root, "other"), "[user]\n\tname = ShouldNotLoad\n")

	f.write(t, f.global,
		"[includeIf \"gitdir:"+slashPath(f.root)+"/\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "matched"))+"\n"+
			"[includeIf \"gitdir:/definitely/elsewhere/\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "other"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)

	assert.Equal(t, "matched@example.com", cfg.User.Email)
	assert.Empty(t, cfg.User.Name)
}

// worktree: conditions match the repository's working tree, and are false
// for a bare repository, which has none.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludeIfWorkTree(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "matched"), "[user]\n\temail = matched@example.com\n")
	f.write(t, f.global,
		"[includeIf \"worktree:"+slashPath(filepath.Join(f.root, "repo"))+"\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "matched"))+"\n"+
			"[includeIf \"worktree:**\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "matched"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)
	assert.Equal(t, "matched@example.com", cfg.User.Email)

	bare, err := git.PlainInit(filepath.Join(f.root, "bare.git"), true)
	require.NoError(t, err)
	cfg, err = bare.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)
	assert.Empty(t, cfg.User.Email)
}

//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludeIfOnBranch(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "onbranch"), "[user]\n\temail = branch@example.com\n")
	f.write(t, filepath.Join(f.root, "other"), "[user]\n\tname = ShouldNotLoad\n")

	f.write(t, f.global,
		"[includeIf \"onbranch:"+f.branch+"\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "onbranch"))+"\n"+
			"[includeIf \"onbranch:no-such-branch\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "other"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)

	assert.Equal(t, "branch@example.com", cfg.User.Email)
	assert.Empty(t, cfg.User.Name)
}

// The remote the condition matches on lives in the repository's own
// config, while the condition itself is in the global one. Resolving it
// needs the pre-pass that collects remote URLs across every scope.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludeIfHasConfigRemoteURL(t *testing.T) {
	f := newIncludeFixture(t)

	f.appendLocal(t, "[remote \"origin\"]\n\turl = git@github.com:work/thing.git\n")

	f.write(t, filepath.Join(f.root, "work"), "[user]\n\temail = work@example.com\n")
	f.write(t, filepath.Join(f.root, "other"), "[user]\n\tname = ShouldNotLoad\n")

	f.write(t, f.global,
		"[includeIf \"hasconfig:remote.*.url:git@github.com:work/**\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "work"))+"\n"+
			"[includeIf \"hasconfig:remote.*.url:git@github.com:nope/**\"]\n"+
			"\tpath = "+slashPath(filepath.Join(f.root, "other"))+"\n")

	cfg, err := f.repo.ConfigScoped(config.GlobalScope)
	require.NoError(t, err)

	assert.Equal(t, "work@example.com", cfg.User.Email)
	assert.Empty(t, cfg.User.Name)
}

// Repository.Config must keep returning the file as written. It is what
// SetConfig writes back, so inlining included options there would copy
// them permanently into .git/config.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigDoesNotInlineIncludes(t *testing.T) {
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "extra"), "[user]\n\tname = FromInclude\n")
	f.appendLocal(t, "[include]\n\tpath = "+slashPath(filepath.Join(f.root, "extra"))+"\n")

	cfg, err := f.repo.Config()
	require.NoError(t, err)
	assert.Empty(t, cfg.User.Name, "Config must not resolve includes")

	// A read-modify-write cycle must leave the include directive in
	// place and must not materialise the included value.
	require.NoError(t, f.repo.SetConfig(cfg))

	written, err := os.ReadFile(f.local)
	require.NoError(t, err)

	assert.Contains(t, string(written), "path = "+slashPath(filepath.Join(f.root, "extra")))
	assert.NotContains(t, string(written), "FromInclude")

	// The resolved view still sees the included value afterwards.
	scoped, err := f.repo.ConfigScoped(config.LocalScope)
	require.NoError(t, err)
	assert.Equal(t, "FromInclude", scoped.User.Name)
}

// An include naming a file that does not exist is skipped, the way git
// skips an optional per-machine config.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedMissingIncludeIsSkipped(t *testing.T) {
	f := newIncludeFixture(t)

	f.appendLocal(t,
		"[include]\n\tpath = "+slashPath(filepath.Join(f.root, "absent"))+"\n"+
			"[user]\n\tname = Survives\n")

	cfg, err := f.repo.ConfigScoped(config.LocalScope)
	require.NoError(t, err)
	assert.Equal(t, "Survives", cfg.User.Name)
}

// An include path through a symlinked directory is opened as git opens it,
// on the host, rather than through a filesystem bound to its root that
// refuses to cross the symlink.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedIncludeThroughSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	f := newIncludeFixture(t)

	f.write(t, filepath.Join(f.root, "store", "inc"), "[user]\n\tname = ViaSymlink\n")
	require.NoError(t, os.Symlink(filepath.Join(f.root, "store"), filepath.Join(f.root, "link")))
	include := "[include]\n\tpath = " + slashPath(filepath.Join(f.root, "link", "inc")) + "\n"
	f.appendLocal(t, include)
	f.write(t, f.global, include)

	for _, scope := range []config.Scope{config.LocalScope, config.SystemScope} {
		cfg, err := f.repo.ConfigScoped(scope)
		require.NoError(t, err)
		assert.Equal(t, "ViaSymlink", cfg.User.Name)
	}
}

// A linked worktree reads the config of the common directory, and that
// config's relative includes resolve against the common directory too.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedRelativeIncludeFromLinkedWorktree(t *testing.T) {
	gitBin := requireGit(t)
	f := newIncludeFixture(t)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	worktree := filepath.Join(f.root, "repo")
	linked := filepath.Join(f.root, "linked")
	run("-C", worktree, "commit", "-q", "--allow-empty", "-m", "initial")
	run("-C", worktree, "worktree", "add", "-q", "-b", "linked", linked)

	f.write(t, filepath.Join(f.gitDir, "inc"), "[user]\n\tname = FromCommonDir\n")
	f.write(t, filepath.Join(f.gitDir, "worktrees", "linked", "inc"), "[user]\n\tname = FromWorktreeDir\n")
	f.appendLocal(t, "[include]\n\tpath = inc\n")

	repo, err := git.PlainOpen(linked)
	require.NoError(t, err)
	cfg, err := repo.ConfigScoped(config.LocalScope)
	require.NoError(t, err)
	assert.Equal(t, "FromCommonDir", cfg.User.Name)
}

// Git refuses a remote URL in a file an includeIf reaches when it has to
// collect remote URLs for a hasconfig:remote.*.url: condition, so that no
// such condition can make itself true.
//
//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConfigScopedRefusesSelfSatisfyingRemoteURLCondition(t *testing.T) {
	f := newIncludeFixture(t)

	inc := filepath.Join(f.root, "inc")
	f.write(t, inc, "[remote \"x\"]\n\turl = https://example.com/r.git\n[user]\n\tname = SelfSatisfied\n")
	f.appendLocal(t, "[includeIf \"hasconfig:remote.*.url:https://example.com/**\"]\n\tpath = "+slashPath(inc)+"\n")

	_, err := f.repo.ConfigScoped(config.LocalScope)
	assert.ErrorIs(t, err, format.ErrRemoteURLInConditionalInclude)
}
