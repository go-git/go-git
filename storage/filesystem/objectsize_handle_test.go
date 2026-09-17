package filesystem

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/x/fdpool"
)

// packHandleFS counts opens and closes of ".pack" files so a test can
// assert that handles taken during a lookup are handed back.
type packHandleFS struct {
	billy.Filesystem
	mu     sync.Mutex
	opens  int
	closes int
}

func (c *packHandleFS) track(path string, f billy.File, err error) (billy.File, error) {
	if err != nil || filepath.Ext(path) != ".pack" {
		return f, err
	}
	c.mu.Lock()
	c.opens++
	c.mu.Unlock()
	return &packHandleFile{File: f, fs: c}, nil
}

func (c *packHandleFS) Open(path string) (billy.File, error) {
	f, err := c.Filesystem.Open(path)
	return c.track(path, f, err)
}

func (c *packHandleFS) OpenFile(path string, flag int, perm os.FileMode) (billy.File, error) {
	f, err := c.Filesystem.OpenFile(path, flag, perm)
	return c.track(path, f, err)
}

func (c *packHandleFS) outstanding() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opens - c.closes, c.opens, c.closes
}

type packHandleFile struct {
	billy.File
	fs   *packHandleFS
	once sync.Once
}

func (f *packHandleFile) Close() error {
	f.once.Do(func() {
		f.fs.mu.Lock()
		f.fs.closes++
		f.fs.mu.Unlock()
	})
	return f.File.Close()
}

// TestEncodedObjectSizeReleasesPackHandle guards the pack handle that
// EncodedObjectSize opens. packfile() takes a SharedFile reference that pins
// the descriptor until it is released, so a missing Close leaves the pack
// open for the life of the storage and the fd pool cannot reclaim it.
func TestEncodedObjectSizeReleasesPackHandle(t *testing.T) {
	const (
		nPacks     = 16
		objPerPack = 4
		poolCap    = 4
	)
	diskFS, perPack := makeMultiPackFixture(t, nPacks, objPerPack)
	counting := &packHandleFS{Filesystem: diskFS}

	s := NewStorageWithOptions(counting, cache.NewObjectLRU(0), Options{
		Pool: fdpool.New(poolCap),
	})

	for _, pack := range perPack {
		for _, h := range pack {
			if _, err := s.EncodedObjectSize(h); err != nil {
				t.Fatalf("EncodedObjectSize(%s): %v", h, err)
			}
		}
	}

	// Measured while the storage is still live: the fd pool is allowed to
	// keep up to poolCap packs open, but a handle the size lookup forgot to
	// release pins its pack beyond that budget.
	out, opened, closed := counting.outstanding()
	t.Logf("pack files: opened=%d closed=%d outstanding=%d (pool cap %d)",
		opened, closed, out, poolCap)

	if err := s.Close(); err != nil {
		t.Fatalf("storage close: %v", err)
	}

	if out > poolCap {
		t.Fatalf("pack files held open beyond the pool budget: %d outstanding, cap %d "+
			"(opened %d, closed %d)", out, poolCap, opened, closed)
	}
}
