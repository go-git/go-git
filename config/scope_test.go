package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRoot(parts ...string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// memRepo builds an in-memory tree containing the given files, plus a
// directory for each entry in dirs.
//
// The directories are made by writing a placeholder inside them rather
// than with MkdirAll: on Windows, memfs resolves the two through
// different code paths, and a drive-lettered path passed to MkdirAll
// ends up stored somewhere Stat cannot find it again.
func memRepo(t *testing.T, files map[string]string, dirs ...string) billy.Filesystem {
	t.Helper()

	fs := memfs.New()
	for _, d := range dirs {
		require.NoError(t, util.WriteFile(fs, filepath.Join(d, ".keep"), nil, 0o644))
	}
	for p, content := range files {
		require.NoError(t, util.WriteFile(fs, p, []byte(content), 0o644))
	}

	return fs
}

// clearGitEnv makes discovery independent of the environment the test
// process happens to run in.
// Tests using this helper cannot call t.Parallel: it sets environment
// variables, which Go forbids combining with parallel tests.
func clearGitEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envGitDir, envGitCommonDir, envGitCeiling} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}
}

//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestDiscoverGitDirFromWorktree(t *testing.T) {
	clearGitEnv(t)

	repo := testRoot("src", "repo")
	gitDir := filepath.Join(repo, dotGit)
	fs := memRepo(t,
		map[string]string{filepath.Join(gitDir, configFile): ""},
		gitDir)

	// Discovery walks up from a nested subdirectory.
	nested := filepath.Join(repo, "a", "b")
	require.NoError(t, fs.MkdirAll(nested, 0o755))

	got, err := DiscoverGitDir(fs, nested)
	require.NoError(t, err)
	assert.Equal(t, gitDir, got)
}

func TestDiscoverGitDirHonoursGitDirEnv(t *testing.T) {
	clearGitEnv(t)

	explicit := testRoot("elsewhere", "gitdir")
	t.Setenv(envGitDir, explicit)

	fs := memRepo(t, nil)

	got, err := DiscoverGitDir(fs, testRoot("src", "repo"))
	require.NoError(t, err)
	assert.Equal(t, explicit, got)
}

// A linked worktree or submodule has a .git file pointing elsewhere.
//
//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestDiscoverGitDirFollowsGitFile(t *testing.T) {
	clearGitEnv(t)

	worktree := testRoot("src", "wt")
	target := testRoot("src", "repo", ".git", "worktrees", "wt")

	fs := memRepo(t, map[string]string{
		filepath.Join(worktree, dotGit): "gitdir: " + filepath.ToSlash(target) + "\n",
	}, target)

	got, err := DiscoverGitDir(fs, worktree)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(target), got)
}

//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestDiscoverGitDirFollowsRelativeGitFile(t *testing.T) {
	clearGitEnv(t)

	worktree := testRoot("src", "wt")
	target := testRoot("src", "repo", ".git", "worktrees", "wt")

	fs := memRepo(t, map[string]string{
		filepath.Join(worktree, dotGit): "gitdir: ../repo/.git/worktrees/wt\n",
	}, target)

	got, err := DiscoverGitDir(fs, worktree)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(target), got)
}

//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestDiscoverGitDirBareRepository(t *testing.T) {
	clearGitEnv(t)

	bare := testRoot("srv", "repo.git")
	fs := memRepo(t,
		map[string]string{
			filepath.Join(bare, headFile):   "ref: refs/heads/main\n",
			filepath.Join(bare, configFile): "",
		},
		filepath.Join(bare, "objects"),
		filepath.Join(bare, "refs"))

	got, err := DiscoverGitDir(fs, bare)
	require.NoError(t, err)
	assert.Equal(t, bare, got)
}

//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestDiscoverGitDirNotFound(t *testing.T) {
	clearGitEnv(t)

	dir := testRoot("nowhere", "at", "all")
	fs := memRepo(t, nil, dir)

	_, err := DiscoverGitDir(fs, dir)
	assert.ErrorIs(t, err, ErrGitDirNotFound)
}

func TestDiscoverGitDirStopsAtCeiling(t *testing.T) {
	clearGitEnv(t)

	repo := testRoot("src", "repo")
	gitDir := filepath.Join(repo, dotGit)
	nested := filepath.Join(repo, "a")

	fs := memRepo(t, map[string]string{
		filepath.Join(gitDir, configFile): "",
	}, gitDir, nested)

	// Without a ceiling the repository is found.
	got, err := DiscoverGitDir(fs, nested)
	require.NoError(t, err)
	assert.Equal(t, gitDir, got)

	// With the repository root as ceiling the walk stops before it.
	t.Setenv(envGitCeiling, repo)
	_, err = DiscoverGitDir(fs, nested)
	assert.ErrorIs(t, err, ErrGitDirNotFound)
}

// A linked worktree shares the repository-level config with the main
// worktree, which is what git reads for the local scope.
//
//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestLocalConfigPathUsesCommonDir(t *testing.T) {
	clearGitEnv(t)

	mainGit := testRoot("src", "repo", ".git")
	wtGit := filepath.Join(mainGit, "worktrees", "wt")

	fs := memRepo(t, map[string]string{
		filepath.Join(wtGit, commonDirFile): "../..\n",
	}, mainGit)

	got, err := LocalConfigPath(fs, wtGit)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(mainGit, configFile), got)
}

//nolint:paralleltest // clearGitEnv calls t.Setenv, which forbids t.Parallel
func TestLocalConfigPathWithoutCommonDir(t *testing.T) {
	clearGitEnv(t)

	gitDir := testRoot("src", "repo", ".git")
	fs := memRepo(t, nil, gitDir)

	got, err := LocalConfigPath(fs, gitDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(gitDir, configFile), got)
}

func TestHeadBranch(t *testing.T) {
	t.Parallel()

	gitDir := testRoot("src", "repo", ".git")

	tests := []struct {
		name string
		head string
		want string
	}{
		{"branch", "ref: refs/heads/main\n", "main"},
		{"nested branch", "ref: refs/heads/feature/x\n", "feature/x"},
		{"detached", "0123456789012345678901234567890123456789\n", ""},
		{"non-branch ref", "ref: refs/tags/v1\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := memRepo(t, map[string]string{
				filepath.Join(gitDir, headFile): tt.head,
			}, gitDir)

			assert.Equal(t, tt.want, HeadBranch(fs, gitDir))
		})
	}
}

func TestHeadBranchMissingFile(t *testing.T) {
	t.Parallel()

	gitDir := testRoot("src", "repo", ".git")
	fs := memRepo(t, nil, gitDir)

	assert.Empty(t, HeadBranch(fs, gitDir))
}

func TestRemoteURLs(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal([]byte(`
[remote "origin"]
	url = git@github.com:work/a.git
	url = git@github.com:work/mirror.git
[remote "fork"]
	url = git@github.com:me/a.git
`)))

	assert.ElementsMatch(t, []string{
		"git@github.com:work/a.git",
		"git@github.com:work/mirror.git",
		"git@github.com:me/a.git",
	}, RemoteURLs(cfg.Raw))
}

func TestRemoteURLsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, RemoteURLs(nil))
}
