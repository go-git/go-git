package pathutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHasVolumeNameMatchesStdlib pins the port against the real
// filepath.VolumeName over a closed corpus. It can only run here:
// off Windows the stdlib answers for the host it was compiled for,
// so the comparison would pass for a predicate that always said no.
//
// Candidate volume names holding a ".." component are left out: the
// stdlib answer for those depends on the toolchain, and go-git builds
// on both sides of the change. See volumeNameHasDotDot.
func TestHasVolumeNameMatchesStdlib(t *testing.T) {
	t.Parallel()

	corpus := generateComponents(t, `./\?:a1%`, 5)
	corpus = append(corpus, `\\host\share`, `\\.\UNC\h\s`, `\??\C:\x`, `\\?\C:\x`, "ä:foo")

	for _, p := range corpus {
		if volumeNameHasDotDot(p) {
			continue
		}
		require.Equal(t, filepath.VolumeName(p) != "", HasVolumeName(p), "path %q", p)
	}
}

// volumeNameHasDotDot reports whether the volume name this package
// reads off p holds a ".." component, such as `\\..` or `//a/../b`.
//
// Go 1.27 reports no volume name for those, so filepath.Clean reduces
// the parent components lexically; Go 1.26 reports one. go-git builds
// on both, so no single answer matches the stdlib everywhere, and this
// package keeps the Go 1.26 one.
//
// Reference: golang/go#58451, fixed by CL 788701 in Go 1.27[1][2].
//
// [1]: https://github.com/golang/go/issues/58451
// [2]: https://go-review.googlesource.com/c/go/+/788701
func volumeNameHasDotDot(p string) bool {
	for v := p[:volumeNameLen(p)]; v != ""; {
		var part string
		part, v, _ = cutPath(v)
		if part == ".." {
			return true
		}
	}
	return false
}
