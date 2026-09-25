package dotgit

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

const (
	hashA = "e8d3ffab552895c19b9fcf7aa264d277cde33881"
	hashB = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
)

func writeFile(t *testing.T, fs billy.Filesystem, name, content string) {
	t.Helper()
	require.NoError(t, util.WriteFile(fs, name, []byte(content), 0o644))
}

func collectRefs(t *testing.T, iter storer.ReferenceIter) []string {
	t.Helper()
	var refs []string
	require.NoError(t, iter.ForEach(func(r *plumbing.Reference) error {
		refs = append(refs, r.String())
		return nil
	}))
	return refs
}

func newPrefixTestDotGit(t *testing.T) (billy.Filesystem, *DotGit) {
	t.Helper()
	fs := memfs.New()
	writeFile(t, fs, "HEAD", "ref: refs/heads/main\n")
	writeFile(t, fs, "refs/heads/main", hashA+"\n")
	writeFile(t, fs, "refs/heads/fe/one", hashA+"\n")
	writeFile(t, fs, "refs/heads/feature", hashB+"\n")
	writeFile(t, fs, "refs/remotes/origin/HEAD", "ref: refs/remotes/origin/main\n")
	writeFile(t, fs, "refs/remotes/origin-other/main", hashA+"\n")
	writeFile(t, fs, "packed-refs", strings.Join([]string{
		"# pack-refs with: peeled fully-peeled sorted ",
		hashA + " refs/heads/feature",
		hashA + " refs/heads/fix",
		hashA + " refs/remotes/origin/main",
		hashB + " refs/remotes/origin/topic",
		hashB + " refs/tags/v1",
		"^" + hashA,
		"",
	}, "\n"))
	return fs, New(fs)
}

func TestRefsWithPrefixMatchesFilteredRefs(t *testing.T) {
	t.Parallel()
	_, dir := newPrefixTestDotGit(t)
	all, err := dir.Refs()
	require.NoError(t, err)

	for _, prefix := range []string{
		"", "H", "HEAD", "HEADS", "r", "refs", "refs/",
		"refs/heads/", "refs/heads/fe", "refs/heads/fe/", "refs/heads/feature",
		"refs/remotes/origin", "refs/remotes/origin/", "refs/tags/",
		"refs/heads/main/", "refs//", "refs/heads//", "refs/nope/",
		"refs/../", "refs/heads/../../", "packed-refs", "objects/",
	} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			want := make([]string, 0, len(all))
			for _, r := range all {
				if strings.HasPrefix(r.Name().String(), prefix) {
					want = append(want, r.String())
				}
			}

			iter, err := dir.RefsWithPrefix(prefix)
			require.NoError(t, err)
			assert.ElementsMatch(t, want, collectRefs(t, iter))
		})
	}
}

func TestRefsWithPrefixEmptyPrefixMatchesRefsOrder(t *testing.T) {
	t.Parallel()
	_, dir := newPrefixTestDotGit(t)
	all, err := dir.Refs()
	require.NoError(t, err)
	want := make([]string, 0, len(all))
	for _, r := range all {
		want = append(want, r.String())
	}

	iter, err := dir.RefsWithPrefix("")
	require.NoError(t, err)
	assert.Equal(t, want, collectRefs(t, iter))
}

func TestRefsWithPrefixLooseShadowsPacked(t *testing.T) {
	t.Parallel()
	_, dir := newPrefixTestDotGit(t)

	iter, err := dir.RefsWithPrefix("refs/heads/feature")
	require.NoError(t, err)
	assert.Equal(t, []string{hashB + " refs/heads/feature"}, collectRefs(t, iter))
}

func TestRefsWithPrefixKeepsFirstOfDuplicatePackedRefs(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	writeFile(t, fs, "packed-refs", hashA+" refs/heads/main\n"+hashB+" refs/heads/main\n")
	dir := New(fs)

	all, err := dir.Refs()
	require.NoError(t, err)
	require.Len(t, all, 1)

	iter, err := dir.RefsWithPrefix("refs/heads/")
	require.NoError(t, err)
	assert.Equal(t, []string{all[0].String()}, collectRefs(t, iter))
}

