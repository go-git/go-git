// Package gitenv runs the git binary from tests without the machine's own
// configuration.
//
// A test that shells out to git inherits the process environment, and git
// reads the user's and the system's configuration through it. What the test
// observes then depends on who is running it: signing, identity, aliases,
// template directories, excludes files and default branch names all differ
// from one machine to the next, CI supplies settings a developer machine does
// not, and a config file git cannot parse fails every command it is given,
// `git --version` included. Some variables are worse than a setting: GIT_DIR
// and its relatives point git at a repository other than the one the test
// built, so a stray value redirects the command rather than colouring it.
//
// Command returns a command with all of that removed and an identity supplied.
// Env returns the environment on its own, for a caller that builds the command
// itself. Every variable this package names is set unconditionally, whatever
// the machine had: a value inherited from the environment is the thing being
// isolated, so honouring it would defeat the purpose. A caller who needs
// something different appends it, since the last entry for a variable is the
// one the child sees:
//
//	cmd := gitenv.Command("git", "commit", "-m", "example")
//	cmd.Dir = dir
//	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=someone else")
//	out, err := cmd.CombinedOutput()
package gitenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// stripped are variables no value of ours can correct, because their problem
// is that they are set at all: they name a repository, or a part of one, or
// inject configuration directly.
//
// The repository half is git's own local_repo_env (environment.c), which git
// clears in sanitize_repo_env() before running against a repository other
// than the one it was invoked in — which is gitenv's situation exactly. A test
// run inherits these whenever `go test` is reached from a pre-commit or
// pre-push hook (githooks(5): "Environment variables, such as GIT_DIR,
// GIT_WORK_TREE, etc., are exported"), from `git bisect run`, or from
// `git rebase --exec`.
//
// GIT_CONFIG_PARAMETERS and the GIT_CONFIG_COUNT family go too, which git
// itself keeps deliberately: to git they carry the -c options of the parent
// git and are worth propagating, but to a test they are one more ambient
// setting, and settings below needs the count to itself.
//
// GIT_EXEC_PATH is deliberately absent: the compatibility matrix points it at
// the git build under test.
var stripped = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_CONFIG",
	"GIT_CONFIG_COUNT",
	"GIT_CONFIG_PARAMETERS",
	"GIT_DIR",
	"GIT_GRAFT_FILE",
	"GIT_IMPLICIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_NO_REPLACE_OBJECTS",
	"GIT_OBJECT_DIRECTORY",
	"GIT_PREFIX",
	"GIT_REPLACE_REF_BASE",
	"GIT_SHALLOW_FILE",
	"GIT_WORK_TREE",

	// Not repository locations, and not in local_repo_env: an inherited
	// askpass helper is asked for credentials at a point where the test
	// wanted a failure, and it is a program of the machine's choosing.
	"GIT_ASKPASS",
	"SSH_ASKPASS",
}

// strippedPrefixes are stripped by prefix because the number of them is not
// fixed: GIT_CONFIG_KEY_0 upwards, as many as GIT_CONFIG_COUNT claimed.
var strippedPrefixes = []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"}

// replaced are the variables given a value of this package's own, whatever the
// environment held.
var replaced = []string{
	// GIT_CONFIG_NOSYSTEM is what actually suppresses system config:
	// GIT_CONFIG_SYSTEM only redirects the ETC_GITCONFIG path, and a git that
	// reads a gitconfig from elsewhere — Apple's does — ignores it.
	"GIT_CONFIG_NOSYSTEM=1",
	// An empty GIT_CONFIG_GLOBAL or GIT_CONFIG_SYSTEM names the empty path
	// rather than disabling the file; git reads nothing because access()
	// reports ENOENT and git tolerates that errno. /dev/null, which
	// git-config(1) recommends and git for Windows special-cases, would do the
	// same thing by the same mechanism.
	"GIT_CONFIG_GLOBAL=",
	"GIT_CONFIG_SYSTEM=",
	"GIT_AUTHOR_NAME=tester",
	"GIT_AUTHOR_EMAIL=tester@test",
	"GIT_COMMITTER_NAME=tester",
	"GIT_COMMITTER_EMAIL=tester@test",
	// Prompting is on by default and the prompt opens /dev/tty itself, so
	// redirected stdio does not prevent it. With no home directory there is no
	// credential helper either, which would turn an authentication failure a
	// test asserts on into a hang.
	"GIT_TERMINAL_PROMPT=0",
}

