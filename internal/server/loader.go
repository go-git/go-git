// Package server provides test helpers for running in-process git servers.
package server

import (
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type fixturesLoader struct {
	dot billy.Filesystem
}

var _ transport.Loader = &fixturesLoader{}

func (f *fixturesLoader) Load(_ *transport.Endpoint) (storage.Storer, error) {
	if f.dot == nil {
		return nil, fmt.Errorf("cannot load endpoint: fixture not set")
	}

	// Re-use the pre-extracted filesystem for every request. The server is
	// read-only (upload-pack only), so sharing the underlying billy.Filesystem
	// across storage instances is safe and avoids re-extracting the tgz on
	// every clone or fetch.
	return filesystem.NewStorage(f.dot, nil), nil
}

// Loader returns a transport.Loader backed by fix. The fixture's .git directory
// is extracted exactly once (into memory) when Loader is called; all subsequent
// server requests share the same extracted filesystem.
func Loader(t testing.TB, fix *fixtures.Fixture) transport.Loader {
	t.Helper()
	if fix == nil {
		t.Fatal("Loader: fixture must not be nil")
	}
	dot, err := fix.DotGit(fixtures.WithMemFS())
	if err != nil {
		t.Fatal("Loader: DotGit failed:", err)
	}
	return &fixturesLoader{dot: dot}
}
