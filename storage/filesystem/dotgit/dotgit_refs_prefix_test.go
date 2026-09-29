package dotgit

import (
	"fmt"
	"io"
	"slices"
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
	refs := []string{}
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
			var matching []*plumbing.Reference
			for _, r := range all {
				if strings.HasPrefix(r.Name().String(), prefix) {
					matching = append(matching, r)
				}
			}
			slices.SortFunc(matching, func(a, b *plumbing.Reference) int {
				return strings.Compare(a.Name().String(), b.Name().String())
			})
			want := make([]string, 0, len(matching))
			for _, r := range matching {
				want = append(want, r.String())
			}

			iter, err := dir.RefsWithPrefix(prefix)
			require.NoError(t, err)
			assert.Equal(t, want, collectRefs(t, iter))
		})
	}
}

// "-", "." and "0" sort around "/", so a walk that visited a directory
// before its siblings would be out of order. A loose ref shadows a packed one
// between other packed refs, and a repeated packed entry is yielded once.
func TestRefsWithPrefixYieldsNameOrder(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"# pack-refs with: peeled fully-peeled sorted ", ""} {
		t.Run(header, func(t *testing.T) {
			t.Parallel()
			fs := memfs.New()
			writeFile(t, fs, "HEAD", "ref: refs/heads/a/b\n")
			writeFile(t, fs, "refs/heads/a/b", hashB+"\n")
			writeFile(t, fs, "refs/heads/a-c", hashA+"\n")
			writeFile(t, fs, "refs/heads/a0/d", hashA+"\n")
			packed := []string{
				hashA + " refs/heads/a.b",
				hashA + " refs/heads/a/a",
				hashA + " refs/heads/a/b",
				hashA + " refs/heads/a/c",
				hashA + " refs/heads/a0",
				hashB + " refs/heads/a0",
				hashA + " refs/tags/v1",
			}
			if header == "" {
				slices.Reverse(packed)
			} else {
				packed = append([]string{header}, packed...)
			}
			writeFile(t, fs, "packed-refs", strings.Join(packed, "\n")+"\n")
			dir := New(fs)

			iter, err := dir.RefsWithPrefix("")
			require.NoError(t, err)
			want := []string{
				"ref: refs/heads/a/b HEAD",
				hashA + " refs/heads/a-c",
				hashA + " refs/heads/a.b",
				hashA + " refs/heads/a/a",
				hashB + " refs/heads/a/b",
				hashA + " refs/heads/a/c",
				hashA + " refs/heads/a0",
				hashA + " refs/heads/a0/d",
				hashA + " refs/tags/v1",
			}
			if header == "" {
				// Reversing the file put the later duplicate first.
				want[6] = hashB + " refs/heads/a0"
			}
			assert.Equal(t, want, collectRefs(t, iter))
		})
	}
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

			// An unsorted file is scanned in full before the first result.
			ref, err := iter.Next()
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "refs/heads/main", ref.Name().String())

			_, err = iter.Next()
			assert.ErrorIs(t, err, io.EOF)
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
		// Branches and Tags callers commonly read one result and walk away.
		"AbandonedAfterFirst": func(iter storer.ReferenceIter) {
			_, _ = iter.Next()
		},
	} {
		for _, sorted := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/sorted=%v", name, sorted), func(t *testing.T) {
				t.Parallel()
				fs, _ := newPrefixTestDotGit(t)
				if !sorted {
					writeFile(t, fs, "packed-refs", hashB+" refs/remotes/origin/topic\n"+hashA+" refs/remotes/origin/main\n")
				}
				counting := &openCountingFS{Filesystem: fs}
				dir := New(counting)

				iter, err := dir.RefsWithPrefix("refs/remotes/origin/")
				require.NoError(t, err)
				consume(iter)
				assert.Zero(t, counting.open.Load())

				if name != "AbandonedAfterFirst" {
					_, err = iter.Next()
					assert.ErrorIs(t, err, io.EOF, "a released iterator yields nothing more")
				}
			})
		}
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

// Like Git, broken loose refs are skipped rather than failing the iteration,
// yet still hide the packed ref of the same name, and ".*" and "*.lock"
// entries are not refs at all. Refs instead reports the broken ones with the
// all-zero ID.
func TestRefsWithPrefixSkipsBrokenLooseRefs(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	writeFile(t, fs, "HEAD", "")
	writeFile(t, fs, "refs/heads/empty", "")
	writeFile(t, fs, "refs/heads/garbage", "garbage\n")
	writeFile(t, fs, "refs/heads/zero", plumbing.ZeroHash.String()+"\n")
	writeFile(t, fs, "refs/heads/spaced", "  "+hashA+"\n")
	writeFile(t, fs, "refs/heads/short", hashA[:39]+"\n")
	writeFile(t, fs, "refs/heads/vtab", hashA+"\v\n")
	writeFile(t, fs, "refs/heads/symempty", "ref: \n")
	writeFile(t, fs, "refs/heads/shadow", "")
	writeFile(t, fs, "refs/heads/main.lock", hashA+"\n")
	writeFile(t, fs, "refs/heads/.hidden", hashA+"\n")
	writeFile(t, fs, "refs/heads/.dir/x", hashA+"\n")
	writeFile(t, fs, "refs/heads/main", hashA+"\n")
	writeFile(t, fs, "refs/heads/trailing", hashB+" trailing\n")
	writeFile(t, fs, "refs/heads/sym", "ref:refs/heads/main\n")
	writeFile(t, fs, "packed-refs", "# pack-refs with: sorted \n"+
		hashB+" refs/heads/after\n"+
		hashB+" refs/heads/shadow\n")
	dir := New(fs)

	all, err := dir.Refs()
	require.NoError(t, err)
	assert.Contains(t, collectRefs(t, storer.NewReferenceSliceIter(all)), plumbing.ZeroHash.String()+" refs/heads/empty")

	iter, err := dir.RefsWithPrefix("")
	require.NoError(t, err)
	assert.Equal(t, []string{
		hashB + " refs/heads/after",
		hashA + " refs/heads/main",
		"ref: refs/heads/main refs/heads/sym",
		hashB + " refs/heads/trailing",
	}, collectRefs(t, iter))
}

