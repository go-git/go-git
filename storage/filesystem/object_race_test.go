package filesystem

import (
	"sync"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
)

// TestConcurrentIndexAccess tests that concurrent access to s.index is
// properly synchronized with mutexes. Without proper mutex protection, this
// test will trigger race detector failures when run with -race flag.
//
// Run with: go test -race -run TestConcurrentIndexAccess ./storage/filesystem/
func TestConcurrentIndexAccess(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile").One()
	fs, err := fixture.DotGit()
	if err != nil {
		t.Fatal(err)
	}

	storage := NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = storage.Close() }()

	var wg sync.WaitGroup

	// Simulate concurrent operations that access s.index
	// This should trigger race detector if proper locking isn't in place
	for range 20 {
		wg.Add(3)

		// Reader 1: HashesWithPrefix (iterates over s.index)
		go func() {
			defer wg.Done()
			_, _ = storage.HashesWithPrefix([]byte{0x6e})
		}()

		// Reader 2: EncodedObject (checks s.index != nil)
		go func() {
			defer wg.Done()
			hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
			_, _ = storage.EncodedObject(plumbing.AnyObject, hash)
		}()

		// Writer: Reindex (sets s.index = nil)
		go func() {
			defer wg.Done()
			storage.Reindex()
		}()
	}

	wg.Wait()
}
