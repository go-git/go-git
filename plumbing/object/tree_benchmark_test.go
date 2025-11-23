package object

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/utils/sync"
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

// BenchmarkTreeDecodeComponents benchmarks individual components of decode
func BenchmarkTreeDecodeComponents(b *testing.B) {
	obj := createTestTreeObject(8)

	b.Run("ReadString", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			reader, _ := obj.Reader()
			r := bufio.NewReader(reader)

			// Read mode
			_, _ = r.ReadString(' ')
			reader.Close()
		}
	})

	b.Run("ReadStringWithPool", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			reader, _ := obj.Reader()
			r := sync.GetBufioReader(reader)

			// Read mode
			_, _ = r.ReadString(' ')

			sync.PutBufioReader(r)
			reader.Close()
		}
	})

	b.Run("FileMode.New", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = filemode.New("100644")
		}
	})

	b.Run("HashReadFrom", func(b *testing.B) {
		hashBytes := []byte{
			0xa8, 0xd3, 0x15, 0xb2, 0xb1, 0xc6, 0x15, 0xd4, 0x30, 0x42,
			0xc3, 0xa6, 0x24, 0x02, 0xb8, 0xa5, 0x42, 0x88, 0xcf, 0x5c,
		}

		b.ReportAllocs()
		for b.Loop() {
			buf := bytes.NewReader(hashBytes)
			var hash plumbing.Hash
			hash.ReadFrom(buf)
		}
	})

	b.Run("EntryAppend", func(b *testing.B) {
		entries := make([]TreeEntry, 0, 10)
		entry := TreeEntry{
			Name: "test.txt",
			Mode: 0o100644,
			Hash: plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"),
		}

		b.ReportAllocs()
		for b.Loop() {
			entries = append(entries[:0], entry)
		}
	})

	b.Run("FullDecode", func(b *testing.B) {
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

// BenchmarkTreeDecodeAllocation tests memory allocation patterns
func BenchmarkTreeDecodeAllocation(b *testing.B) {
	obj := createTestTreeObject(8)

	b.Run("WithPreallocation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			tree := &Tree{}
			// Pre-allocate based on typical tree sizes
			tree.Entries = make([]TreeEntry, 0, 16)
			err := tree.Decode(obj)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("WithoutPreallocation", func(b *testing.B) {
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
