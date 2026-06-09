package commitgraph

import (
	"path"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	commitgraphfmt "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
)

var benchHead = plumbing.NewHash("b9d69064b190e7aedccf84731ca1d917871f8a1c")

func benchObjectIndex(b *testing.B) (CommitNodeIndex, func()) {
	b.Helper()
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)
	return NewObjectCommitNodeIndex(storer), func() { _ = storer.Close() }
}

func benchGraphIndex(b *testing.B) (CommitNodeIndex, func()) {
	b.Helper()
	f := fixtures.ByTag("commit-graph").One()
	storer := unpackRepository(f)
	reader, err := storer.Filesystem().Open(path.Join("objects", "info", "commit-graph"))
	if err != nil {
		b.Fatal(err)
	}
	index, err := commitgraphfmt.OpenFileIndex(reader)
	if err != nil {
		b.Fatal(err)
	}
	return NewGraphCommitNodeIndex(index, storer), func() {
		_ = index.Close()
		_ = reader.Close()
		_ = storer.Close()
	}
}

func drain(b *testing.B, iter CommitNodeIter) {
	b.Helper()
	n := 0
	err := iter.ForEach(func(c CommitNode) error {
		n += len(c.ID().String())
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	if n == 0 {
		b.Fatal("walked no commits")
	}
}

type walkerFn func(CommitNode, map[plumbing.Hash]bool, []plumbing.Hash) CommitNodeIter

var walkers = []struct {
	name string
	fn   walkerFn
}{
	{"CTime", NewCommitNodeIterCTime},
	{"DateOrder", NewCommitNodeIterDateOrder},
	{"TopoOrder", NewCommitNodeIterTopoOrder},
	{"AuthorOrder", NewCommitNodeIterAuthorDateOrder},
}

func BenchmarkObjectIndexGet(b *testing.B) {
	idx, cleanup := benchObjectIndex(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := idx.Get(benchHead); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkObjectIndexWalk(b *testing.B) {
	idx, cleanup := benchObjectIndex(b)
	defer cleanup()

	for _, w := range walkers {
		b.Run(w.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				head, err := idx.Get(benchHead)
				if err != nil {
					b.Fatal(err)
				}
				drain(b, w.fn(head, nil, nil))
			}
		})
	}
}

func BenchmarkGraphIndexWalk(b *testing.B) {
	idx, cleanup := benchGraphIndex(b)
	defer cleanup()

	for _, w := range walkers {
		b.Run(w.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				head, err := idx.Get(benchHead)
				if err != nil {
					b.Fatal(err)
				}
				drain(b, w.fn(head, nil, nil))
			}
		})
	}
}
