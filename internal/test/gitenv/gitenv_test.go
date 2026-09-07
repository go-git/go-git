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

// gitIn runs a git command through Command and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitenv.Command("git", args...)
	cmd.Dir = dir

	return runGit(t, cmd, dir, args)
}

// gitInEnv is gitIn with the environment chosen by the caller, so a test can
// run git as a machine that reads only part of that environment would.
func gitInEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env

	return runGit(t, cmd, dir, args)
}

func runGit(t *testing.T, cmd *exec.Cmd, dir string, args []string) string {
	t.Helper()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v in %s: %s", args, dir, out)

	return strings.TrimSpace(string(out))
}

// commitIn makes a commit in dir with the given environment, initialising the
// repository first: the sequence every isolation case here has to survive.
func commitIn(t *testing.T, dir string, env []string, subject string) {
	t.Helper()

	gitInEnv(t, dir, env, "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644))
	gitInEnv(t, dir, env, "add", "f.txt")
	gitInEnv(t, dir, env, "commit", "-m", subject)
}

// unparseableConfig makes git refuse every command it is given, so a command
// that reads it fails outright rather than quietly picking up a setting.
const unparseableConfig = "[core\nthis is not a config file\n"

// poisonHome points the home directory at one holding the given config, at
// both paths git looks for it. This is how a real machine breaks a test run:
// not through an environment variable, but through the file the contributor
// has had in their home directory for years.
func poisonHome(t *testing.T, config string) {
	t.Helper()

	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "git"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, "git", "config"), []byte(config), 0o600))

	t.Setenv("HOME", home)
	// git for Windows derives HOME from USERPROFILE when HOME is unset; set
	// both so the poison lands on every platform the tests run on.
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
}

// withoutConfigEnv drops GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM, leaving the
// environment as a git before 2.32 reads it — the matrix builds v2.11.0 — so
// isolation has to hold here as well.
func withoutConfigEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, kv := range env {
		switch key, _, _ := strings.Cut(kv, "="); key {
		case "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM":
			continue
		default:
			kept = append(kept, kv)
		}
	}

	return kept
}

// envVariants are the environments the isolation property must hold in.
var envVariants = []struct {
	name string
	env  func() []string
}{
	{name: "as built", env: gitenv.Env},
	{
		name: "as a git without GIT_CONFIG_GLOBAL reads it",
		env:  func() []string { return withoutConfigEnv(gitenv.Env()) },
	},
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
		for _, variant := range envVariants {
			t.Run(tc.name+", "+variant.name, func(t *testing.T) {
				requireGit(t)
				poisonHome(t, tc.config)
				env := variant.env()

				dir := t.TempDir()
				commitIn(t, dir, env, "hermetic")

				require.Equal(t, "hermetic", gitInEnv(t, dir, env, "log", "-1", "--format=%s"))
				require.Empty(t, gitInEnv(t, dir, env, "log", "-1", "--format=%GK"),
					"the commit must not be signed with a key the machine happens to hold")
			})
		}
	}
}

