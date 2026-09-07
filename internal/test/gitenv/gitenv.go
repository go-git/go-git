// Package gitenv runs the git binary from tests without the machine's own
// configuration.
//
// A test that shells out to git inherits the process environment, and git
// reads the user's and the system's configuration through it. What the test
// observes then depends on who is running it: signing, identity, aliases,
// template directories, excludes files and default branch names all differ
// from one machine to the next, CI supplies settings a developer machine does
// not, and a config file git cannot parse fails every command it is given,
// `git --version` included.
//
// Command returns a command with that configuration excluded and an identity
// supplied. Env returns the environment on its own, for a caller that builds
// the command itself. Neither replaces a variable the caller has already set,
// so a test or a CI job can still pin what it needs:
//
//	cmd := gitenv.Command("git", "commit", "-m", "example")
//	cmd.Dir = dir
//	out, err := cmd.CombinedOutput()
package gitenv

import (
	"os"
	"os/exec"
	"strings"
)

// defaults are the settings a test run should not inherit from the machine.
//
// An empty GIT_CONFIG_GLOBAL or GIT_CONFIG_SYSTEM disables that file rather
// than naming one, which is portable in a way that a path such as /dev/null is
// not: the tests run on Windows too.
var defaults = []string{
	"GIT_CONFIG_GLOBAL=",
	"GIT_CONFIG_SYSTEM=",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=tester",
	"GIT_AUTHOR_EMAIL=tester@test",
	"GIT_COMMITTER_NAME=tester",
	"GIT_COMMITTER_EMAIL=tester@test",
}

// Command returns an exec.Cmd for the named program, with Env already applied.
//
// This is the way to reach for git from a test. A command built here cannot
// read the machine's configuration, where one built with exec.Command has to
// remember not to. The caller sets Dir, and may append to Env for anything it
// needs on top: later entries win.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = Env()

	return cmd
}

// Env returns the process environment with the defaults above applied, plus any
// extra "KEY=value" entries the caller needs.
//
// A default is applied only where the variable is not already set, so a caller
// that means to control one of these — CI pinning an identity, or pointing
// GIT_CONFIG_GLOBAL at a fixture — keeps it. Setting a variable to the empty
// string counts as setting it. Entries in extra are appended last and so
// override everything, including an inherited value.
func Env(extra ...string) []string {
	env := os.Environ()
	for _, kv := range defaults {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := os.LookupEnv(key); !ok {
			env = append(env, kv)
		}
	}

	return append(env, extra...)
}
