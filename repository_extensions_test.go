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
		name          string
		setup         func(*testing.T, *config.Config)
		wantSetConfig string
		wantOpenErr   string
	}{
		{
			name: "repositoryformatversion=0: invalid extension",
			setup: func(_ *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version0
				cfg.Raw.Section("extensions").SetOption("unknown", "foo")
				cfg.Raw.Section("extensions").SetOption("objectformat", "sha1")
			},
			wantSetConfig: "config extensions require core.repositoryformatversion = 1",
		},
		{
			name: "repositoryformatversion=0: allows supported noop",
			setup: func(_ *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version0
				cfg.Raw.Section("extensions").SetOption("noop", "bar")
			},
		},
		{
			name: "repositoryformatversion='': allows supported noop",
			setup: func(_ *testing.T, cfg *config.Config) {
				cfg.Raw.Section("extensions").SetOption("noop", "bar")
			},
		},
		{
			name: "repositoryformatversion=1: rejects unknown extensions",
			setup: func(_ *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Raw.Section("extensions").SetOption("unknownext", "true")
			},
			wantOpenErr: "unknown extension: unknownext",
		},
		{
			name: "repositoryformatversion=1: allows known extension",
			setup: func(_ *testing.T, cfg *config.Config) {
				cfg.Core.RepositoryFormatVersion = formatcfg.Version1
				cfg.Raw.Section("extensions").SetOption("NOOP", "foo")
				cfg.Raw.Section("extensions").SetOption("noop-v1", "bar")
			},
		},
		{
			name: "repositoryformatversion=1: allows objectformat=sha1",
			setup: func(_ *testing.T, cfg *config.Config) {
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
			err = st.SetConfig(cfg)
			if tt.wantSetConfig != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantSetConfig)
				return
			}
			require.NoError(t, err)

			r, err = Open(st, nil)
			if tt.wantOpenErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantOpenErr)
				assert.Nil(t, r)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, r)
			}
		})
	}
}
