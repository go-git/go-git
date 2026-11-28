package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage"
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
	err = WriteTarArchive(st, &buf, tree, nil, "", nil, nil, time.Now())
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

// buildExcludeTestTree builds the tree shape used by upstream
// t/t6132-pathspec-exclude.sh: file, sub/file, sub/sub/file, sub2/file.
func buildExcludeTestTree(t *testing.T, st storage.Storer) *object.Tree {
	t.Helper()

	addBlob := func(name string) plumbing.Hash {
		obj := &plumbing.MemoryObject{}
		obj.SetType(plumbing.BlobObject)
		_, err := obj.Write([]byte(name + "\n"))
		require.NoError(t, err)
		hash, err := st.SetEncodedObject(obj)
		require.NoError(t, err)
		return hash
	}
	addTree := func(entries ...object.TreeEntry) plumbing.Hash {
		obj := &plumbing.MemoryObject{}
		obj.SetType(plumbing.TreeObject)
		require.NoError(t, (&object.Tree{Entries: entries}).Encode(obj))
		hash, err := st.SetEncodedObject(obj)
		require.NoError(t, err)
		return hash
	}

	subSubTree := addTree(object.TreeEntry{
		Name: "file", Mode: filemode.Regular, Hash: addBlob("sub/sub/file"),
	})
	subTree := addTree(
		object.TreeEntry{Name: "file", Mode: filemode.Regular, Hash: addBlob("sub/file")},
		object.TreeEntry{Name: "sub", Mode: filemode.Dir, Hash: subSubTree},
	)
	sub2Tree := addTree(object.TreeEntry{
		Name: "file", Mode: filemode.Regular, Hash: addBlob("sub2/file"),
	})
	root := addTree(
		object.TreeEntry{Name: "file", Mode: filemode.Regular, Hash: addBlob("file")},
		object.TreeEntry{Name: "sub", Mode: filemode.Dir, Hash: subTree},
		object.TreeEntry{Name: "sub2", Mode: filemode.Dir, Hash: sub2Tree},
	)

	tree, err := object.GetTree(st, root)
	require.NoError(t, err)
	return tree
}

func tarEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	return names
}

func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func TestWriteTarArchive_ExcludeFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		paths    []string
		excludes []string
		want     []string
	}{
		{
			name:     "exclude subtree",
			excludes: []string{"sub"},
			want:     []string{"file", "sub2/", "sub2/file"},
		},
		{
			name:     "exclude subtree with trailing slash",
			excludes: []string{"sub/"},
			want:     []string{"file", "sub2/", "sub2/file"},
		},
		{
			name:     "exclude single file preserves parent dir",
			excludes: []string{"sub/file"},
			want:     []string{"file", "sub/", "sub/sub/", "sub/sub/file", "sub2/", "sub2/file"},
		},
		{
			name:     "exclude nested file omits emptied leaf dir",
			excludes: []string{"sub/sub/file"},
			want:     []string{"file", "sub/", "sub/file", "sub2/", "sub2/file"},
		},
		{
			name:     "positive subtree minus nested subtree preserves parent dir",
			paths:    []string{"sub"},
			excludes: []string{"sub/sub"},
			want:     []string{"sub/", "sub/file"},
		},
		{
			name:     "single-level glob keeps deeper file and parent dirs",
			excludes: []string{"sub/*"},
			want:     []string{"file", "sub/", "sub/sub/", "sub/sub/file", "sub2/", "sub2/file"},
		},
		{
			name:     "glob matching leaf file omits emptied dir",
			excludes: []string{"sub/*/file"},
			want:     []string{"file", "sub/", "sub/file", "sub2/", "sub2/file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage()
			tree := buildExcludeTestTree(t, st)

			var buf bytes.Buffer
			require.NoError(t, WriteTarArchive(st, &buf, tree, nil, "", tt.paths, tt.excludes, time.Time{}))
			assert.Equal(t, tt.want, tarEntryNames(t, buf.Bytes()))
		})
	}
}

