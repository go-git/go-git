package pathutil

import (
	"strings"
	"testing"
)

func BenchmarkValidTreePath(b *testing.B) {
	paths := map[string]string{
		"single": "readme.md",
		"nested": "src/module/pkg/file.go",
		"deep":   strings.Repeat("modules/", 16) + "file.go",
	}
	for name, path := range paths {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := ValidTreePath(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
