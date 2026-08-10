package configinclude_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
)

// These tests compare go-git's include resolution against the real git
// binary. Self-consistent tests cannot catch a misreading of
// git-config(1); this can.

func requireGit(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available in PATH")
	}

	return path
}

// gitConfigGet returns `git config --get key` for the repository, and
// whether the key was set at all.
func gitConfigGet(t *testing.T, gitBin, worktree, key string) (string, bool) {
	t.Helper()

	cmd := exec.Command(gitBin, "-C", worktree, "config", "--get", key)
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		// Exit status 1 means the key is simply unset.
		if ok := asExitError(err, &exit); ok && exit.ExitCode() == 1 {
			return "", false
		}
		require.NoError(t, err, "git config --get %s: %s", key, string(out))
	}

	return strings.TrimRight(string(out), "\r\n"), true
}

func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// conformanceCase describes one configuration layout to compare.
type conformanceCase struct {
	name string
	// files are written relative to the fixture root.
	files map[string]string
	// global is the content of the global config file.
	global string
	// local is appended to the repository's own config.
	local string
}

//nolint:paralleltest // newIncludeFixture calls t.Setenv, which forbids t.Parallel
func TestConformanceWithGit(t *testing.T) {
	gitBin := requireGit(t)

	cases := []conformanceCase{
		{
			name:   "unconditional include",
			files:  map[string]string{"inc": "[user]\n\tname = Included\n\temail = inc@example.com\n"},
			global: "[include]\n\tpath = {root}/inc\n",
		},
		{
			name:   "include overrides earlier value",
			files:  map[string]string{"inc": "[user]\n\tname = Included\n"},
			global: "[user]\n\tname = Before\n[include]\n\tpath = {root}/inc\n",
		},
		{
			name:   "later value overrides include",
			files:  map[string]string{"inc": "[user]\n\tname = Included\n"},
			global: "[include]\n\tpath = {root}/inc\n[user]\n\tname = After\n",
		},
		{
			name: "nested include with relative path",
			files: map[string]string{
				"a":      "[include]\n\tpath = b\n",
				"b":      "[user]\n\tname = Nested\n",
				"unused": "",
			},
			global: "[include]\n\tpath = {root}/a\n",
		},
		{
			name:   "missing include file is skipped",
			files:  map[string]string{},
			global: "[include]\n\tpath = {root}/absent\n[user]\n\tname = Survives\n",
		},
		{
			name:   "gitdir with trailing slash",
			files:  map[string]string{"inc": "[user]\n\tname = ByGitDir\n"},
			global: "[includeIf \"gitdir:{root}/\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir non-matching",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"gitdir:/definitely/elsewhere/\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir bare name gets **/ prefix",
			files:  map[string]string{"inc": "[user]\n\tname = ByBareName\n"},
			global: "[includeIf \"gitdir:repo/\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir is case sensitive",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"gitdir:REPO/\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir/i is case insensitive",
			files:  map[string]string{"inc": "[user]\n\tname = CaseInsensitive\n"},
			global: "[includeIf \"gitdir/i:REPO/\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir single star does not cross separator",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"gitdir:{root}/*/.git\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "gitdir double star crosses separators",
			files:  map[string]string{"inc": "[user]\n\tname = DoubleStar\n"},
			global: "[includeIf \"gitdir:{root}/**/.git\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "onbranch matching",
			files:  map[string]string{"inc": "[user]\n\tname = OnBranch\n"},
			global: "[includeIf \"onbranch:{branch}\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "onbranch non-matching",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"onbranch:no-such-branch\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "unknown condition is false",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"onsolarflare:high\"]\n\tpath = {root}/inc\n",
		},
		{
			name:   "hasconfig remote url matching",
			files:  map[string]string{"inc": "[user]\n\tname = ByRemote\n"},
			global: "[includeIf \"hasconfig:remote.*.url:git@github.com:work/**\"]\n\tpath = {root}/inc\n",
			local:  "[remote \"origin\"]\n\turl = git@github.com:work/thing.git\n",
		},
		{
			name:   "hasconfig remote url non-matching",
			files:  map[string]string{"inc": "[user]\n\tname = ShouldNotLoad\n"},
			global: "[includeIf \"hasconfig:remote.*.url:git@github.com:nope/**\"]\n\tpath = {root}/inc\n",
			local:  "[remote \"origin\"]\n\turl = git@github.com:work/thing.git\n",
		},
		{
			name:   "local include in repository config",
			files:  map[string]string{"inc": "[user]\n\tname = LocalInclude\n"},
			global: "",
			local:  "[include]\n\tpath = {root}/inc\n",
		},
	}

	// Only keys go-git models on the merged Config can be compared:
	// config.Merge rebuilds its Raw from struct state, so options it
	// does not know about do not survive the merge.
	keys := []string{"user.name", "user.email"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newIncludeFixture(t)

			expand := func(s string) string {
				s = strings.ReplaceAll(s, "{root}", slashPath(f.root))
				return strings.ReplaceAll(s, "{branch}", f.branch)
			}

			for name, content := range tc.files {
				f.write(t, filepath.Join(f.root, name), expand(content))
			}
			if tc.global != "" {
				f.write(t, f.global, expand(tc.global))
			}
			if tc.local != "" {
				f.appendLocal(t, expand(tc.local))
			}

			cfg, err := f.repo.ConfigScoped(config.SystemScope)
			require.NoError(t, err)

			got := map[string]string{
				"user.name":  cfg.User.Name,
				"user.email": cfg.User.Email,
			}

			for _, key := range keys {
				want, _ := gitConfigGet(t, gitBin, filepath.Join(f.root, "repo"), key)
				assert.Equal(t, want, got[key], "%s differs from git", key)
			}
		})
	}
}