func TestWriteZipArchive_ExcludeFilter(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	var buf bytes.Buffer
	require.NoError(t, WriteZipArchive(st, &buf, tree, nil, "", nil, []string{"sub"}, time.Time{}))

	names := zipEntryNames(t, buf.Bytes())
	assert.ElementsMatch(t, []string{"file", "sub2/file"}, names)
}

func TestWriteArchive_ExcludeCombinedWithPositiveFilter(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	// `git archive -- sub ':(exclude)sub/sub'`: only sub/ and sub/file survive
	// inside the positive subtree.
	var tarBuf bytes.Buffer
	require.NoError(t, WriteTarArchive(
		st, &tarBuf, tree, nil, "",
		[]string{"sub"}, []string{"sub/sub"}, time.Time{},
	))
	assert.Equal(t, []string{"sub/", "sub/file"}, tarEntryNames(t, tarBuf.Bytes()))

	var zipBuf bytes.Buffer
	require.NoError(t, WriteZipArchive(
		st, &zipBuf, tree, nil, "",
		[]string{"sub"}, []string{"sub/sub"}, time.Time{},
	))
	assert.ElementsMatch(t, []string{"sub/file"}, zipEntryNames(t, zipBuf.Bytes()))
}

func TestWriteArchive_ExcludeGlob(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	// A single-level glob excludes sub/sub/file but keeps sub/file,
	// mirroring the `:(glob)sub/*/file` case in t/t6132-pathspec-exclude.sh.
	var buf bytes.Buffer
	require.NoError(t, WriteZipArchive(
		st, &buf, tree, nil, "",
		nil, []string{"sub/*/file"}, time.Time{},
	))
	assert.ElementsMatch(t,
		[]string{"file", "sub/file", "sub2/file"},
		zipEntryNames(t, buf.Bytes()),
	)
}

func TestWriteArchive_ExcludeOnlyCanEmptyTheArchive(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	// Negative pathspecs alone subtract from the whole tree: excluding
	// everything produces an empty archive, not a no-match error.
	var tarBuf bytes.Buffer
	require.NoError(t, WriteTarArchive(
		st, &tarBuf, tree, nil, "",
		nil, []string{"file", "sub", "sub2"}, time.Time{},
	))
	assert.Empty(t, tarEntryNames(t, tarBuf.Bytes()))

	var zipBuf bytes.Buffer
	require.NoError(t, WriteZipArchive(
		st, &zipBuf, tree, nil, "",
		nil, []string{"file", "sub", "sub2"}, time.Time{},
	))
	assert.Empty(t, zipEntryNames(t, zipBuf.Bytes()))
}

func TestWriteArchive_PositiveNoMatchStillErrors(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	// The no-match error stays tied to positive pathspecs even when an
	// exclude filter is also given.
	var tarBuf bytes.Buffer
	err := WriteTarArchive(
		st, &tarBuf, tree, nil, "",
		[]string{"missing"}, []string{"sub"}, time.Time{},
	)
	require.ErrorIs(t, err, ErrPathspecNoMatch)
}

func TestWriteArchive_PositiveFullyExcludedIsEmptyNotError(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()
	tree := buildExcludeTestTree(t, st)

	// Matches `git archive -- sub ':(exclude)sub/...'`: the positive
	// spec does match, but every entry it would select is excluded. Git
	// writes an empty archive and exits successfully rather than failing
	// with a no-match error, so the no-match flag must be evaluated before
	// the exclude filter removes the last entry.
	excludes := []string{"sub/file", "sub/sub"}

	var tarBuf bytes.Buffer
	require.NoError(t, WriteTarArchive(
		st, &tarBuf, tree, nil, "",
		[]string{"sub"}, excludes, time.Time{},
	))
	assert.Empty(t, tarEntryNames(t, tarBuf.Bytes()))

	var zipBuf bytes.Buffer
	require.NoError(t, WriteZipArchive(
		st, &zipBuf, tree, nil, "",
		[]string{"sub"}, excludes, time.Time{},
	))
	assert.Empty(t, zipEntryNames(t, zipBuf.Bytes()))
}