// TestEnvIgnoresAnIdentityInTheEnvironment is the same property for the
// identity, which reaches git through variables rather than through a file: a
// name exported from a shell profile or by direnv is ambient input too, and
// this package replaces it rather than filling in around it.
func TestEnvIgnoresAnIdentityInTheEnvironment(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_AUTHOR_NAME", "poison")
	t.Setenv("GIT_AUTHOR_EMAIL", "poison@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "poison")
	t.Setenv("GIT_COMMITTER_EMAIL", "poison@example.invalid")

	dir := t.TempDir()
	commitIn(t, dir, gitenv.Env(), "hermetic")

	require.Equal(t, "tester tester", gitIn(t, dir, "log", "-1", "--format=%an %cn"))
}

// TestEnvIgnoresAmbientRepositoryLocations covers the variables that redirect
// git rather than configure it. GIT_DIR and its relatives are exported by
// every hook git runs (githooks(5)), by `git bisect run` and by
// `git rebase --exec`, so `go test` reached from any of those inherits them —
// and a redirected command is a worse failure than a stray setting.
func TestEnvIgnoresAmbientRepositoryLocations(t *testing.T) {
	requireGit(t)

	// The repository the ambient variables point at, built before they are
	// set so that it is reachable at all.
	decoy := t.TempDir()
	gitIn(t, decoy, "init")

	decoyGit := filepath.Join(decoy, ".git")
	t.Setenv("GIT_DIR", decoyGit)
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_COMMON_DIR", decoyGit)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoyGit, "index"))
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(decoyGit, "objects"))

	dir := t.TempDir()
	commitIn(t, dir, gitenv.Env(), "hermetic")

	require.Equal(t, "hermetic", gitIn(t, dir, "log", "-1", "--format=%s"),
		"the commit must land in the repository the test built")
	// for-each-ref rather than rev-list: it is the reference that would have
	// been created here, and asking for one in a repository that has none is
	// a usage error to git 2.11 rather than an answer of zero.
	require.Empty(t, gitIn(t, decoy, "for-each-ref"),
		"nothing must reach the repository the environment named")
}

// TestEnvIgnoresConfigInjectedThroughTheEnvironment covers the other way
// configuration arrives without a file: the -c options of a parent git, which
// git exports for its own children to inherit. git keeps these deliberately
// when it runs against another repository; a test cannot.
func TestEnvIgnoresConfigInjectedThroughTheEnvironment(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'user.name=poison' 'core.pager=poison'")
	// Two of them, so that the count this package sets for itself is not the
	// only thing standing between the second and git.
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "poison@example.invalid")
	t.Setenv("GIT_CONFIG_KEY_1", "core.excludesFile")
	t.Setenv("GIT_CONFIG_VALUE_1", "poison")

	dir := t.TempDir()
	gitIn(t, dir, "init")

	require.NotContains(t, gitIn(t, dir, "config", "--list"), "poison")
}

// TestEnvPinsTheInitialBranchName checks the setting that has no variable of
// its own. Left to git the name is a property of the build — master today,
// documented to become main in git 3.0 — which a call site that does not pass
// -c init.defaultBranch would silently follow.
func TestEnvPinsTheInitialBranchName(t *testing.T) { //nolint:paralleltest // t.Setenv
	requireGit(t)
	poisonHome(t, "[init]\n\tdefaultBranch = poison\n")

	dir := t.TempDir()
	out := gitIn(t, dir, "init")
	require.Equal(t, "refs/heads/master", gitIn(t, dir, "symbolic-ref", "HEAD"))
	require.NotContains(t, out, "hint:",
		"a pinned name is also what suppresses the defaultBranchName advice")
}

// TestCommandTakesAnAppendedOverride is the escape hatch the package documents
// in place of honouring the environment: a caller that needs a variable to
// hold something else appends it, and the last entry is what the child sees.
func TestCommandTakesAnAppendedOverride(t *testing.T) {
	t.Parallel()
	requireGit(t)

	dir := t.TempDir()
	gitIn(t, dir, "init")

	cmd := gitenv.Command("git", "commit", "--allow-empty", "-m", "appended")
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=Appended", "GIT_AUTHOR_EMAIL=appended@test")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git commit: %s", out)

	require.Equal(t, "Appended", gitIn(t, dir, "log", "-1", "--format=%an"))
}

// TestEnvSetsWhatItPromises is the environment read directly, for the entries
// whose effect is a command that does not happen: a prompt that is not opened,
// a helper that is not run.
func TestEnvSetsWhatItPromises(t *testing.T) { //nolint:paralleltest // t.Setenv
	// Set every variable to something recognisable, so that a value found
	// below came from this package rather than from the machine.
	for _, key := range []string{
		"GIT_TERMINAL_PROMPT", "GIT_CONFIG_NOSYSTEM", "GIT_ASKPASS", "SSH_ASKPASS",
	} {
		t.Setenv(key, "poison")
	}

	env := gitenv.Env()
	require.Equal(t, "0", valueOf(t, env, "GIT_TERMINAL_PROMPT"),
		"the prompt opens /dev/tty itself, so redirected stdio does not prevent it")
	require.Equal(t, "1", valueOf(t, env, "GIT_CONFIG_NOSYSTEM"),
		"this, not GIT_CONFIG_SYSTEM, is what suppresses system config")

	for _, key := range []string{"GIT_ASKPASS", "SSH_ASKPASS"} {
		require.NotContains(t, keys(env), key,
			"%s names a program of the machine's choosing, so it is removed rather than set", key)
	}
}

