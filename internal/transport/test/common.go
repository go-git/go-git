package test

import (
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

// GitSupportsV2 reports whether the git binary in PATH supports protocol v2
// (git >= 2.18). Transports backed by the reference git server use it to skip
// the v2 suite run on older git, which would silently fall back to v0.
func GitSupportsV2() bool {
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out)) // e.g. "git version 2.39.2"
	if len(fields) < 3 {
		return false
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major > 2 || (major == 2 && minor >= 18)
}

// UploadPackVersions returns the protocol versions to run the upload-pack suite
// against. realGit indicates the server is the reference git implementation,
// whose protocol v2 support depends on the installed git version; v2 is dropped
// for such servers when the local git is too old.
func UploadPackVersions(realGit bool) []protocol.Version {
	versions := []protocol.Version{protocol.V0, protocol.V1}
	if !realGit || GitSupportsV2() {
		versions = append(versions, protocol.V2)
	}
	return versions
}

// FixturesFactory returns a function that creates a fixture path.
func FixturesFactory(base, name string) func() string {
	return func() string {
		return filepath.Join(base, name)
	}
}

// PrepareRepository creates a bare repository from a fixture.
func PrepareRepository(t testing.TB, f *fixtures.Fixture, base, name string) billy.Filesystem {
	fs, err := f.DotGit(fixtures.WithTargetDir(FixturesFactory(base, name)))
	require.NoError(t, err)
	err = fixtures.EnsureIsBare(fs)
	require.NoError(t, err)
	return fs
}

// FreePort returns an available TCP port on localhost.
func FreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}

	return l.Addr().(*net.TCPAddr).Port, l.Close()
}

// ListenTCP listens localhost:0.
// It reserves the listener to be closed on t.CleanUp.
func ListenTCP(t testing.TB) *net.TCPListener {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	t.Cleanup(func() {
		err := l.Close()
		if err != nil {
			require.ErrorIs(t, err, net.ErrClosed)
		}
	})

	return l.(*net.TCPListener)
}
