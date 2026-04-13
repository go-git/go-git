package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		opts    *ArchiveOptions
		wantErr string
	}{
		{
			name:    "empty tree-ish",
			url:     "file:///tmp/repo.git",
			opts:    &ArchiveOptions{Format: "tar", Treeish: ""},
			wantErr: "tree-ish is required",
		},
		{
			name:    "empty URL",
			url:     "",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "master"},
			wantErr: "remote URL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ArchiveRemote(tt.url, tt.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
