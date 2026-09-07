package gitenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

// gitIn runs a git command through Env and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v in %s: %s", args, dir, out)

	return strings.TrimSpace(string(out))
}

// unparseableConfig makes git refuse every command it is given, so a command
// that reads it fails outright rather than quietly picking up a setting.
const unparseableConfig = "[core\nthis is not a config file\n"

// poisonHome points HOME at a directory holding the given .gitconfig. This is
// how a real machine breaks a test run: not through an environment variable,
// but through the file the contributor has had in their home directory for
// years.
func poisonHome(t *testing.T, config string) {
	t.Helper()

	home := t.TempDir()
	path := filepath.Join(home, ".gitconfig")
	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))
	t.Setenv("HOME", home)
	// git for Windows derives HOME from USERPROFILE when HOME is unset; set
	// both so the poison lands on every platform the tests run on.
	t.Setenv("USERPROFILE", home)
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH")
	}
}

// TestEnvIgnoresTheMachinesOwnConfig is the property the package exists for: a
// git subprocess started through Env does not read the user's configuration.
//
// Each case is a shape a real ~/.gitconfig takes, and each would break a test
// run in its own way if it were read.
//
//nolint:paralleltest // every case calls t.Setenv, which forbids parallel
func TestEnvIgnoresTheMachinesOwnConfig(t *testing.T) {
	// Not parallel: t.Setenv, and the environment is process-wide.
	for _, tc := range []struct { //nolint:paralleltest // t.Setenv in each subtest
		name   string
		config string
	}{
		{
			// What a contributor who signs their commits has. gpg.program
			// cannot be executed here, standing in for a signing key that is
			// absent, locked, or on a token nobody is present to touch.
			name: "signing turned on",
			config: "[user]\n\tname = poison\n\temail = poison@example.invalid\n" +
				"[commit]\n\tgpgsign = true\n" +
				"[tag]\n\tgpgsign = true\n" +
				"[gpg]\n\tprogram = go-git-nonexistent-signing-program\n",
		},
		{
			// The other direction: a machine with no identity at all, which
			// is what a test that never sets one has been borrowing.
			name:   "no identity to borrow",
			config: "[core]\n\tquotePath = false\n",
		},
		{
			// git refuses to run at all, so this case is what a read-only
			// command has to survive: isolating those matters too, and
			// nothing else here would notice if it were dropped.
			name:   "config that does not parse",
			config: unparseableConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireGit(t)
			poisonHome(t, tc.config)

			dir := t.TempDir()
			gitIn(t, dir, "init")
			f := filepath.Join(dir, "f.txt")
			require.NoError(t, os.WriteFile(f, []byte("x\n"), 0o644))
			gitIn(t, dir, "add", "f.txt")
			gitIn(t, dir, "commit", "-m", "hermetic")

			require.Equal(t, "hermetic", gitIn(t, dir, "log", "-1", "--format=%s"))
			require.Empty(t, gitIn(t, dir, "log", "-1", "--format=%GK"),
				"the commit must not be signed with a key the machine happens to hold")
		})
	}
}

// TestEnvKeepsAnExplicitIdentity checks the override half: a caller that sets
// one of these deliberately — CI pinning the author it wants to see — keeps it.
func TestEnvKeepsAnExplicitIdentity(t *testing.T) {
	requireGit(t)
	poisonHome(t, unparseableConfig)
	t.Setenv("GIT_AUTHOR_NAME", "CI Runner")
	t.Setenv("GIT_AUTHOR_EMAIL", "ci@example.com")

	dir := t.TempDir()
	gitIn(t, dir, "init")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "authored by ci")

	require.Equal(t, "CI Runner", gitIn(t, dir, "log", "-1", "--format=%an"))
	require.Equal(t, "ci@example.com", gitIn(t, dir, "log", "-1", "--format=%ae"))
}

// TestEnvKeepsAnExplicitConfigFile is the same override, for the setting that
// disables config reading rather than one that supplies a value: a caller who
// points GIT_CONFIG_GLOBAL at a fixture gets that fixture, not the empty
// default this package would otherwise apply.
func TestEnvKeepsAnExplicitConfigFile(t *testing.T) {
	requireGit(t)
	poisonHome(t, unparseableConfig)

	fixture := filepath.Join(t.TempDir(), "gitconfig")
	contents := []byte("[user]\n\tname = From The Fixture\n")
	require.NoError(t, os.WriteFile(fixture, contents, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", fixture)

	dir := t.TempDir()
	gitIn(t, dir, "init")

	// Read back through config rather than through a commit: the identity
	// defaults are environment variables, and those outrank config on an
	// author line, which would hide whether the file was read at all.
	require.Equal(t, "From The Fixture", gitIn(t, dir, "config", "--get", "user.name"))
}
