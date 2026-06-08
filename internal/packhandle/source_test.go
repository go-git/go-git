package packhandle_test

import (
	"io"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
)

func TestPathSource_OpenAndSize_ReadsFixturePackFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixture := fixtures.NewOSFixture(fixtures.Basic().One(), dir)

	// fixture.Packfile() writes the pack contents to a fresh file under
	// dir and returns an open billy.File pointed at it. We only need the
	// file's relative path to feed PathSource — close the returned handle
	// and let PathSource open its own.
	materialized, err := fixture.Packfile()
	if err != nil {
		t.Fatalf("Packfile() err: %v", err)
	}
	relPath := materialized.Name()
	materialized.Close()

	fs := osfs.New(dir)
	src := packhandle.PathSource(fs, relPath)

	size, err := src.Size()
	if err != nil {
		t.Fatalf("Size() err: %v", err)
	}
	if size <= 0 {
		t.Fatalf("Size() = %d, want > 0", size)
	}

	rc, err := src.Open()
	if err != nil {
		t.Fatalf("Open() err: %v", err)
	}
	defer rc.Close()

	buf := make([]byte, 4)
	if _, err := rc.ReadAt(buf, 0); err != nil && err != io.EOF {
		t.Fatalf("ReadAt err: %v", err)
	}
	if string(buf) != "PACK" {
		t.Fatalf("ReadAt at 0 = %q, want \"PACK\"", string(buf))
	}
}
