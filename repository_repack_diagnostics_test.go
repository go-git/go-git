package git

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

// diagnoseRepack preserves failed writes outside t.TempDir so that they survive
// test cleanup. Set GO_GIT_REPACK_DIAGNOSTICS to an artifact directory in CI.
// This fixture uses SHA-1; the independent check deliberately uses crypto/sha1
// rather than the hash implementation shared by the pack encoder and scanner.
func diagnoseRepack(t *testing.T, fs billy.Filesystem) []string {
	t.Helper()

	entries, err := fs.ReadDir("objects/pack")
	if err != nil {
		t.Logf("repack diagnostics: list packs: %v", err)
		return nil
	}

	var paths []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "tmp_pack_") {
			continue
		}
		data, err := util.ReadFile(fs, fs.Join("objects/pack", entry.Name()))
		if err != nil {
			t.Logf("repack diagnostics: read %s: %v", entry.Name(), err)
			continue
		}

		if len(data) >= sha1.Size {
			payload, footer := data[:len(data)-sha1.Size], data[len(data)-sha1.Size:]
			sum := sha1.Sum(payload)
			t.Logf("repack diagnostics: %s: bytes=%d sha1=%x footer=%x match=%t",
				entry.Name(), len(data), sum, footer, bytes.Equal(sum[:], footer))
		} else {
			t.Logf("repack diagnostics: %s: truncated (%d bytes)", entry.Name(), len(data))
		}

		scanner := packfile.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
		}
		t.Logf("repack diagnostics: %s: completed-file scanner error: %v", entry.Name(), scanner.Error())

		root := os.Getenv("GO_GIT_REPACK_DIAGNOSTICS")
		if root != "" {
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Logf("repack diagnostics: create artifact root: %v", err)
				continue
			}
		}
		dir, err := os.MkdirTemp(root, "go-git-repack-")
		if err != nil {
			t.Logf("repack diagnostics: create artifact directory: %v", err)
			continue
		}
		name := filepath.Join(dir, "failed.pack")
		if err := os.WriteFile(name, data, 0o600); err != nil {
			t.Logf("repack diagnostics: save pack: %v", err)
			continue
		}
		t.Logf("repack diagnostics: preserved %s; verify independently with: git index-pack %q", name, name)
		paths = append(paths, name)
	}
	return paths
}

func TestDiagnoseRepack(t *testing.T) {
	t.Setenv("GO_GIT_REPACK_DIAGNOSTICS", t.TempDir())

	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupt=%t", corrupt), func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			data := make([]byte, 12+sha1.Size)
			copy(data, []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 0})
			sum := sha1.Sum(data[:12])
			copy(data[12:], sum[:])
			if corrupt {
				data[len(data)-1] ^= 1
			}
			require.NoError(t, util.WriteFile(fs, "objects/pack/tmp_pack_test", data, 0o600))
			require.NoError(t, util.WriteFile(fs, "objects/pack/pack-existing.pack", data, 0o600))

			paths := diagnoseRepack(t, fs)
			require.Len(t, paths, 1)
			got, err := os.ReadFile(paths[0])
			require.NoError(t, err)
			require.Equal(t, data, got)
		})
	}
}
