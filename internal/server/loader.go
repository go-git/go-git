package server

import (
	"fmt"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type fixturesLoader struct {
	fix *fixtures.Fixture
	t   *testing.T
}

var _ transport.Loader = &fixturesLoader{}

func (f *fixturesLoader) Load(ep *transport.Endpoint) (storage.Storer, error) {
	if f.fix == nil {
		return nil, fmt.Errorf("cannot load endpoint: fixture not set")
	}

	dot := f.fix.DotGit(fixtures.WithTargetDir(f.t.TempDir))
	st := filesystem.NewStorage(dot, nil)
	return st, nil
}

// Load implements transport.Loader.
func Loader(t *testing.T, fix *fixtures.Fixture) transport.Loader {
	return &fixturesLoader{t: t, fix: fix}
}
