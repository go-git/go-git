package gitenv

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// fixedDates pins the author and committer dates of the commits Run
// creates, so that their ids do not depend on when the test runs.
var fixedDates = []string{
	"GIT_AUTHOR_DATE=1700000000 +0000",
	"GIT_COMMITTER_DATE=1700000000 +0000",
}

// RequireAtLeast skips t when git is not in PATH, or is older than
// major.minor.
func RequireAtLeast(t testing.TB, major, minor int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH")
	}
	out, err := Command("git", "version").Output()
	if err != nil {
		t.Skipf("cannot run git version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Skipf("cannot parse git version %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	gotMajor, errMajor := strconv.Atoi(parts[0])
	gotMinor := 0
	if len(parts) > 1 {
		gotMinor, _ = strconv.Atoi(parts[1])
	}
	if errMajor != nil || gotMajor < major || (gotMajor == major && gotMinor < minor) {
		t.Skipf("git %s is older than %d.%d", fields[2], major, minor)
	}
}

// Run runs git with args in dir, through [Command], and returns its
// combined output without surrounding white space. Commits it creates
// have fixed dates. It fails t when git fails.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := Command("git", args...)
	if cmd.Err != nil {
		t.Fatalf("git %v: %v", args, cmd.Err)
	}
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, fixedDates...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
