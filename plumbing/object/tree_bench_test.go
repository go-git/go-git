package object

import "testing"

func BenchmarkTreeFindEntry(b *testing.B) {
	tree := &Tree{Entries: []TreeEntry{{Name: "readme.md"}}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := tree.FindEntry("readme.md"); err != nil {
			b.Fatal(err)
		}
	}
}
