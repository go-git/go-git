package git

import (
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// BenchmarkWritePackfile benchmarks the client-side pack reception path:
// raw packfile bytes are written through PackfileWriter into filesystem
// storage, which concurrently builds the pack index. This isolates the
// memory-intensive buildIndex phase from transport and server overhead.
func BenchmarkWritePackfile(b *testing.B) {
	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()

	b.ReportAllocs()
	for b.Loop() {
		fs := osfs.New(b.TempDir())
		storage := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{})

		pf, err := f.Packfile()
		require.NoError(b, err)

		err = packfile.WritePackfileToObjectStorage(storage, pf)
		require.NoError(b, err)
	}
}

// BenchmarkWritePackfileHighMemory benchmarks the same path with
// HighMemoryMode enabled to provide a baseline comparison.
func BenchmarkWritePackfileHighMemory(b *testing.B) {
	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()

	b.ReportAllocs()
	for b.Loop() {
		fs := osfs.New(b.TempDir())
		storage := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{
			HighMemoryMode: true,
		})

		pf, err := f.Packfile()
		require.NoError(b, err)

		err = packfile.WritePackfileToObjectStorage(storage, pf)
		require.NoError(b, err)
	}
}
