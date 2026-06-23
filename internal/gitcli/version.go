// Package gitcli provides small helpers for tests that shell out to the
// system git binary. It intentionally depends only on the standard library so
// it can be imported from any transport test package without creating an
// import cycle.
package gitcli

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// SkipIfProtocolV2Unsupported skips the test unless a git new enough to
// advertise wire protocol v2 is available on PATH. Protocol v2 was introduced
// in Git 2.18; older servers (the CI matrix still exercises Git 2.11) only
// speak v0/v1, so tests asserting v2 negotiation cannot pass against them.
func SkipIfProtocolV2Unsupported(t testing.TB) {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		t.Skipf("cannot determine git version: %v", err)
	}
	if !supportsProtocolV2(string(out)) {
		t.Skipf("git too old for protocol v2: %s", strings.TrimSpace(string(out)))
	}
}

// supportsProtocolV2 reports whether a "git version X.Y.Z" string denotes a
// Git that advertises wire protocol v2 (Git >= 2.18).
func supportsProtocolV2(version string) bool {
	fields := strings.Fields(version)
	if len(fields) < 3 {
		return false
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 2 || (major == 2 && minor >= 18)
}
