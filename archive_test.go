package git

import (
	"archive/tar"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
)

func TestArchiveOptionsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    *ArchiveOptions
		wantErr string
	}{
		{
			name:    "valid hash treeish",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "8ab686eafeb1f44702738c8b0f24f2567c36da6d"},
			wantErr: "",
		},
		{
			name:    "valid branch treeish",
			opts:    &ArchiveOptions{Format: "zip", Treeish: "master"},
			wantErr: "",
		},
		{
			name:    "valid revision expression with caret",
			opts:    &ArchiveOptions{Treeish: "v1.0^{tree}"},
			wantErr: "",
		},
		{
			name:    "valid revision expression with tilde",
			opts:    &ArchiveOptions{Treeish: "HEAD~3"},
			wantErr: "",
		},
		{
			name:    "valid colon subpath syntax",
			opts:    &ArchiveOptions{Treeish: "HEAD:Documentation/"},
			wantErr: "",
		},
		{
			name:    "prefix with path traversal",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "HEAD", Prefix: "../escape/"},
			wantErr: "invalid archive prefix",
		},
		{
			name:    "absolute prefix",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "HEAD", Prefix: "/opt/build/"},
			wantErr: "invalid archive prefix",
		},
		{
			name:    "valid relative prefix",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "HEAD", Prefix: "myproject/"},
			wantErr: "",
		},
		{
			name:    "valid paths with dot-dot",
			opts:    &ArchiveOptions{Format: "tar", Treeish: "HEAD", Paths: []string{"a/../b"}},
			wantErr: "",
		},
		{
			name:    "empty format defaults to tar",
			opts:    &ArchiveOptions{Treeish: "main"},
			wantErr: "",
		},
		{
			name:    "empty tree-ish",
			opts:    &ArchiveOptions{Format: "tar", Treeish: ""},
			wantErr: "tree-ish is required",
		},
		{
			// Validation happens during either Archive or ArchiveRemote.
			name:    "unsupported format",
			opts:    &ArchiveOptions{Format: "rar", Treeish: "HEAD"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

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
		{
			name:    "remote allows custom format",
			url:     "file:///tmp/repo.git",
			opts:    &ArchiveOptions{Format: "tar.xz", Treeish: "master"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ArchiveRemote(tt.url, tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					assert.NotContains(t, err.Error(), "unsupported format")
				}
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestArchiveRemoteIntegration(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)
	repoURL := "file://" + repoPath

	tests := []struct {
		name      string
		opts      *ArchiveOptions
		wantFiles bool
		wantCheck func(t *testing.T, data []byte)
	}{
		{
			name:      "tar format",
			opts:      &ArchiveOptions{Format: "tar", Treeish: "master"},
			wantFiles: true,
		},
		{
			name: "tar with prefix",
			opts: &ArchiveOptions{Format: "tar", Treeish: "master", Prefix: "myproject/"},
			wantCheck: func(t *testing.T, data []byte) {
				tr := tar.NewReader(bytes.NewReader(data))
				for {
					hdr, err := tr.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if hdr.Typeflag == tar.TypeXGlobalHeader {
						continue
					}
					assert.True(t, strings.HasPrefix(hdr.Name, "myproject/"),
						"expected prefix myproject/, got %s", hdr.Name)
				}
			},
		},
		{
			name: "tar with path filter",
			opts: &ArchiveOptions{Format: "tar", Treeish: "master", Paths: []string{".gitignore"}},
			wantCheck: func(t *testing.T, data []byte) {
				tr := tar.NewReader(bytes.NewReader(data))
				var names []string
				for {
					hdr, err := tr.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if hdr.Typeflag != tar.TypeXGlobalHeader {
						names = append(names, hdr.Name)
					}
				}
				assert.Equal(t, []string{".gitignore"}, names)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rc, err := ArchiveRemote(repoURL, tt.opts)
			require.NoError(t, err)
			t.Cleanup(func() { rc.Close() })

			data, err := io.ReadAll(rc)
			require.NoError(t, err)

			if tt.wantCheck != nil {
				tt.wantCheck(t, data)
				return
			}

			if tt.wantFiles {
				tr := tar.NewReader(bytes.NewReader(data))
				var count int
				for {
					_, err := tr.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					count++
				}
				assert.Greater(t, count, 0, "archive should contain entries")
			}
		})
	}
}
