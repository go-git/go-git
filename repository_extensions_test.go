package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v5/config"
	formatcfg "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/storage/memory"
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
				cfg.Core.RepositoryFormatVersion = formatcfg.Version_0
				cfg.Raw.Section("extensions").SetOption("unknown", "foo")
				cfg.Raw.Section("extensions").SetOption("objectformat", "sha1")
			},
			wantErr: "repositoryformatversion does not support extension: unknown, objectformat",
		},
		{
			name: "repositoryformatversion=0: allows supported noop",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version_0
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
				cfg.Core.RepositoryFormatVersion = formatcfg.Version_1
				cfg.Raw.Section("extensions").SetOption("unknownext", "true")
			},
			wantErr: "unknown extension: unknownext",
		},
		{
			name: "repositoryformatversion=1: allows known extension",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version_1
				cfg.Raw.Section("extensions").SetOption("NOOP", "foo")
				cfg.Raw.Section("extensions").SetOption("noop-v1", "bar")
			},
		},
		{
			name: "repositoryformatversion=1: rejects objectformat=sha1", // not supported in go-git/v5
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version_1
				cfg.Raw.Section("extensions").SetOption("objectformat", "sha1")
			},
			wantErr: "unknown extension: objectformat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()

			r, err := Init(st, nil)
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
