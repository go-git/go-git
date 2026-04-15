package archive

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestMatchesPathFilter(t *testing.T) {
	// Note: All paths use forward slashes because these are Git internal paths,
	// not OS filesystem paths. Git always uses / regardless of platform.
	tests := []struct {
		name    string
		path    string
		filters []string
		want    bool
	}{
		{
			name:    "exact match",
			path:    "README.md",
			filters: []string{"README.md"},
			want:    true,
		},
		{
			name:    "prefix match",
			path:    "docs/guide.md",
			filters: []string{"docs"},
			want:    true,
		},
		{
			name:    "deep nested path",
			path:    "src/pkg/sub/module.go",
			filters: []string{"src/pkg"},
			want:    true,
		},
		{
			name:    "glob pattern match",
			path:    "test.go",
			filters: []string{"*.go"},
			want:    true,
		},
		{
			name:    "glob pattern no match",
			path:    "README.md",
			filters: []string{"*.go"},
			want:    false,
		},
		{
			name:    "child match parent",
			path:    "docs",
			filters: []string{"docs/guide.md"},
			want:    true,
		},
		{
			name:    "exact directory no trailing slash",
			path:    "docs/guide.md",
			filters: []string{"docs/"},
			want:    false,
		},
		{
			name:    "no match",
			path:    "src/main.go",
			filters: []string{"docs", "*.md"},
			want:    false,
		},
		{
			name:    "multiple filters match",
			path:    "docs/guide.md",
			filters: []string{"src", "docs"},
			want:    true,
		},
		{
			name:    "dot prefix match",
			path:    ".github/workflows/ci.yml",
			filters: []string{".github"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesPathFilter(tt.path, tt.filters)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchesPathFilter_PlatformIndependence verifies that the function
// works correctly with forward slashes, which is the format used by Git
// internally regardless of the operating system.
func TestMatchesPathFilter_PlatformIndependence(t *testing.T) {
	// Windows-style backslash paths should NOT match because Git
	// always uses forward slashes internally
	assert.False(t, MatchesPathFilter("docs\\guide.md", []string{"docs"}),
		"backslash paths should not match - Git uses forward slashes")

	// Forward slashes should match regardless of OS
	assert.True(t, MatchesPathFilter("docs/guide.md", []string{"docs"}),
		"forward slash paths should always match")
}

func TestResolveRef(t *testing.T) {
	fixture := fixtures.Basic().One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)

	// Get expected hashes from the fixture
	masterRef, err := storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	masterHash := masterRef.Hash()

	tagRef, err := storer.Reference(plumbing.ReferenceName("refs/tags/v1.0.0"))
	require.NoError(t, err)
	tagHash := tagRef.Hash()

	tests := []struct {
		name       string
		ref        string
		allowHash  bool
		wantHash   plumbing.Hash
		wantErr    bool
		errContain string
	}{
		{
			name:     "resolve branch with full ref",
			ref:      "refs/heads/master",
			allowHash: false,
			wantHash: masterHash,
			wantErr:  false,
		},
		{
			name:     "resolve short branch name",
			ref:      "master",
			allowHash: false,
			wantHash: masterHash,
			wantErr:  false,
		},
		{
			name:     "resolve tag name",
			ref:      "v1.0.0",
			allowHash: false,
			wantHash: tagHash,
			wantErr:  false,
		},
		{
			name:       "non-existent ref fails",
			ref:        "nonexistent",
			allowHash:  false,
			wantHash:   plumbing.ZeroHash,
			wantErr:    true,
			errContain: "cannot resolve",
		},
		{
			name:     "raw hash allowed when allowHash=true",
			ref:      masterHash.String(),
			allowHash: true,
			wantHash: masterHash,
			wantErr:  false,
		},
		{
			name:       "raw hash rejected when allowHash=false",
			ref:        masterHash.String(),
			allowHash:  false,
			wantHash:   plumbing.ZeroHash,
			wantErr:    true,
			errContain: "cannot resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveRef(storer, tt.ref, tt.allowHash)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantHash, got)
			}
		})
	}
}

func TestResolveTreeish_Errors(t *testing.T) {
	fixture := fixtures.Basic().One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)

	tests := []struct {
		name       string
		treeish    string
		allowUnreachable bool
		wantErr    bool
		errIs      error
		errContain string
	}{
		{
			name:       "raw hash rejected without allowUnreachable",
			treeish:    "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			allowUnreachable: false,
			wantErr:    true,
			errIs:      ErrOnlyRefNames,
		},
		{
			name:       "relative ref expression rejected",
			treeish:    "master~1",
			allowUnreachable: false,
			wantErr:    true,
			errIs:      ErrRelativeExpressions,
		},
		{
			name:       "non-existent ref fails",
			treeish:    "nonexistent",
			allowUnreachable: false,
			wantErr:    true,
			errContain: "cannot resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ResolveTreeish(storer, tt.treeish, tt.allowUnreachable)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSupportedFormats(t *testing.T) {
	formats := SupportedFormats()
	assert.Contains(t, formats, "tar")
	assert.Contains(t, formats, "zip")
	assert.Contains(t, formats, "tar.gz")
	assert.Contains(t, formats, "tgz")
}

func TestApplyUmask(t *testing.T) {
	tests := []struct {
		name         string
		mode         int64
		isExecutable bool
		want         int64
	}{
		{
			name:         "regular file",
			mode:         0o000,
			isExecutable: false,
			want:         0o664,
		},
		{
			name:         "executable file",
			mode:         0o000,
			isExecutable: true,
			want:         0o775,
		},
		{
			name:         "preserves existing bits",
			mode:         0o640,
			isExecutable: false,
			want:         0o664,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyUmask(tt.mode, tt.isExecutable)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyUmaskDir(t *testing.T) {
	got := ApplyUmaskDir(0o000)
	assert.Equal(t, int64(0o775), got)
}
