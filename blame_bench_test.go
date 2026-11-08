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
//
// Both report the number of commit, tree and blob lookups a Blame call
// makes, which, unlike its duration, does not depend on the machine.
func benchmarkBlame(b *testing.B, open func() (storer.EncodedObjectStorer, error), rev plumbing.Hash, path string) {
	// Count the lookups in a separate Blame call, so the timed runs read from
	// the storage directly.
	s, err := open()
	if err != nil {
		b.Fatal(err)
	}

	lookups := make(blameObjectLookups)
	c, err := object.GetCommit(&countingObjectStorer{EncodedObjectStorer: s, lookups: lookups}, rev)
	if err != nil {
		b.Fatal(err)
	}

	// Only count what Blame looks up, not the commit it starts from.
	clear(lookups)
	_, err = Blame(c, path)
	closeBlameBenchmarkStorage(b, s)
	if err != nil {
		b.Fatal(err)
	}

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

		lookups.report(b)
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

		lookups.report(b)
	})
}

// blameObjectLookups counts the object lookups of a Blame call, by type.
type blameObjectLookups map[plumbing.ObjectType]int

// report reports the counts as metrics of b. It must be called after the
// benchmark loop, as starting the loop resets the timer, which discards
// reported metrics.
func (l blameObjectLookups) report(b *testing.B) {
	b.ReportMetric(float64(l[plumbing.CommitObject]), "commits/op")
	b.ReportMetric(float64(l[plumbing.TreeObject]), "trees/op")
	b.ReportMetric(float64(l[plumbing.BlobObject]), "blobs/op")
}

// countingObjectStorer is an EncodedObjectStorer that counts the lookups made
// through it. It sits above the storage's object cache, so a lookup counts
// whether or not the cache serves it, and again each time the same object is
// looked up.
type countingObjectStorer struct {
	storer.EncodedObjectStorer
	lookups blameObjectLookups
}

func (s *countingObjectStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.EncodedObjectStorer.EncodedObject(t, h)
	if err == nil {
		s.lookups[obj.Type()]++
	}

	return obj, err
}

// closeBlameBenchmarkStorage closes s, if it can be closed.
func closeBlameBenchmarkStorage(b *testing.B, s storer.EncodedObjectStorer) {
	if closer, ok := s.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