// keys returns the variable names in env.
func keys(env []string) []string {
	names := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		names = append(names, key)
	}

	return names
}

// valueOf returns the value the given environment carries for key, taking the
// last entry as exec does when a variable appears more than once.
func valueOf(t *testing.T, env []string, key string) string {
	t.Helper()

	value, found := "", false
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			value, found = v, true
		}
	}
	require.True(t, found, "Env must set %s", key)

	return value
}

// TestEnvRedirectsTheHomeDirectory checks what isolation rests on where
// GIT_CONFIG_GLOBAL is not read: the home directory is replaced rather than
// honoured, with a path holding no config file.
func TestEnvRedirectsTheHomeDirectory(t *testing.T) { //nolint:paralleltest // poisonHome calls t.Setenv
	poisonHome(t, unparseableConfig)
	machine := os.Getenv("HOME")

	env := gitenv.Env()
	home := valueOf(t, env, "HOME")
	require.NotEqual(t, machine, home, "the machine's home directory must not survive")
	for _, key := range []string{"USERPROFILE", "XDG_CONFIG_HOME"} {
		require.Equal(t, home, valueOf(t, env, key),
			"%s must lead to the same place as HOME", key)
	}

	for _, path := range []string{
		filepath.Join(home, ".gitconfig"),
		filepath.Join(home, "git", "config"),
	} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "%s must not exist", path)
	}
}

// TestEnvHomeDirectoryIsNotCreated is the other half of that: the path Env
// hands git holds nothing because it does not exist, which is what lets a
// package that cannot reach a *testing.T — `_examples` builds commands while
// initialising a package-level variable, and the ssh server's handler runs on
// a connection of its own — take part without leaving a directory behind.
//
// Running git against it is what the test checks, since a git that created a
// home directory it was pointed at would defeat this quietly.
func TestEnvHomeDirectoryIsNotCreated(t *testing.T) {
	t.Parallel()
	requireGit(t)

	home := valueOf(t, gitenv.Env(), "HOME")

	_, err := os.Lstat(home)
	require.ErrorIs(t, err, os.ErrNotExist, "%s must not be created", home)

	dir := t.TempDir()
	commitIn(t, dir, gitenv.Env(), "hermetic")
	gitIn(t, dir, "config", "--list")

	_, err = os.Lstat(home)
	require.ErrorIs(t, err, os.ErrNotExist, "%s must still not exist after git has run", home)
}

// TestEnvHomeDirectoryCannotBeSubstituted is why that path is the one beside
// the test binary rather than a name of its own in the temporary directory. A
// path that does not exist is a path someone else may create, and git reads a
// config file in a home directory without checking who owns it — the
// CVE-2022-24765 checks cover repositories. The go tool builds the binary
// under a directory it makes private, so no other user can get there.
func TestEnvHomeDirectoryCannotBeSubstituted(t *testing.T) {
	t.Parallel()

	home := valueOf(t, gitenv.Env(), "HOME")

	exe, err := os.Executable()
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(exe), filepath.Dir(home),
		"the home directory must sit beside the test binary, in the tree the go tool owns")

	if os.PathSeparator != '/' {
		return
	}
	for dir := filepath.Dir(home); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		if info.Mode().Perm()&0o022 == 0 {
			// Something on the way up denies everyone else the write it would
			// take to create the home directory, which is all this needs.
			return
		}
		require.NotEqual(t, dir, filepath.Dir(dir),
			"no directory above %s keeps other users out", home)
	}
}