// A malformed line beyond the matching range detects whether scanning stops
// early. Only a sorted header permits skipping it.
func TestRefsWithPrefixStopsAfterSortedPackedRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header  string
		wantErr error
	}{
		{header: "# pack-refs with: peeled fully-peeled sorted "},
		{header: "# pack-refs with: peeled fully-peeled ", wantErr: ErrPackedRefsBadFormat},
		{header: "", wantErr: ErrPackedRefsBadFormat},
	} {
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			fs := memfs.New()
			writeFile(t, fs, "packed-refs", tc.header+"\n"+
				hashA+" refs/heads/main\n"+
				hashA+" refs/remotes/origin/main\n"+
				"malformed packed-refs line\n")
			dir := New(fs)

			iter, err := dir.RefsWithPrefix("refs/heads/")
			require.NoError(t, err)
			defer iter.Close()

			ref, err := iter.Next()
			require.NoError(t, err)
			assert.Equal(t, "refs/heads/main", ref.Name().String())

			_, err = iter.Next()
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.ErrorIs(t, err, io.EOF)
			}
		})
	}
}

func TestRefsWithPrefixReleasesPackedRefs(t *testing.T) {
	t.Parallel()
	for name, consume := range map[string]func(storer.ReferenceIter){
		"CloseAfterFirst": func(iter storer.ReferenceIter) {
			_, _ = iter.Next()
			iter.Close()
		},
		"Exhausted": func(iter storer.ReferenceIter) {
			for {
				if _, err := iter.Next(); err != nil {
					return
				}
			}
		},
		"ForEachStop": func(iter storer.ReferenceIter) {
			_ = iter.ForEach(func(*plumbing.Reference) error { return storer.ErrStop })
		},
		"CloseUnused": func(iter storer.ReferenceIter) {
			iter.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs, _ := newPrefixTestDotGit(t)
			counting := &openCountingFS{Filesystem: fs}
			dir := New(counting)

			iter, err := dir.RefsWithPrefix("refs/remotes/origin/")
			require.NoError(t, err)
			consume(iter)
			assert.Zero(t, counting.open.Load())

			_, err = iter.Next()
			assert.ErrorIs(t, err, io.EOF, "a released iterator yields nothing more")
		})
	}
}

func TestRefsWithPrefixToleratesRefsRemovedMidWalk(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	writeFile(t, fs, "refs/heads/a", hashA+"\n")
	writeFile(t, fs, "refs/heads/b", hashA+"\n")
	writeFile(t, fs, "refs/heads/z/c", hashA+"\n")
	dir := New(fs)

	iter, err := dir.RefsWithPrefix("refs/heads/")
	require.NoError(t, err)
	defer iter.Close()

	ref, err := iter.Next()
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/a", ref.Name().String())

	require.NoError(t, fs.Remove("refs/heads/b"))
	require.NoError(t, util.RemoveAll(fs, "refs/heads/z"))

	_, err = iter.Next()
	assert.ErrorIs(t, err, io.EOF)
}

func TestRefsWithPrefixReportsEmptyLooseRef(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	writeFile(t, fs, "refs/heads/empty", "")
	writeFile(t, fs, "refs/tags/v1", hashA+"\n")
	dir := New(fs)

	_, err := dir.Refs()
	require.ErrorIs(t, err, ErrEmptyRefFile)

	iter, err := dir.RefsWithPrefix("refs/heads/")
	require.NoError(t, err)
	defer iter.Close()
	_, err = iter.Next()
	assert.ErrorIs(t, err, ErrEmptyRefFile)

	iter, err = dir.RefsWithPrefix("refs/tags/")
	require.NoError(t, err)
	assert.Equal(t, []string{hashA + " refs/tags/v1"}, collectRefs(t, iter),
		"a broken reference outside the prefix is not read")
}

type openCountingFS struct {
	billy.Filesystem
	open atomic.Int64
}

func (fs *openCountingFS) Open(name string) (billy.File, error) {
	f, err := fs.Filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	fs.open.Add(1)
	return &openCountingFile{File: f, fs: fs}, nil
}

type openCountingFile struct {
	billy.File
	fs *openCountingFS
}

func (f *openCountingFile) Close() error {
	f.fs.open.Add(-1)
	return f.File.Close()
}