// Ref, Refs and RefsWithPrefix share one loose-ref parser, so they agree on
// the value of every file, including forms Git accepts beyond "<id>\n". The
// ID's length, not the configured object format, selects SHA-1 or SHA-256,
// so a DotGit left at the default format still reads SHA-256 refs.
func TestLooseRefReadersAgree(t *testing.T) {
	t.Parallel()
	sha256ID := strings.Repeat("ab", 32)
	files := map[string]string{
		"refs/heads/extra":   hashA + " extra\n",
		"refs/heads/crlf":    hashA + "\r\n",
		"refs/heads/sym":     "ref:refs/heads/crlf\n",
		"refs/heads/sha256":  sha256ID + "\n",
		"refs/heads/nolf":    hashB,
		"refs/heads/symtabs": "ref:\trefs/heads/nolf \n",
	}
	fs := memfs.New()
	for name, content := range files {
		writeFile(t, fs, name, content)
	}
	dir := New(fs)

	all, err := dir.Refs()
	require.NoError(t, err)
	iter, err := dir.RefsWithPrefix("refs/")
	require.NoError(t, err)
	assert.Equal(t, collectRefs(t, storer.NewReferenceSliceIter(all)), collectRefs(t, iter))

	want := map[string]string{
		"refs/heads/extra":   hashA + " refs/heads/extra",
		"refs/heads/crlf":    hashA + " refs/heads/crlf",
		"refs/heads/sym":     "ref: refs/heads/crlf refs/heads/sym",
		"refs/heads/sha256":  sha256ID + " refs/heads/sha256",
		"refs/heads/nolf":    hashB + " refs/heads/nolf",
		"refs/heads/symtabs": "ref: refs/heads/nolf refs/heads/symtabs",
	}
	for name, value := range want {
		ref, err := dir.Ref(plumbing.ReferenceName(name))
		require.NoError(t, err, name)
		assert.Equal(t, value, ref.String(), name)
	}
}

// Content Git treats as broken fails Ref, is reported by Refs with the
// all-zero ID as git ls-remote shows it, and is skipped by RefsWithPrefix as
// git for-each-ref skips it.
func TestBrokenLooseRefReaders(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"garbage\n", "  " + hashA + "\n", hashA[:39] + "\n", hashA + "\v\n", "ref:\n", ""} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			fs := memfs.New()
			writeFile(t, fs, "refs/heads/broken", content)
			writeFile(t, fs, "refs/heads/main", hashA+"\n")
			dir := New(fs)

			if content != "" {
				// An empty file falls back to packed-refs, as SetRef may
				// not have written it yet.
				_, err := dir.Ref("refs/heads/broken")
				assert.ErrorIs(t, err, ErrBrokenRefFile)
			}
			all, err := dir.Refs()
			require.NoError(t, err)
			assert.Equal(t, []string{
				plumbing.ZeroHash.String() + " refs/heads/broken",
				hashA + " refs/heads/main",
			}, collectRefs(t, storer.NewReferenceSliceIter(all)))

			iter, err := dir.RefsWithPrefix("refs/heads/")
			require.NoError(t, err)
			assert.Equal(t, []string{hashA + " refs/heads/main"}, collectRefs(t, iter))
		})
	}
}

// A packed-refs file claiming the sorted trait is only trusted once its
// names are checked to ascend. Otherwise a lying header would end the scan
// early, hiding later matches, and leave a repeated name apart from its
// first, so that the merge would yield it twice.
func TestRefsWithPrefixChecksSortedClaim(t *testing.T) {
	t.Parallel()
	fs := memfs.New()
	writeFile(t, fs, "packed-refs", "# pack-refs with: peeled fully-peeled sorted \n"+
		hashA+" refs/heads/main\n"+
		hashA+" refs/tags/v1\n"+
		hashB+" refs/heads/feature\n"+
		hashB+" refs/heads/main\n")
	dir := New(fs)

	iter, err := dir.RefsWithPrefix("refs/heads/")
	require.NoError(t, err)
	assert.Equal(t, []string{
		hashB + " refs/heads/feature",
		hashA + " refs/heads/main",
	}, collectRefs(t, iter))

	all, err := dir.Refs()
	require.NoError(t, err)
	assert.Equal(t, []string{
		hashB + " refs/heads/feature",
		hashA + " refs/heads/main",
		hashA + " refs/tags/v1",
	}, collectRefs(t, storer.NewReferenceSliceIter(all)))
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
