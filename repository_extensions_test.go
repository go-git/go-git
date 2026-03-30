package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestVerifyExtensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*testing.T, *config.Config)
		wantErr string
	}{
		{
			name: "repositoryformatversion=0: invalid extension",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version0
				cfg.Raw.Section("extensions").SetOption("unknown", "foo")
				cfg.Raw.Section("extensions").SetOption("objectformat", "sha1")
			},
			wantErr: "repositoryformatversion does not support extension: unknown, objectformat",
		},
		{
			name: "repositoryformatversion=0: allows supported noop",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version0
				cfg.Raw.Section("extensions").SetOption("noop", "bar")
			},
		},
		{
			name: "repositoryformatversion='': allows supported noop",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Raw.Section("extensions").SetOption("noop", "bar")
			},
		},
		{
			name: "repositoryformatversion=1: rejects unknown extensions",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Raw.Section("extensions").SetOption("unknownext", "true")
			},
			wantErr: "unknown extension: unknownext",
		},
		{
			name: "repositoryformatversion=1: allows known extension",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Raw.Section("extensions").SetOption("NOOP", "foo")
				cfg.Raw.Section("extensions").SetOption("noop-v1", "bar")
			},
		},
		{
			name: "repositoryformatversion=1: allows objectformat=sha1",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Raw.Section("extensions").SetOption("objectformat", "sha1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()

			r, err := Init(st)
			require.NoError(t, err)
			require.NotNil(t, r)

			cfg, err := st.Config()
			require.NoError(t, err)

			tt.setup(t, cfg)
			require.NoError(t, st.SetConfig(cfg))

			r, err = Open(st, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, r)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, r)
			}
		})
	}
}
