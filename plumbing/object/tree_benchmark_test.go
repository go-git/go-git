package object

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
)

// createTestTreeObject creates a synthetic tree object for benchmarking
func createTestTreeObject(numEntries int) *plumbing.MemoryObject {
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.TreeObject)

	w, _ := obj.Writer()
	for i := range numEntries {
		// Write mode
		w.Write([]byte("100644 "))
		// Write name
		name := make([]byte, 0, 16)
		name = fmt.Appendf(name, "file%03d", i)
		w.Write(name)
		w.Write([]byte{0})
		// Write hash (20 bytes)
		hash := plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c")
		hash.WriteTo(w)
	}
	w.Close()

	return obj
}

// BenchmarkTreeDecode benchmarks the decoding of tree objects with varying sizes
func BenchmarkTreeDecode(b *testing.B) {
	tests := []struct {
		name       string
		numEntries int
	}{
		{"Small/1entry", 1},
		{"Medium/8entries", 8},
		{"Large/100entries", 100},
		{"VeryLarge/1000entries", 1000},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			obj := createTestTreeObject(tt.numEntries)
			b.ReportAllocs()

			for b.Loop() {
				tree := &Tree{}
				err := tree.Decode(obj)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMultipleTreeDecodes simulates a Clone operation pattern
func BenchmarkMultipleTreeDecodes(b *testing.B) {
	// Create multiple tree objects
	objects := []*plumbing.MemoryObject{
		createTestTreeObject(5),
		createTestTreeObject(10),
		createTestTreeObject(20),
		createTestTreeObject(8),
		createTestTreeObject(15),
	}

	b.ReportAllocs()

	for b.Loop() {
		for _, obj := range objects {
			tree := &Tree{}
			tree.Decode(obj)
		}
	}
}
