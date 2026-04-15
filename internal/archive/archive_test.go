package archive

import (
	"bytes"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestMatchesPathFilter(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := MatchesPathFilter(tt.path, tt.filters)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchesPathFilter_PlatformIndependence verifies that the function
// works correctly with forward slashes, which is the format used by Git
// internally regardless of the operating system.
func TestMatchesPathFilter_PlatformIndependence(t *testing.T) {
	t.Parallel()
	// Windows-style backslash paths should NOT match because Git
	// always uses forward slashes internally
	assert.False(t, MatchesPathFilter("docs\\guide.md", []string{"docs"}),
		"backslash paths should not match - Git uses forward slashes")

	// Forward slashes should match regardless of OS
	assert.True(t, MatchesPathFilter("docs/guide.md", []string{"docs"}),
		"forward slash paths should always match")
}

func TestResolveRef(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)
	defer func() { _ = storer.Close() }()

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
			name:      "resolve branch with full ref",
			ref:       "refs/heads/master",
			allowHash: false,
			wantHash:  masterHash,
			wantErr:   false,
		},
		{
			name:      "resolve short branch name",
			ref:       "master",
			allowHash: false,
			wantHash:  masterHash,
			wantErr:   false,
		},
		{
			name:      "resolve tag name",
			ref:       "v1.0.0",
			allowHash: false,
			wantHash:  tagHash,
			wantErr:   false,
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
			name:      "raw hash allowed when allowHash=true",
			ref:       masterHash.String(),
			allowHash: true,
			wantHash:  masterHash,
			wantErr:   false,
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

	for _, tt := range tests { //nolint:paralleltest // avoid parallel test because of shared fixture state
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
	t.Parallel()
	fixture := fixtures.Basic().One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)
	defer func() { _ = storer.Close() }()

	tests := []struct {
		name             string
		treeish          string
		allowUnreachable bool
		wantErr          bool
		errIs            error
		errContain       string
	}{
		{
			name:             "raw hash rejected without allowUnreachable",
			treeish:          "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrOnlyRefNames,
		},
		{
			name:             "relative ref expression rejected",
			treeish:          "master~1",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "non-existent ref fails",
			treeish:          "nonexistent",
			allowUnreachable: false,
			wantErr:          true,
			errContain:       "cannot resolve",
		},
	}

	for _, tt := range tests { //nolint:paralleltest // avoid parallel test because of shared fixture state
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

// TestResolveTreeish_AllowUnreachable tests the allowUnreachable flag enforcement
// comprehensively with various tree-ish expressions.
func TestResolveTreeish_AllowUnreachable(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)
	defer func() { _ = storer.Close() }()

	// Get the master commit hash for testing
	masterRef, err := storer.Reference(plumbing.ReferenceName("refs/heads/master"))
	require.NoError(t, err)
	masterHash := masterRef.Hash().String()

	tests := []struct {
		name             string
		treeish          string
		allowUnreachable bool
		wantErr          bool
		errIs            error
		errContain       string
	}{
		// allowUnreachable=false cases (secure mode)
		{
			name:             "raw hash blocked when allowUnreachable=false",
			treeish:          masterHash,
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrOnlyRefNames,
		},
		{
			name:             "parent expression blocked (~)",
			treeish:          "master~1",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "ancestor expression blocked (~N)",
			treeish:          "master~3",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "parent expression blocked (^)",
			treeish:          "master^",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "nth parent blocked (^N)",
			treeish:          "master^2",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "at-sign expression blocked (@)",
			treeish:          "@{upstream}",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "brace expression blocked",
			treeish:          "master@{1}",
			allowUnreachable: false,
			wantErr:          true,
			errIs:            ErrRelativeExpressions,
		},
		{
			name:             "ref short name allowed",
			treeish:          "master",
			allowUnreachable: false,
			wantErr:          false,
		},
		{
			name:             "full ref path allowed",
			treeish:          "refs/heads/master",
			allowUnreachable: false,
			wantErr:          false,
		},
		{
			name:             "tag short name allowed",
			treeish:          "v1.0.0",
			allowUnreachable: false,
			wantErr:          false,
		},
		// allowUnreachable=true cases
		{
			name:             "raw hash allowed when allowUnreachable=true",
			treeish:          masterHash,
			allowUnreachable: true,
			wantErr:          false,
		},
		{
			name:             "parent expression allowed (~)",
			treeish:          "master~1",
			allowUnreachable: true,
			wantErr:          true, // fails because we can't resolve, but not blocked
			errContain:       "cannot resolve",
		},
		{
			name:             "parent expression allowed (^)",
			treeish:          "master^",
			allowUnreachable: true,
			wantErr:          true,
			errContain:       "cannot resolve",
		},
	}

	for _, tt := range tests { //nolint:paralleltest // avoid parallel test because of shared fixture state
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

func TestResolveTreeish_AnnotatedTags(t *testing.T) {
	t.Parallel()
	fixture := fixtures.ByTag("tags").One()
	dotGit, err := fixture.DotGit()
	require.NoError(t, err)
	storer := filesystem.NewStorage(dotGit, nil)
	defer func() { _ = storer.Close() }()

	tests := []struct {
		name    string
		treeish string
		assert  func(*testing.T, *object.Tag)
	}{
		{
			name:    "commit target",
			treeish: "commit-tag",
			assert: func(t *testing.T, tag *object.Tag) {
				commit, err := object.GetCommit(storer, tag.Target)
				require.NoError(t, err)

				expectedTree, err := commit.Tree()
				require.NoError(t, err)

				tree, commitHash, commitTime, err := ResolveTreeish(storer, "commit-tag", false)
				require.NoError(t, err)
				require.NotNil(t, commitHash)
				assert.Equal(t, expectedTree.Hash, tree.Hash)
				assert.Equal(t, commit.Hash, *commitHash)
				assert.True(t, commit.Committer.When.Equal(commitTime))
			},
		},
		{
			name:    "tree target",
			treeish: "tree-tag",
			assert: func(t *testing.T, tag *object.Tag) {
				expectedTree, err := object.GetTree(storer, tag.Target)
				require.NoError(t, err)

				tree, commitHash, commitTime, err := ResolveTreeish(storer, "tree-tag", false)
				require.NoError(t, err)
				assert.Equal(t, expectedTree.Hash, tree.Hash)
				assert.Nil(t, commitHash)
				assert.False(t, commitTime.IsZero())
			},
		},
		{
			name:    "unsupported target",
			treeish: "blob-tag",
			assert: func(t *testing.T, _ *object.Tag) {
				_, _, _, err := ResolveTreeish(storer, "blob-tag", false)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported object type for archive")
			},
		},
	}

	for _, tt := range tests { //nolint:paralleltest // object / storer are not thread safe
		t.Run(tt.name, func(t *testing.T) {
			tagRef, err := storer.Reference(plumbing.ReferenceName("refs/tags/" + tt.treeish))
			require.NoError(t, err)

			tag, err := object.GetTag(storer, tagRef.Hash())
			require.NoError(t, err)

			tt.assert(t, tag)
		})
	}
}

func TestWriteTarArchive_RejectsOversizedSymlinkTarget(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()

	blobObj := &plumbing.MemoryObject{}
	blobObj.SetType(plumbing.BlobObject)
	_, err := blobObj.Write([]byte(strings.Repeat("a", maxTarSymlinkTargetSize+1)))
	require.NoError(t, err)
	blobHash, err := st.SetEncodedObject(blobObj)
	require.NoError(t, err)

	treeObj := &object.Tree{
		Entries: []object.TreeEntry{
			{
				Name: "link",
				Mode: filemode.Symlink,
				Hash: blobHash,
			},
		},
	}

	encodedTree := &plumbing.MemoryObject{}
	encodedTree.SetType(plumbing.TreeObject)
	require.NoError(t, treeObj.Encode(encodedTree))
	treeHash, err := st.SetEncodedObject(encodedTree)
	require.NoError(t, err)

	tree, err := object.GetTree(st, treeHash)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = WriteTarArchive(st, &buf, tree, nil, "", nil, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSymlinkTargetTooLarge)
	assert.Contains(t, err.Error(), "link")
}

func TestSupportedFormats(t *testing.T) {
	t.Parallel()
	formats := SupportedFormats()
	assert.Contains(t, formats, "tar")
	assert.Contains(t, formats, "zip")
	assert.Contains(t, formats, "tar.gz")
	assert.Contains(t, formats, "tgz")
}

func TestApplyUmask(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := ApplyUmask(tt.mode, tt.isExecutable)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyUmaskDir(t *testing.T) {
	t.Parallel()
	got := ApplyUmaskDir(0o000)
	assert.Equal(t, int64(0o775), got)
}
