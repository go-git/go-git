package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/archive"
)

func TestSplitArchivePathspecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         []string
		wantInclude []string
		wantExclude []string
		wantErr     error
	}{
		{
			name:        "positive and exclude magic forms",
			raw:         []string{"src", ":(exclude)src/vendor", ":!docs", ":^tmp"},
			wantInclude: []string{"src"},
			wantExclude: []string{"src/vendor", "docs", "tmp"},
		},
		{
			name:        "exclude only",
			raw:         []string{":(exclude)vendor"},
			wantExclude: []string{"vendor"},
		},
		{
			name:    "empty long exclude errors",
			raw:     []string{":(exclude)"},
			wantErr: archive.ErrPathspecNoMatch,
		},
		{
			name:    "empty short exclude errors",
			raw:     []string{":!"},
			wantErr: archive.ErrPathspecNoMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inc, exc, err := splitArchivePathspecs(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantInclude, inc)
			assert.Equal(t, tt.wantExclude, exc)
		})
	}
}
