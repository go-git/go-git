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
func TestHasVolumeNameMatchesStdlib(t *testing.T) {
	t.Parallel()

	corpus := generateComponents(t, `./\?:a1%`, 5)
	corpus = append(corpus, `\\host\share`, `\\.\UNC\h\s`, `\??\C:\x`, `\\?\C:\x`, "ä:foo")

	for _, p := range corpus {
		require.Equal(t, filepath.VolumeName(p) != "", HasVolumeName(p), "path %q", p)
	}
}
