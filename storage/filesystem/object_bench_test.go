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
	"github.com/go-git/go-git/v6/x/fdpool"
)

// BenchmarkAlternatesObjectLookup measures object lookup performance
// when using alternates. Setup mirrors what PlainClone(Shared:true)
// produces in the public API: a work .git that points at a template
// via objects/info/alternates. We can't call PlainClone here due to
// an import cycle (repository.go imports storage/filesystem), so we
// build the alternate manually and construct a Storage via
// NewStorageWithOptions — the same path PlainClone takes. This is
// what wires the FD pool through to the alternate's PackHandles.
func BenchmarkAlternatesObjectLookup(b *testing.B) {
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
	storage := NewStorageWithOptions(workFs, cache.NewObjectLRUDefault(),
		Options{AlternatesFS: rootFs})
	b.Cleanup(func() { _ = storage.Close() })

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
				if _, err := storage.EncodedObject(b.Context(), plumbing.AnyObject, hash); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("HasEncodedObject", func(b *testing.B) {
		for b.Loop() {
			for _, hash := range commitHashes {
				if err := storage.HasEncodedObject(b.Context(), hash); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("EncodedObjectSize", func(b *testing.B) {
		for b.Loop() {
			for _, hash := range commitHashes {
				if _, err := storage.EncodedObjectSize(b.Context(), hash); err != nil {
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
			if _, err := stor.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
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
				if _, err := stor.EncodedObject(b.Context(),
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
					if _, err := stor.EncodedObject(b.Context(),
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
				if err := stor.HasEncodedObject(b.Context(), hashes[i%len(hashes)]); err != nil {
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
					if err := stor.HasEncodedObject(b.Context(), hashes[i%len(hashes)]); err != nil {
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

// BenchmarkObjectStorage_ColdEncodedObject measures end-to-end
// first-read latency on a freshly-constructed Storage against a
// multi-pack fixture. Each iteration pays the populateIndex
// cold-load (which loads every pack's idx in parallel via
// errgroup) plus the per-object decode for a single read.
//
// Storage construction is in the timed window because b.Loop
// forbids stopping the timer at iteration boundaries; the cost
// is bounded (a struct alloc + dotgit pointer setup, no I/O) and
// the multi-pack fixture ensures populateIndex is the dominant
// per-iteration cost rather than the decode.
//
// The parallel-vs-serial populateIndex win is a property of the
// code (errgroup.SetLimit(GOMAXPROCS)) — to quantify it use
// benchstat against a baseline recording on main; in-bench -cpu
// scaling is masked by the per-iteration decode cost.
func BenchmarkObjectStorage_ColdEncodedObject(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// Pick a hash from the last pack so the lookup actually
	// iterates s.packs rather than hitting whichever pack happens
	// to be first in the (randomised) map iteration order.
	target := perPack[nPacks-1][0]

	b.ReportAllocs()
	for b.Loop() {
		s := NewStorage(diskFS, cache.NewObjectLRUDefault())
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, target); err != nil {
			b.Fatal(err)
		}
		_ = s.Close()
	}
}

// BenchmarkObjectStorage_ReindexThenRead measures Reindex + read.
// Pre-fix: Reindex returns fast but the next read pays the cold
// bootstrap. Post-fix: Reindex pre-warms s.index synchronously so
// the next read is hot.
func BenchmarkObjectStorage_ReindexThenRead(b *testing.B) {
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	if err != nil {
		b.Fatal(err)
	}
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	b.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(b.Context(), plumbing.AnyObject)
	if err != nil {
		b.Fatal(err)
	}
	obj, err := iter.Next(b.Context())
	iter.Close()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := s.Reindex(); err != nil {
			b.Fatal(err)
		}
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, obj.Hash()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkObjectStorage_ParallelReads measures concurrent
// EncodedObject throughput. The packed read path takes muI as
// RLock so independent goroutines do not serialise on the lock.
// Run with -cpu=1,2,4,8 to see the throughput curve scale.
func BenchmarkObjectStorage_ParallelReads(b *testing.B) {
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	if err != nil {
		b.Fatal(err)
	}
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	b.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(b.Context(), plumbing.AnyObject)
	if err != nil {
		b.Fatal(err)
	}
	var hashes []plumbing.Hash
	for range 32 {
		obj, err := iter.Next(b.Context())
		if err != nil {
			break
		}
		hashes = append(hashes, obj.Hash())
	}
	iter.Close()
	if len(hashes) == 0 {
		b.Fatal("fixture must have objects")
	}

	if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, hashes[0]); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int64
		for pb.Next() {
			h := hashes[i%int64(len(hashes))]
			if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// BenchmarkStorage_PoolPressure measures end-to-end read latency
// at two pool capacities. DefaultCapacity holds every fixture FD
// warm; CapacityOne is the worst-case shape where pack, idx, and
// rev sharedFiles all contend for one LRU slot and FDs reopen on
// every read.
//
// The basic fixture is small enough that the object cache may
// serve hot reads without touching the FD layer; for a precise
// FD-churn measurement use a larger fixture or invalidate the
// cache between iterations.
// BenchmarkStorage_PoolPressure measures the FD-pool's effect on
// read latency across two regimes. A 32-pack fixture is read with
// a working set wide enough that the object cache cannot serve the
// hot loop: the bench uses cache.NewObjectLRU(0) so every read
// traverses the index and pack FD layers. Each sub-benchmark
// injects its own [*fdpool.Pool] and asserts against
// [fdpool.Stats.Evictions] at end-of-bench, guarding against
// silent regressions where the expected pool behaviour stops
// happening.
//
//	NoChurn  cap = 256, fixture FDs (~96) fit comfortably
//	Churn    cap =   8, every Touch evicts a member
//
// Comparing the two sub-benchmarks isolates the eviction +
// reopen tax. The bench fails loud if Churn does not actually
// evict, or NoChurn unexpectedly does — keeping the bench honest
// across future code paths.
func BenchmarkStorage_PoolPressure(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// Flatten into a working set spanning every pack so reads cycle
	// across all FDs and the pool's eviction policy is exercised
	// uniformly rather than against a hot subset.
	var hashes []plumbing.Hash
	for _, p := range perPack {
		hashes = append(hashes, p...)
	}

	run := func(b *testing.B, capacity int, wantChurn bool) {
		pool := fdpool.New(capacity)
		s := NewStorageWithOptions(diskFS, cache.NewObjectLRU(0), Options{
			Pool: pool,
		})
		b.Cleanup(func() { _ = s.Close() })

		// Warm-up: ensure every pack's idx + rev is loaded into
		// s.index so the timed loop is not paying a cold populate
		// on iteration 1.
		for _, h := range hashes {
			if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
				b.Fatal(err)
			}
		}
		startEvictions := pool.Stats().Evictions

		b.ReportAllocs()
		b.ResetTimer()
		var i int
		for b.Loop() {
			h := hashes[i%len(hashes)]
			if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
				b.Fatal(err)
			}
			i++
		}
		b.StopTimer()

		gotEvictions := pool.Stats().Evictions - startEvictions
		switch {
		case wantChurn && gotEvictions == 0:
			b.Fatalf("cap=%d ws=%d: expected pool churn, observed 0 evictions; "+
				"bench no longer exercises the pool", capacity, len(hashes))
		case !wantChurn && gotEvictions > 0:
			b.Fatalf("cap=%d ws=%d: expected steady-state, observed %d evictions; "+
				"working set should fit in pool", capacity, len(hashes), gotEvictions)
		}
	}

	b.Run("NoChurn", func(b *testing.B) { run(b, 256, false) })
	b.Run("Churn", func(b *testing.B) { run(b, 8, true) })
}

// BenchmarkStorage_PackHandleCacheContention exercises the
// [dotgit.DotGit] packHandlesMu by running NumCPU goroutines
// against a 32-pack fixture, with each goroutine cycling through
// every pack so independent reads still meet on the cache lock.
// The pool is sized generously so FD churn does not mask the
// lookup-mutex cost. Compared against
// `BenchmarkStorage_PackHandleCacheContention/Serial`, the
// per-iteration delta is the contended-mutex overhead a sharded
// cache would aim to remove.
//
// Run with `-cpu=1,2,4,8` to see how the contended path scales —
// if the parallel curve flattens early, packHandlesMu is the
// bottleneck and sharding the map is worth pursuing.
func BenchmarkStorage_PackHandleCacheContention(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	hashes := make([]plumbing.Hash, 0, nPacks*objPerPack)
	for _, p := range perPack {
		hashes = append(hashes, p...)
	}

	newStorage := func(b *testing.B) *Storage {
		b.Helper()
		s := NewStorageWithOptions(diskFS, cache.NewObjectLRU(0), Options{
			Pool: fdpool.New(256),
		})
		b.Cleanup(func() { _ = s.Close() })

		// Warm-up: populate the dotgit packHandles map and every
		// LazyIndex so the timed loop measures only the cache
		// lookup + read, not first-touch index loads.
		for _, h := range hashes {
			if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
				b.Fatal(err)
			}
		}
		return s
	}

	b.Run("Serial", func(b *testing.B) {
		s := newStorage(b)
		b.ReportAllocs()
		b.ResetTimer()
		var i int
		for b.Loop() {
			h := hashes[i%len(hashes)]
			if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		s := newStorage(b)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			var i int
			for pb.Next() {
				h := hashes[i%len(hashes)]
				if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})
}

// BenchmarkObjectStorage_FSObjectReader measures the FSObject
// Reader materialisation path (the regression vector for
// go-git#2153). Each iteration: EncodedObject lookup + Reader
// open + full Copy to io.Discard + Close, across fd vs mmap.
func BenchmarkObjectStorage_FSObjectReader(b *testing.B) {
	hashes := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"),
		plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a"),
		plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea"),
		plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
	}

	setup := func(b *testing.B, withMmap bool) *ObjectStorage {
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

		// Serial warm-up: build the index and prime FSObject caches.
		for _, h := range hashes {
			obj, err := stor.EncodedObject(b.Context(), plumbing.AnyObject, h)
			if err != nil {
				b.Fatalf("warm-up EncodedObject: %v", err)
			}
			r, err := obj.Reader()
			if err != nil {
				b.Fatalf("warm-up Reader: %v", err)
			}
			if _, err := io.Copy(io.Discard, r); err != nil {
				b.Fatalf("warm-up Copy: %v", err)
			}
			if err := r.Close(); err != nil {
				b.Fatalf("warm-up Close: %v", err)
			}
		}
		return stor
	}

	for _, mode := range []struct {
		name string
		mmap bool
	}{
		{"fd", false},
		{"mmap", true},
	} {
		b.Run("Reader/G=1/"+mode.name, func(b *testing.B) {
			stor := setup(b, mode.mmap)
			b.ReportAllocs()
			b.ResetTimer()
			var i int
			for b.Loop() {
				obj, err := stor.EncodedObject(b.Context(),
					plumbing.AnyObject, hashes[i%len(hashes)])
				if err != nil {
					b.Fatal(err)
				}
				r, err := obj.Reader()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, r); err != nil {
					b.Fatal(err)
				}
				if err := r.Close(); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	}
}

// BenchmarkFindObjectInPackfile_FanoutMiss measures the per-pack
// probe cost when the target hash lives in only one of N indexes.
// With MayContain in place, N-1 packs reject via a cheap fanout
// check; pre-fix, every pack pays a FindOffset call (mutex +
// ReadAt + binary search).
//
// Workload: cycle through one hash per pack so each lookup
// targets a different pack, defeating the MRU fast path. Reads
// use cache.NewObjectLRU(0) so the object cache cannot serve the
// hot loop and every iteration traverses the membership probe.
func BenchmarkFindObjectInPackfile_FanoutMiss(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// One hash per pack so consecutive iterations always switch
	// packs; MRU hint is invalidated on every read.
	hashes := make([]plumbing.Hash, nPacks)
	for i, p := range perPack {
		hashes[i] = p[0]
	}

	s := NewStorage(diskFS, cache.NewObjectLRU(0))
	b.Cleanup(func() { _ = s.Close() })

	// Warm: ensure every pack's idx is loaded so the timed loop
	// is not paying a cold populate on iteration 1.
	for _, h := range hashes {
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	var i int
	for b.Loop() {
		h := hashes[i%len(hashes)]
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkEncodedObject_LooseHit measures EncodedObject latency
// when the target lives in a loose object. With pack-membership-
// first, findObjectInPackfile reports the loose-only hash as
// not-in-packs in O(1) and the read routes straight to the loose
// path: one Stat + open + decompress, no wasted pack probing.
// Setup: a packed fixture plus one freshly-written loose object.
func BenchmarkEncodedObject_LooseHit(b *testing.B) {
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	if err != nil {
		b.Fatal(err)
	}
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	b.Cleanup(func() { _ = s.Close() })

	// Write a fresh loose object so the hit-path is observable.
	o := s.NewEncodedObject()
	o.SetType(plumbing.BlobObject)
	w, err := o.Writer()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := w.Write([]byte("loose-bench-payload")); err != nil {
		b.Fatal(err)
	}
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	h, err := s.SetEncodedObject(b.Context(), o)
	if err != nil {
		b.Fatal(err)
	}

	// Warm the index so the per-iteration call doesn't pay a
	// cold-load.
	if err := s.HasEncodedObject(b.Context(), h); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHashesWithPrefix_TwoByte measures the cost of a short
// prefix lookup against a multi-pack fixture. Pre-fix: O(N*M)
// walking every entry of every pack per call. Post-fix:
// O(N * (log M + matches)) via fanout-bounded EntriesWithPrefix
// — only the relevant fanout bucket is scanned in each pack.
//
// The 2-byte prefix is picked from a real hash so at least one
// pack returns a match; the remaining packs reject via empty
// fanout buckets.
func BenchmarkHashesWithPrefix_TwoByte(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 32
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// Use a hash from the last pack so the lookup walks every
	// pack's idx (no MRU short-circuit).
	prefix := perPack[nPacks-1][0].Bytes()[:2]

	s := NewStorage(diskFS, cache.NewObjectLRUDefault())
	b.Cleanup(func() { _ = s.Close() })

	// Warm the index so per-iteration cost excludes cold populate.
	if _, err := s.HashesWithPrefix(prefix); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.HashesWithPrefix(prefix); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFindObjectInPackfile_MRU_Hit reads hashes that all
// live in the same pack against a multi-pack fixture. With MRU,
// the lastHitPackIdx hint resolves the right pack in O(1) on the
// fast path; without MRU, the map iteration is randomised and
// the average lookup pays N/2 MayContain rejects before finding
// the right pack.
//
// Uses cache.NewObjectLRU(0) so the object cache cannot serve
// the hot loop — each iteration must traverse findObjectInPackfile.
func BenchmarkFindObjectInPackfile_MRU_Hit(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// All hashes in pack 0 — repeated reads keep the MRU hint
	// pointing at the same pack.
	hashes := perPack[0]

	s := NewStorage(diskFS, cache.NewObjectLRU(0))
	b.Cleanup(func() { _ = s.Close() })

	// Warm: seeds lastHitPackIdx on pack 0.
	for _, h := range hashes {
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	var i int
	for b.Loop() {
		h := hashes[i%len(hashes)]
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkFindObjectInPackfile_MRU_Churn measures the steady-state
// cost when reads cycle a working set of K hot packs in round-robin
// order. With a single-slot MRU hint, the hint is correct on 1 of
// every K iterations and stale on the rest; the stale-hint cost is
// one MayContain probe before the linear scan finds the right pack.
// Slots between MRU_Hit (K=1, perfect reuse) and FanoutMiss (K=N, no
// reuse) to make the hint's degradation curve visible to benchstat.
//
// K=4 was picked to match realistic GC-time pack counts where a
// reader walks a tree spread across a small hot set. Cache disabled
// (cache.NewObjectLRU(0)) so the object cache cannot serve the hot
// loop and every iteration traverses findObjectInPackfile.
func BenchmarkFindObjectInPackfile_MRU_Churn(b *testing.B) {
	const (
		nPacks     = 32
		objPerPack = 8
		hotPacks   = 4
	)
	diskFS, perPack := makeMultiPackFixture(b, nPacks, objPerPack)

	// One hash per hot pack so consecutive reads switch packs in a
	// fixed cycle; the MRU hint always trails by one position.
	hashes := make([]plumbing.Hash, hotPacks)
	for i := range hotPacks {
		hashes[i] = perPack[i][0]
	}

	s := NewStorage(diskFS, cache.NewObjectLRU(0))
	b.Cleanup(func() { _ = s.Close() })

	// Warm: ensure every hot pack's idx is loaded so the timed loop
	// is not paying a cold populate on the first cycle.
	for _, h := range hashes {
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	var i int
	for b.Loop() {
		h := hashes[i%hotPacks]
		if _, err := s.EncodedObject(b.Context(), plumbing.AnyObject, h); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
