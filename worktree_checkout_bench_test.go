package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func setupAutoCRLFCheckoutRepo(b *testing.B, numFiles, fileSize int) (files []*object.File, cfg *config.Config) {
	b.Helper()

	sourceDir := filepath.Join(b.TempDir(), "source")
	sourceRepo, err := PlainInit(sourceDir, false)
	require.NoError(b, err)
	b.Cleanup(func() { _ = sourceRepo.Close() })

	sourceWt, err := sourceRepo.Worktree()
	require.NoError(b, err)

	content := make([]byte, 0, fileSize)
	for len(content) < fileSize {
		content = append(content, []byte("line of text content for autocrlf benchmark\n")...)
	}
	content = content[:fileSize]

	for i := range numFiles {
		filePath := filepath.Join("dir", fmt.Sprintf("file%04d.txt", i))
		require.NoError(b, sourceWt.filesystem.MkdirAll(filepath.Dir(filePath), 0o755))
		require.NoError(b, util.WriteFile(sourceWt.filesystem, filePath, content, 0o644))
	}
	require.NoError(b, sourceWt.AddGlob("dir/*"))

	sig := &object.Signature{Name: "Bench", Email: "bench@test.com", When: time.Now()}
	_, err = sourceWt.Commit("initial", &CommitOptions{Author: sig, Committer: sig})
	require.NoError(b, err)

	head, err := sourceRepo.Head()
	require.NoError(b, err)
	commit, err := sourceRepo.CommitObject(head.Hash())
	require.NoError(b, err)
	tree, err := commit.Tree()
	require.NoError(b, err)

	require.NoError(b, tree.Files().ForEach(func(f *object.File) error {
		files = append(files, f)
		return nil
	}))

	cfg = config.NewConfig()
	cfg.Core.AutoCRLF = "true"
	return files, cfg
}

func checkoutAutoCRLFBench(b *testing.B, files []*object.File, cfg *config.Config) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		fs := memfs.New()
		for _, f := range files {
			dst, err := fs.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			require.NoError(b, err)
			require.NoError(b, (&Worktree{}).copyObjectToWorktree(cfg, f, dst))
			require.NoError(b, dst.Close())
		}
	}
}

func BenchmarkCheckoutAutoCRLF(b *testing.B) {
	files, cfg := setupAutoCRLFCheckoutRepo(b, 200, 256<<10)
	checkoutAutoCRLFBench(b, files, cfg)
}

func BenchmarkCheckoutAutoCRLFLarge(b *testing.B) {
	files, cfg := setupAutoCRLFCheckoutRepo(b, 8, 4<<20)
	checkoutAutoCRLFBench(b, files, cfg)
}
