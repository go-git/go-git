package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

// BenchmarkAlternatesObjectLookup measures object lookup performance when using
// alternates. This benchmark tests the improvement from caching alternate
// ObjectStorage instances.
func BenchmarkAlternatesObjectLookup(b *testing.B) {
	// Setup: Create a shared clone using alternates
	// Note: We can't use PlainClone with Shared:true here due to import cycle
	// (repository.go imports storage/filesystem), so we set up alternates manually.
	baseDir := b.TempDir()

	templateFs := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return baseDir }))

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	if err := os.MkdirAll(alternatesDir, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alternatesDir, "alternates"),
		[]byte(templateFs.Root()+"/objects\n"), 0o644); err != nil {
		b.Fatal(err)
	}

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	if err != nil {
		b.Fatal(err)
	}
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())
	b.Cleanup(func() { storage.Close() })

	commitHashes := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	}

	b.ReportAllocs()
	b.Run("EncodedObject", func(b *testing.B) {
		for b.Loop() {
			for _, hash := range commitHashes {
				if _, err := storage.EncodedObject(plumbing.AnyObject, hash); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("HasEncodedObject", func(b *testing.B) {
		for b.Loop() {
			for _, hash := range commitHashes {
				if err := storage.HasEncodedObject(hash); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("EncodedObjectSize", func(b *testing.B) {
		for b.Loop() {
			for _, hash := range commitHashes {
				if _, err := storage.EncodedObjectSize(hash); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
