package gitignore

import (
	"os"
	"testing"

	"gopkg.in/src-d/go-billy.v2"
	"gopkg.in/src-d/go-billy.v2/memfs"
)

func setupTestFS(subdirError bool) billy.Filesystem {
	fs := memfs.New()
	f, _ := fs.Create(".gitignore")
	f.Write([]byte("vendor/g*/\n"))
	f.Close()
	fs.MkdirAll("vendor", os.ModePerm)
	f, _ = fs.Create("vendor/.gitignore")
	f.Write([]byte("!github.com/\n"))
	f.Close()
	fs.MkdirAll("another", os.ModePerm)
	fs.MkdirAll("vendor/github.com", os.ModePerm)
	fs.MkdirAll("vendor/gopkg.in", os.ModePerm)
	return fs
}

func TestDir_ReadPatterns(t *testing.T) {
	ps, err := ReadPatterns(setupTestFS(false), nil)
	if err != nil {
		t.Errorf("no error expected, found %v", err)
	}
	if len(ps) != 2 {
		t.Errorf("expected 2 patterns, found %v", len(ps))
	}
	m := NewMatcher(ps)
	if !m.Match([]string{"vendor", "gopkg.in"}, true) {
		t.Error("expected a match")
	}
	if m.Match([]string{"vendor", "github.com"}, true) {
		t.Error("expected no match")
	}
}
