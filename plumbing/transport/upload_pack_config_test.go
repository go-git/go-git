package transport

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

type configErrStorer struct {
	storage.Storer
}

func (configErrStorer) Config() (*config.Config, error) {
	return nil, errors.New("no config")
}

func TestPackConfig(t *testing.T) {
	t.Parallel()

	sha256Window := memory.NewStorage(memory.WithObjectFormat(hash.SHA256))
	cfg, err := sha256Window.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Pack.Window = 5

	tests := []struct {
		name       string
		st         storage.Storer
		skipDeltas bool
		window     uint
		format     hash.ObjectFormat
	}{
		{"default storage", memory.NewStorage(), false, config.DefaultPackWindow, hash.UnsetObjectFormat},
		{"sha256 storage", sha256Window, false, 5, hash.SHA256},
		{"skip delta compression", sha256Window, true, 0, hash.SHA256},
		{"unreadable config", configErrStorer{memory.NewStorage()}, false, config.DefaultPackWindow, hash.UnsetObjectFormat},
		{"unreadable config, skip delta compression", configErrStorer{memory.NewStorage()}, true, 0, hash.UnsetObjectFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			window, format := packConfig(tc.st, tc.skipDeltas)
			assert.Equal(t, tc.window, window)
			assert.Equal(t, tc.format, format)
		})
	}
}
