package commitgraph

import (
	"path"
	"runtime"
	"strconv"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	commitgraphfmt "github.com/go-git/go-git/v6/plumbing/format/commitgraph"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
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
		_ = storer.Close()
		b.Fatal(err)
	}
	index, err := commitgraphfmt.OpenFileIndex(reader)
	if err != nil {
		_ = reader.Close()
		_ = storer.Close()
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

func BenchmarkObjectIndexFullCommit(b *testing.B) {
	idx, cleanup := benchObjectIndex(b)
	defer cleanup()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node, err := idx.Get(benchHead)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := node.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeCommitBody(b *testing.B) {
	for _, size := range []int{1024, 1024 * 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			s := memory.NewStorage()
			obj := &plumbing.MemoryObject{}
			obj.SetType(plumbing.CommitObject)
			_, err := obj.Write([]byte("tree " + treeHex + "\nauthor A <a> 1 +0000\ncommitter B <b> 2 +0000\n\n" + strings.Repeat("m", size)))
			if err != nil {
				b.Fatal(err)
			}
			id, err := s.SetEncodedObject(obj)
			if err != nil {
				b.Fatal(err)
			}
			idx := NewObjectCommitNodeIndex(s)
			b.Run("Traversal", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := idx.Get(id); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("FullCommit", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := object.GetCommit(s, id); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkRetainedCommitNodes(b *testing.B) {
	const count = 128
	s := memory.NewStorage()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	_, err := obj.Write([]byte("tree " + treeHex + "\nauthor A <a> 1 +0000\ncommitter B <b> 2 +0000\n\n" + strings.Repeat("m", 64*1024)))
	if err != nil {
		b.Fatal(err)
	}
	id, err := s.SetEncodedObject(obj)
	if err != nil {
		b.Fatal(err)
	}
	idx := NewObjectCommitNodeIndex(s)
	for _, full := range []bool{false, true} {
		name := "Traversal"
		if full {
			name = "FullCommit"
		}
		b.Run(name, func(b *testing.B) {
			var retained uint64
			for i := 0; i < b.N; i++ {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				nodes := make([]any, count)
				for j := range nodes {
					node, err := idx.Get(id)
					if err != nil {
						b.Fatal(err)
					}
					nodes[j] = node
					if full {
						nodes[j], err = node.Commit()
						if err != nil {
							b.Fatal(err)
						}
					}
				}
				runtime.GC()
				runtime.ReadMemStats(&after)
				runtime.KeepAlive(nodes)
				runtime.KeepAlive(idx)
				if after.HeapAlloc > before.HeapAlloc {
					retained += after.HeapAlloc - before.HeapAlloc
				}
			}
			b.ReportMetric(float64(retained)/float64(b.N*count), "retained-B/node")
		})
	}
}
