package transport

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// canonicalRepo is a bare repository that both go-git and git upload-pack can
// serve, so that their responses to the same request can be compared.
type canonicalRepo struct {
	t       testing.TB
	dir     string
	st      *filesystem.Storage
	commits map[string]plumbing.Hash
	files   map[plumbing.Hash]map[string]plumbing.Hash
	when    time.Time
}

func newCanonicalRepo(t testing.TB) *canonicalRepo {
	t.Helper()
	dir := t.TempDir()
	out, err := gitenv.Command("git", "init", "--bare", "-q", dir).CombinedOutput()
	require.NoError(t, err, string(out))

	st := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = st.Close() })
	return &canonicalRepo{
		t:       t,
		dir:     dir,
		st:      st,
		commits: map[string]plumbing.Hash{},
		files:   map[plumbing.Hash]map[string]plumbing.Hash{},
		when:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// commit records a commit named name whose tree holds every file of its
// parents plus one file called name, like a commit made with git add.
func (r *canonicalRepo) commit(name string, parents ...string) plumbing.Hash {
	r.t.Helper()
	files := map[string]plumbing.Hash{}
	parentHashes := make([]plumbing.Hash, 0, len(parents))
	for _, p := range parents {
		ph := r.commits[p]
		parentHashes = append(parentHashes, ph)
		maps.Copy(files, r.files[ph])
	}

	blob := r.st.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	bw, err := blob.Writer()
	require.NoError(r.t, err)
	_, err = bw.Write([]byte(name + "\n"))
	require.NoError(r.t, err)
	require.NoError(r.t, bw.Close())
	files[name], err = r.st.SetEncodedObject(blob)
	require.NoError(r.t, err)

	tree := &object.Tree{}
	for _, n := range slices.Sorted(maps.Keys(files)) {
		tree.Entries = append(tree.Entries, object.TreeEntry{Name: n, Mode: filemode.Regular, Hash: files[n]})
	}
	to := r.st.NewEncodedObject()
	require.NoError(r.t, tree.Encode(to))
	treeHash, err := r.st.SetEncodedObject(to)
	require.NoError(r.t, err)

	r.when = r.when.Add(time.Minute)
	sig := object.Signature{Name: "t", Email: "t@t", When: r.when}
	c := &object.Commit{Author: sig, Committer: sig, Message: name, TreeHash: treeHash, ParentHashes: parentHashes}
	co := r.st.NewEncodedObject()
	require.NoError(r.t, c.Encode(co))
	h, err := r.st.SetEncodedObject(co)
	require.NoError(r.t, err)

	r.commits[name] = h
	r.files[h] = files
	return h
}

// branch points refs/heads/name at the named commit. git upload-pack only
// serves wants that an advertised ref points at.
func (r *canonicalRepo) branch(name, commit string) {
	r.t.Helper()
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), r.commits[commit])
	require.NoError(r.t, r.st.SetReference(ref))
}

// names maps hashes back to commit names, for readable assertions.
func (r *canonicalRepo) names(hashes []plumbing.Hash) []string {
	byHash := map[plumbing.Hash]string{}
	for n, h := range r.commits {
		byHash[h] = n
	}
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		out = append(out, byHash[h])
	}
	slices.Sort(out)
	return out
}

// mergeHistory builds:
//
//	A - B - C - M - D   (main)
//	     \     /
//	      X - Y
//
// D's distance to B is 3 through C but 4 through Y, so the shallow boundary
// depends on following merge parents and on taking the shortest path.
func mergeHistory(t testing.TB) *canonicalRepo {
	t.Helper()
	r := newCanonicalRepo(t)
	r.commit("A")
	r.commit("B", "A")
	r.commit("X", "B")
	r.commit("Y", "X")
	r.commit("C", "B")
	r.commit("M", "C", "Y")
	r.commit("D", "M")
	r.branch("main", "D")
	return r
}

func TestGetShallowCommitsFollowsMergesAlongShortestPaths(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)

	// Expected boundaries are those of git clone --depth <n> of the same
	// history (.git/shallow), verified with git 2.54.
	for depth, want := range map[int][]string{
		1: {"D"},
		2: {"M"},
		3: {"C", "Y"},
		4: {"B", "X"},
		5: {"A"},
		6: {},
	} {
		got, err := getShallowCommits(r.st, []plumbing.Hash{r.commits["D"]}, depth)
		require.NoError(t, err)
		require.Equal(t, want, r.names(got), "depth %d", depth)
	}
}
