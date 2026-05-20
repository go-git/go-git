package filesystem

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

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

	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(
		fixtures.WithTargetDir(func() string { return baseDir }))
	if err != nil {
		b.Fatal(err)
	}

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

// BenchmarkObjectStorage_PackHandle is the Tier-1 headline benchmark for the
// PackHandle feature. It proves three user-visible claims:
//
//  1. PackHandle default beats per-call open/close at any goroutine count.
//  2. Concurrent reads scale: parallel sustained throughput tracks near
//     serial throughput × NumCPU.
//  3. mmap multiplies the win on read-heavy access once the pack is in the
//     page cache.
//
// Sub-benchmark matrix: 2 workloads × 2 concurrency levels × 2 FS modes = 8.
//
// Uses the default LRU object cache — this benchmark measures realistic
// storage throughput where the application cache is part of the path. For
// pack-FD-only attribution see
// internal/packhandle/packhandle_bench_test.go.
//
// The "main baseline" comparison is done with benchstat against
// origin/main, not via an inline branch — keeping a legacy code path in
// the same binary would require significant wiring for no correctness gain.
func BenchmarkObjectStorage_PackHandle(b *testing.B) {
	hashes := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	}

	// setupStorage materialises the basic fixture into a fresh b.TempDir(),
	// wraps it with an osfs (mmap or fd-only), and returns a ready
	// ObjectStorage. A fresh dir per sub-benchmark isolates page-cache
	// state between rows. The pack is read end-to-end once before
	// b.ResetTimer() so that first-touch page faults are paid outside the
	// timed region. A serial warm-up pass over all bench hashes builds the
	// index and primes the PackHandle cache before concurrent goroutines
	// start, avoiding a thundering-herd race on first-time setup.
	setupStorage := func(b *testing.B, withMmap bool) *ObjectStorage {
		b.Helper()
		dir := b.TempDir()
		_, err := fixtures.Basic().ByTag(".git").One().DotGit(
			fixtures.WithTargetDir(func() string { return dir }))
		if err != nil {
			b.Fatalf("fixture DotGit: %v", err)
		}

		var fs billy.Filesystem
		if withMmap {
			fs = osfs.New(dir, osfs.WithMmap())
		} else {
			fs = osfs.New(dir)
		}

		dg := dotgit.New(fs)
		stor := NewObjectStorage(dg, cache.NewObjectLRUDefault())
		b.Cleanup(func() { _ = stor.Close() })

		benchWarmPack(b, fs)

		// Serial warm-up: build the pack index and prime the PackHandle
		// FD pool before the timed parallel region starts.
		for _, h := range hashes {
			if _, err := stor.EncodedObject(plumbing.AnyObject, h); err != nil {
				b.Fatalf("setupStorage warm-up: %v", err)
			}
		}
		return stor
	}

	modes := []struct {
		name string
		mmap bool
	}{
		{"fd", false},
		{"mmap", true},
	}

	for _, mode := range modes {
		b.Run("EncodedObject/G=1/"+mode.name, func(b *testing.B) {
			stor := setupStorage(b, mode.mmap)
			b.ReportAllocs()
			b.ResetTimer()
			var i int
			for b.Loop() {
				if _, err := stor.EncodedObject(
					plumbing.AnyObject,
					hashes[i%len(hashes)],
				); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})

		b.Run("EncodedObject/G=NumCPU/"+mode.name, func(b *testing.B) {
			stor := setupStorage(b, mode.mmap)
			// b.RunParallel defaults to GOMAXPROCS = NumCPU goroutines, matching
			// the G=NumCPU label.
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				var i int
				for pb.Next() {
					if _, err := stor.EncodedObject(
						plumbing.AnyObject,
						hashes[i%len(hashes)],
					); err != nil {
						b.Fatalf("iteration %d hash %s: %v", i, hashes[i%len(hashes)], err)
					}
					i++
				}
			})
		})

		b.Run("HasEncodedObject/G=1/"+mode.name, func(b *testing.B) {
			stor := setupStorage(b, mode.mmap)
			b.ReportAllocs()
			b.ResetTimer()
			var i int
			for b.Loop() {
				if err := stor.HasEncodedObject(hashes[i%len(hashes)]); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})

		b.Run("HasEncodedObject/G=NumCPU/"+mode.name, func(b *testing.B) {
			stor := setupStorage(b, mode.mmap)
			// b.RunParallel defaults to GOMAXPROCS = NumCPU goroutines, matching
			// the G=NumCPU label.
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				var i int
				for pb.Next() {
					if err := stor.HasEncodedObject(hashes[i%len(hashes)]); err != nil {
						b.Fatal(err)
					}
					i++
				}
			})
		})
	}
}

// benchWarmPack reads the pack file in the given filesystem end-to-end
// once to populate the OS page cache before the timed benchmark region
// begins. This ensures mmap first-touch faults are paid outside the loop.
func benchWarmPack(b *testing.B, fs billy.Filesystem) {
	b.Helper()
	entries, err := fs.ReadDir("objects/pack")
	if err != nil {
		b.Fatalf("benchWarmPack ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".pack" {
			continue
		}
		f, err := fs.Open(filepath.Join("objects/pack", e.Name()))
		if err != nil {
			b.Fatalf("benchWarmPack Open: %v", err)
		}
		buf := make([]byte, 64*1024)
		for {
			_, err := f.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = f.Close()
				b.Fatalf("benchWarmPack Read: %v", err)
			}
		}
		_ = f.Close()
		return // warm the first pack only
	}
}
