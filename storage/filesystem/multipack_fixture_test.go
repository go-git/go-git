package filesystem

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/storage/memory"
)

// makeMultiPackFixture builds a fresh .git directory containing
// nPacks independent packfiles, each holding objPerPack distinct
// blob objects. Returns the filesystem rooted at .git and a slice
// of per-pack hash lists so callers can construct workloads that
// target specific access patterns (e.g. read one hash from each
// pack to exercise MRU pack ordering, or read a working set
// larger than the FD pool capacity to force eviction churn).
//
// Object content is deterministic per (pack, index) and globally
// unique across the fixture; deltas are disabled at encode time
// so every object resolves with a single offset lookup.
//
// The fixture is built using only the public DotGit + go-git
// internal APIs; no production code paths are touched.
func makeMultiPackFixture(tb testing.TB, nPacks, objPerPack int) (billy.Filesystem, [][]plumbing.Hash) {
	tb.Helper()

	diskFS := osfs.New(tb.TempDir())
	dg := dotgit.New(diskFS)
	if err := dg.Initialize(); err != nil {
		tb.Fatalf("initialize dotgit: %v", err)
	}

	perPack := make([][]plumbing.Hash, nPacks)
	for p := range nPacks {
		// Stage this pack's objects in an in-memory storage so the
		// on-disk repo only ends up with packs (no stray loose
		// objects from the staging step).
		stage := memory.NewStorage()
		hashes := make([]plumbing.Hash, objPerPack)
		for i := range objPerPack {
			o := stage.NewEncodedObject()
			o.SetType(plumbing.BlobObject)
			w, err := o.Writer()
			if err != nil {
				tb.Fatalf("object writer: %v", err)
			}
			// Padding ensures each object has non-trivial body so
			// the pack has realistic per-entry overhead and the
			// idx fanout buckets distribute reasonably.
			content := fmt.Sprintf("pack-%d-obj-%d-%s", p, i, strings.Repeat("x", 64))
			if _, err := w.Write([]byte(content)); err != nil {
				tb.Fatalf("write content: %v", err)
			}
			if err := w.Close(); err != nil {
				tb.Fatalf("close writer: %v", err)
			}
			h, err := stage.SetEncodedObject(tb.Context(), o)
			if err != nil {
				tb.Fatalf("stage object: %v", err)
			}
			hashes[i] = h
		}
		perPack[p] = hashes

		// Encode the staged objects into a pack stream. window=0
		// disables delta compression — bench fixtures want every
		// read to resolve in a single offset lookup.
		var buf bytes.Buffer
		enc := packfile.NewEncoder(tb.Context(), &buf, stage, false)
		if _, err := enc.Encode(tb.Context(), hashes, 0); err != nil {
			tb.Fatalf("encode pack %d: %v", p, err)
		}

		// Stream the encoded pack into the on-disk repo via the
		// PackWriter; this writes both the .pack and the .idx,
		// matching what `git index-pack` produces.
		pw, err := dg.NewObjectPack()
		if err != nil {
			tb.Fatalf("new object pack %d: %v", p, err)
		}
		if _, err := io.Copy(pw, &buf); err != nil {
			tb.Fatalf("copy pack %d: %v", p, err)
		}
		if err := pw.Close(); err != nil {
			tb.Fatalf("close pack %d: %v", p, err)
		}
	}

	return diskFS, perPack
}
