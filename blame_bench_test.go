package git

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// blameBenchmarks are the files blamed by BenchmarkBlame. Each one stresses a
// different part of the history walk.
var blameBenchmarks = []struct {
	name    string
	fixture string
	rev     string
	path    string
}{
	// A long script built up by many commits.
	{name: "spinnaker-script", fixture: "https://github.com/spinnaker/spinnaker.git", rev: "f39d86f59a0781f130e8de6b2115329c1fbe9545", path: "InstallSpinnaker.sh"},
	// A binary file, diffed as if it were text.
	{name: "spinnaker-binary", fixture: "https://github.com/spinnaker/spinnaker.git", rev: "f39d86f59a0781f130e8de6b2115329c1fbe9545", path: "gradle/wrapper/gradle-wrapper.jar"},
	// The most edited file in the repository.
	{name: "go-git-most-edited", fixture: "https://github.com/src-d/go-git.git", rev: "e8788ad9165781196e917292d6055cba1d78664e", path: "repository.go"},
	// Renamed from storage/seekable/storage.go, so the walk goes through
	// rename detection.
	{name: "go-git-renamed", fixture: "https://github.com/src-d/go-git.git", rev: "e8788ad9165781196e917292d6055cba1d78664e", path: "storage/filesystem/storage.go"},
	// Unchanged in the 186 commits after it was added, so most of the walk
	// finds nothing to blame.
	{name: "go-git-unchanged", fixture: "https://github.com/src-d/go-git.git", rev: "e8788ad9165781196e917292d6055cba1d78664e", path: "diff/diff.go"},
}

// BenchmarkBlame blames files from fixture repositories stored on disk, the
// way Blame is used on a cloned repository.
func BenchmarkBlame(b *testing.B) {
	root := b.TempDir()
	dirs := make(map[string]string)
	for _, t := range blameBenchmarks {
		b.Run(t.name, func(b *testing.B) {
			dir, ok := dirs[t.fixture]
			if !ok {
				// Write the fixture packfile to a repository on disk, the
				// first time a file of the fixture is blamed.
				dir = filepath.Join(root, t.name)
				p, err := fixtures.ByURL(t.fixture).One().Packfile()
				if err != nil {
					b.Fatal(err)
				}

				st := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
				err = packfile.UpdateObjectStorage(st, p)
				_ = p.Close()
				if err != nil {
					b.Fatal(err)
				}
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
				dirs[t.fixture] = dir
			}

			open := func() (storer.EncodedObjectStorer, error) {
				return filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault()), nil
			}
			benchmarkBlame(b, open, plumbing.NewHash(t.rev), t.path)
		})
	}
}

// benchmarkBlame blames path at rev, reading objects from the storage
// returned by open. It runs twice: "cold" opens a new storage for every
// iteration and times reading rev from it along with Blame, so loading the
// pack indexes and filling the object cache count towards the result, while
// "warm" reuses one storage across iterations.
func benchmarkBlame(b *testing.B, open func() (storer.EncodedObjectStorer, error), rev plumbing.Hash, path string) {
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			s, err := open()
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()

			c, err := object.GetCommit(s, rev)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := Blame(c, path); err != nil {
				b.Fatal(err)
			}

			b.StopTimer()
			closeBlameBenchmarkStorage(b, s)
			b.StartTimer()
		}
	})

	b.Run("warm", func(b *testing.B) {
		b.ReportAllocs()
		s, err := open()
		if err != nil {
			b.Fatal(err)
		}
		defer closeBlameBenchmarkStorage(b, s)

		c, err := object.GetCommit(s, rev)
		if err != nil {
			b.Fatal(err)
		}

		// Fill the caches before timing.
		if _, err := Blame(c, path); err != nil {
			b.Fatal(err)
		}

		for b.Loop() {
			if _, err := Blame(c, path); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// closeBlameBenchmarkStorage closes s, if it can be closed.
func closeBlameBenchmarkStorage(b *testing.B, s storer.EncodedObjectStorer) {
	if closer, ok := s.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