// homeVars are how git reaches ~/.gitconfig and $XDG_CONFIG_HOME/git/config.
//
// GIT_CONFIG_GLOBAL arrived in git 2.32 and older releases ignore it silently
// — the compatibility matrix builds v2.11.0 — so redirecting the home
// directory is what isolates those. It is the load-bearing half of the two.
var homeVars = []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"}

// settings are configuration a test run should not take from the machine and
// which no variable of its own can express.
//
// init.defaultBranch is the one that matters: with global config gone, `git
// init` falls back to the built-in default, which is master today, is
// documented to become main in git 3.0, and prints a twelve-line hint until it
// is set. Pinning master keeps the name a call site sees the same on every
// machine and every git, and matches what v2.11.0 does unprompted.
//
// GIT_CONFIG_COUNT arrived in git 2.31 and earlier releases ignore it, which
// costs nothing here: v2.11.0 defaults to master already and has no
// defaultBranchName advice to print.
var settings = [][2]string{
	{"init.defaultBranch", "master"},
}

// isolatedHome is the path git is given as a home directory. It does not
// exist, and nothing here creates it.
//
// A home directory git reads no config from is a path that holds no config
// file, and a path that holds nothing at all satisfies that: opening
// ~/.gitconfig under it gives ENOENT, exactly as it does for a user who has
// never written one. Creating a directory instead would mean either leaving it
// behind, since a test binary has no moment at which it knows no further
// command will be built and so cannot remove one, or removing the name at once
// and handing git a path any other local user is then free to create — with a
// .gitconfig in it that git will read, applying no ownership check of its own,
// since the CVE-2022-24765 checks cover repositories rather than the config
// files in a home directory.
//
// The path goes beside the test binary, in the directory the go tool builds it
// in, which answers all of that without creating anything: the tool makes that
// tree 0700, so no other user can put a directory there; it is unique to this
// binary, so test binaries running side by side do not share a path; and the
// tool removes the tree when the run ends, so nothing is left behind.
//
// A path elsewhere would still isolate git, since what does that is the path
// holding nothing, but it would give up the rest: a name in the temporary
// directory is one another user can predict and create. There is no reason to
// reach for one — os.Executable answers on every platform the tests run on —
// so a failure here says so rather than quietly settling for less.
var isolatedHome = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		panic("gitenv: cannot locate the test binary, so git has nowhere private to be pointed at: " + err.Error())
	}

	return filepath.Join(filepath.Dir(exe), "go-git-gitenv-home")
})

// Command returns an exec.Cmd for the named program, with Env already applied.
//
// This is the way to reach for git from a test. A command built here cannot
// read the machine's configuration or be pointed at another repository, where
// one built with exec.Command has to remember not to. The caller sets Dir, and
// may append to Env for anything it needs on top: later entries win.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = Env()

	return cmd
}

// Env returns the environment to run git in: the process environment with the
// machine's configuration taken out and this package's own put in.
//
// The variables in replaced, the home directory and the settings above take
// the value chosen here whatever the environment held, and the variables in
// stripped are removed outright. A caller that means to control one of them
// appends its own entry after this one, which is what the child will see.
func Env() []string {
	added := isolation(isolatedHome())

	drop := make(map[string]bool, len(stripped)+len(added))
	for _, key := range stripped {
		drop[key] = true
	}
	for _, kv := range added {
		key, _, _ := strings.Cut(kv, "=")
		drop[key] = true
	}

	environ := os.Environ()
	env := make([]string, 0, len(environ)+len(added))
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if drop[key] || hasStrippedPrefix(key) {
			continue
		}
		env = append(env, kv)
	}

	return append(env, added...)
}

// isolation is what Env adds, for the home directory it was given.
func isolation(home string) []string {
	env := make([]string, 0, len(replaced)+len(homeVars)+1+2*len(settings))
	env = append(env, replaced...)

	for _, key := range homeVars {
		env = append(env, key+"="+home)
	}

	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, setting := range settings {
		n := strconv.Itoa(i)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+setting[0], "GIT_CONFIG_VALUE_"+n+"="+setting[1])
	}

	return env
}

func hasStrippedPrefix(key string) bool {
	for _, prefix := range strippedPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
