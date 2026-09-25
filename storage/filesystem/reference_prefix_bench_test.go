package filesystem_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func writeBenchRefs(b *testing.B, packed bool) string {
	b.Helper()
	dir := b.TempDir()
	const hash = "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"

	names := make([]string, 0, 6000)
	for i := range 4000 {
		names = append(names, fmt.Sprintf("refs/heads/branch-%04d", i))
	}
	for i := range 1000 {
		names = append(names, fmt.Sprintf("refs/remotes/origin/branch-%04d", i))
		names = append(names, fmt.Sprintf("refs/tags/v%04d", i))
	}

	require.NoError(b, os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/branch-0000\n"), 0o644))
	if packed {
		slices.Sort(names)
		var sb strings.Builder
		sb.WriteString("# pack-refs with: peeled fully-peeled sorted \n")
		for _, name := range names {
			sb.WriteString(hash + " " + name + "\n")
		}
		require.NoError(b, os.WriteFile(filepath.Join(dir, "packed-refs"), []byte(sb.String()), 0o644))
		return dir
	}

	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(b, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(b, os.WriteFile(path, []byte(hash+"\n"), 0o644))
	}
	return dir
}

// "First" measures checking whether a remote has any tracking refs;
// "All" measures listing them.
func BenchmarkIterReferencesWithPrefix(b *testing.B) {
	const prefix = "refs/remotes/origin/"
	filtered := func(s storer.ReferenceStorer) (storer.ReferenceIter, error) {
		iter, err := s.IterReferences()
		if err != nil {
			return nil, err
		}
		return storer.NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
			return strings.HasPrefix(r.Name().String(), prefix)
		}, iter), nil
	}
	withPrefix := func(s storer.ReferenceStorer) (storer.ReferenceIter, error) {
		return storer.IterReferencesWithPrefix(s, prefix)
	}

	for _, layout := range []struct {
		name   string
		packed bool
	}{{"Loose", false}, {"Packed", true}} {
		dir := writeBenchRefs(b, layout.packed)
		sto := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
		b.Cleanup(func() { _ = sto.Close() })

		for _, method := range []struct {
			name string
			iter func(storer.ReferenceStorer) (storer.ReferenceIter, error)
		}{{"Filtered", filtered}, {"Prefix", withPrefix}} {
			b.Run(layout.name+"/"+method.name+"/First", func(b *testing.B) {
				for b.Loop() {
					iter, err := method.iter(sto)
					require.NoError(b, err)
					_, err = iter.Next()
					require.NoError(b, err)
					iter.Close()
				}
			})
			b.Run(layout.name+"/"+method.name+"/All", func(b *testing.B) {
				for b.Loop() {
					iter, err := method.iter(sto)
					require.NoError(b, err)
					n := 0
					require.NoError(b, iter.ForEach(func(*plumbing.Reference) error {
						n++
						return nil
					}))
					require.Equal(b, 1000, n)
				}
			})
		}
	}
}
