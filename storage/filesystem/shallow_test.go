package filesystem_test

import (
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestShallowParsing(t *testing.T) {
	t.Parallel()

	sha1 := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	sha1b := "e8d3ffab552895c19b9fcf7aa264d277cde33881"
	sha256 := "a3c1b7a8f2d1e4c5b6a7980f1e2d3c4b5a6978877665544332211ffeeddccbba"

	tests := []struct {
		name    string
		format  formatcfg.ObjectFormat
		content string
		want    []string
		wantErr string
	}{
		{
			name:    "sha1 lines",
			format:  formatcfg.SHA1,
			content: sha1 + "\n" + sha1b + "\n",
			want:    []string{sha1, sha1b},
		},
		{
			name:    "unset format reads sha1",
			format:  formatcfg.UnsetObjectFormat,
			content: sha1 + "\n",
			want:    []string{sha1},
		},
		{
			name:    "sha256 line",
			format:  formatcfg.SHA256,
			content: sha256 + "\n",
			want:    []string{sha256},
		},
		{
			name:    "no trailing newline",
			format:  formatcfg.SHA1,
			content: sha1,
			want:    []string{sha1},
		},
		{
			name:    "crlf line ending",
			format:  formatcfg.SHA1,
			content: sha1 + "\r\n",
			want:    []string{sha1},
		},
		{
			name:    "bytes after the hash are ignored",
			format:  formatcfg.SHA1,
			content: sha1 + " trailing\n",
			want:    []string{sha1},
		},
		{
			name:    "empty file",
			format:  formatcfg.SHA1,
			content: "",
		},
		{
			name:    "blank line",
			format:  formatcfg.SHA1,
			content: sha1 + "\n\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "short hex",
			format:  formatcfg.SHA1,
			content: sha1[:38] + "\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "non-hex",
			format:  formatcfg.SHA1,
			content: strings.Repeat("z", 40) + "\n",
			wantErr: "bad shallow line",
		},
		{
			name:    "sha1 line in sha256 repository",
			format:  formatcfg.SHA256,
			content: sha1 + "\n",
			wantErr: "bad shallow line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := memfs.New()
			require.NoError(t, util.WriteFile(fs, "shallow", []byte(tc.content), 0o644))
			st := filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(),
				filesystem.Options{ObjectFormat: tc.format})

			got, err := st.Shallow()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			var want []plumbing.Hash
			for _, h := range tc.want {
				want = append(want, plumbing.NewHash(h))
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestShallowRoundTrip(t *testing.T) {
	t.Parallel()

	st := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	want := []plumbing.Hash{
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}
	require.NoError(t, st.SetShallow(want))

	got, err := st.Shallow()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestShallowFollowsSetObjectFormat(t *testing.T) {
	t.Parallel()

	fs := memfs.New()
	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	require.NoError(t, st.SetObjectFormat(formatcfg.SHA256))

	sha256 := "a3c1b7a8f2d1e4c5b6a7980f1e2d3c4b5a6978877665544332211ffeeddccbba"
	require.NoError(t, util.WriteFile(fs, "shallow", []byte(sha256+"\n"), 0o644))

	got, err := st.Shallow()
	require.NoError(t, err)
	assert.Equal(t, []plumbing.Hash{plumbing.NewHash(sha256)}, got)
}
