package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
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
	st := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	require.NoError(t, st.Init())
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))))
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

// hashes maps commit names to their hashes.
func (r *canonicalRepo) hashes(names []string) []plumbing.Hash {
	out := make([]plumbing.Hash, 0, len(names))
	for _, n := range names {
		out = append(out, r.commits[n])
	}
	return out
}

// encodeV1Request encodes a stateless v0/v1 upload-pack request: the
// upload-request followed by a single round of haves ending in done.
func encodeV1Request(t testing.TB, upreq *packp.UploadRequest, haves ...plumbing.Hash) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, upreq.Encode(&buf))
	require.NoError(t, (&packp.UploadHaves{Haves: haves, Done: true}).Encode(&buf))
	return buf.Bytes()
}

// canonicalUploadPack serves req with git upload-pack, with env added to its
// environment.
func canonicalUploadPack(t testing.TB, r *canonicalRepo, req []byte, env ...string) []byte {
	t.Helper()
	cmd := gitenv.Command("git", "upload-pack", "--stateless-rpc", r.dir)
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = bytes.NewReader(req)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())
	return out
}

// goGitUploadPack serves the stateless v0/v1 request req with go-git.
func goGitUploadPack(t testing.TB, r *canonicalRepo, req []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, UploadPack(context.Background(), r.st, io.NopCloser(bytes.NewReader(req)),
		ioutil.WriteNopCloser(&out), &UploadPackRequest{GitProtocol: "version=1", StatelessRPC: true}))
	return out.Bytes()
}

// v1Response is a stateless v0/v1 upload-pack response without sideband.
type v1Response struct {
	shallows, unshallows []string
	// acks holds the ACK and NAK lines in order.
	acks []string
	pack *memory.Storage
}

// parseV1Response splits a v0/v1 response into its shallow, unshallow and
// ACK or NAK lines and the pack that follows them.
func parseV1Response(t testing.TB, r *canonicalRepo, raw []byte) v1Response {
	t.Helper()
	at := bytes.Index(raw, []byte("PACK"))
	require.NotEqual(t, -1, at, "response must carry a pack: %q", raw)

	var resp v1Response
	rd := bytes.NewReader(raw[:at])
	for {
		l, line, err := pktline.ReadLine(rd)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if l == pktline.Flush {
			continue
		}
		// git omits the optional trailing newline on shallow lines.
		text := strings.TrimSuffix(string(line), "\n")
		if hash, ok := strings.CutPrefix(text, "shallow "); ok {
			resp.shallows = append(resp.shallows, r.names([]plumbing.Hash{plumbing.NewHash(hash)})...)
		} else if hash, ok := strings.CutPrefix(text, "unshallow "); ok {
			resp.unshallows = append(resp.unshallows, r.names([]plumbing.Hash{plumbing.NewHash(hash)})...)
		} else {
			resp.acks = append(resp.acks, text)
		}
	}
	slices.Sort(resp.shallows)
	slices.Sort(resp.unshallows)

	resp.pack = memory.NewStorage()
	require.NoError(t, packfile.UpdateObjectStorage(resp.pack, bytes.NewReader(raw[at:])))
	return resp
}

// commitNames lists the commits in a pack by name.
func (r *canonicalRepo) commitNames(pack *memory.Storage) []string {
	r.t.Helper()
	var hashes []plumbing.Hash
	for _, h := range r.commits {
		if _, err := pack.EncodedObject(plumbing.CommitObject, h); err == nil {
			hashes = append(hashes, h)
		}
	}
	return r.names(hashes)
}

// requireComplete runs the connectivity check git runs after a fetch: a
// client that held the history reachable from haves, bounded by its shallow
// commits, and then received resp must have every object reachable from the
// wants, bounded by its updated shallow commits. Comparing packs with git's
// directly would not work, as git re-sends objects the client already has.
func (r *canonicalRepo) requireComplete(wants, haves, clientShallows []string, resp v1Response) {
	r.t.Helper()
	client := memory.NewStorage()
	had, err := revlist.Objects(&shallowBoundaryStorer{Storer: r.st, boundary: r.hashes(clientShallows)}, r.hashes(haves), nil)
	require.NoError(r.t, err)
	for _, h := range had {
		o, err := r.st.EncodedObject(plumbing.AnyObject, h)
		require.NoError(r.t, err)
		_, err = client.SetEncodedObject(o)
		require.NoError(r.t, err)
	}
	iter, err := resp.pack.IterEncodedObjects(plumbing.AnyObject)
	require.NoError(r.t, err)
	require.NoError(r.t, iter.ForEach(func(o plumbing.EncodedObject) error {
		_, err := client.SetEncodedObject(o)
		return err
	}))

	shallow := map[string]struct{}{}
	for _, s := range append(slices.Clone(clientShallows), resp.shallows...) {
		shallow[s] = struct{}{}
	}
	for _, s := range resp.unshallows {
		delete(shallow, s)
	}
	require.NoError(r.t, client.SetShallow(r.hashes(slices.Collect(maps.Keys(shallow)))))

	// Objects lists blobs without reading them, so check each is present.
	needed, err := revlist.Objects(client, r.hashes(wants), nil)
	require.NoError(r.t, err)
	for _, h := range needed {
		_, err := client.EncodedObject(plumbing.AnyObject, h)
		require.NoError(r.t, err, "client is missing %s after the fetch", h)
	}
}

func TestUploadPackV1DeepenMatchesCanonical(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)

	for depth := 1; depth <= 6; depth++ {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			t.Parallel()
			upreq := &packp.UploadRequest{}
			upreq.Capabilities.Add("shallow")
			upreq.Wants = []plumbing.Hash{r.commits["D"]}
			upreq.Depth.Deepen = depth
			req := encodeV1Request(t, upreq)

			canonical := parseV1Response(t, r, canonicalUploadPack(t, r, req))
			got := parseV1Response(t, r, goGitUploadPack(t, r, req))

			require.Equal(t, canonical.shallows, got.shallows, "shallow lines")
			require.Equal(t, canonical.unshallows, got.unshallows, "unshallow lines")
			require.Equal(t, canonical.acks, got.acks, "acknowledgements")
			require.Equal(t, r.commitNames(canonical.pack), r.commitNames(got.pack), "commits")
			r.requireComplete([]string{"D"}, nil, nil, got)
		})
	}
}

func TestUploadPackV1ShallowClientMatchesCanonical(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		want     string
		shallows []string
		haves    []string
		depth    int
	}{
		// A depth-2 clone (boundary M) deepened to 3: M becomes interior and
		// the boundary moves to its parents.
		{name: "deepen a shallow clone", want: "D", shallows: []string{"M"}, haves: []string{"D"}, depth: 3},
		{name: "deepen a shallow clone to full history", want: "D", shallows: []string{"M"}, haves: []string{"D"}, depth: 100},
		// A depth-1 clone of C fetching D without deepening: Y's history
		// forks below C, so the client is missing it and gets it in full.
		{name: "fetch into a shallow clone", want: "D", shallows: []string{"C"}, haves: []string{"C"}},
		// A client with the full history of B fetches D at depth 1. D's tree
		// holds files that only commits beyond the boundary introduced.
		{name: "shallow fetch by a client with older history", want: "D", haves: []string{"B"}, depth: 1},
		{name: "refetch a shallow clone at the same depth", want: "D", shallows: []string{"M"}, haves: []string{"D"}, depth: 2},
		{name: "unshallow a branch the wants do not reach", want: "C", shallows: []string{"Y"}, depth: infiniteDepth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := mergeHistory(t)
			r.branch("c", "C")

			upreq := &packp.UploadRequest{}
			upreq.Capabilities.Add("shallow")
			upreq.Wants = r.hashes([]string{tc.want})
			upreq.Shallows = r.hashes(tc.shallows)
			upreq.Depth.Deepen = tc.depth
			req := encodeV1Request(t, upreq, r.hashes(tc.haves)...)

			canonical := parseV1Response(t, r, canonicalUploadPack(t, r, req))
			got := parseV1Response(t, r, goGitUploadPack(t, r, req))

			require.Equal(t, canonical.shallows, got.shallows, "shallow lines")
			require.Equal(t, canonical.unshallows, got.unshallows, "unshallow lines")
			require.Equal(t, canonical.acks, got.acks, "acknowledgements")
			require.Equal(t, r.commitNames(canonical.pack), r.commitNames(got.pack), "commits")
			r.requireComplete([]string{tc.want}, tc.haves, tc.shallows, got)
		})
	}
}

func TestUploadPackV2ShallowFetchByClientWithOtherHistory(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	r.branch("side", "Y")

	out := serveUploadPackV2Test(t, r.st, v2Request(t, "fetch", nil, []string{
		"want " + r.commits["Y"].String(),
		"have " + r.commits["C"].String(),
		"deepen 1",
		"done",
	}))

	at := strings.Index(out, "packfile\n")
	require.NotEqual(t, -1, at)
	pack := memory.NewStorage()
	demux := sideband.NewDemuxer(sideband.Sideband64k, strings.NewReader(out[at+len("packfile\n"):]))
	require.NoError(t, packfile.UpdateObjectStorage(pack, demux))

	// X's file reaches the client only through Y's tree, since X itself is
	// beyond the boundary.
	_, err := pack.EncodedObject(plumbing.BlobObject, r.files[r.commits["X"]]["X"])
	require.NoError(t, err)
}

func TestGetShallowCommitsFollowsMergesAlongShortestPaths(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)

	// Expected boundaries are those of git clone --depth <n> of the same
	// history (.git/shallow), verified with git 2.54.
	for depth, want := range map[int][]string{
		1:             {"D"},
		2:             {"M"},
		3:             {"C", "Y"},
		4:             {"B", "X"},
		5:             {"A"},
		6:             {},
		infiniteDepth: {},
	} {
		got, err := getShallowCommits(r.st, []plumbing.Hash{r.commits["D"]}, depth)
		require.NoError(t, err)
		require.Equal(t, want, r.names(got), "depth %d", depth)
	}
}

// requireGitV2 skips the test unless git speaks protocol v2, which git
// introduced in 2.18. Older versions parse the command as a v0 want line.
func requireGitV2(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found in PATH")
	}
	out, err := gitenv.Command("git", "version").Output()
	if err != nil {
		t.Skipf("cannot run git version: %v", err)
	}
	fields := strings.Fields(string(out)) // e.g. "git version 2.39.2"
	if len(fields) < 3 {
		t.Skipf("cannot parse git version %q", out)
	}
	parts := strings.SplitN(fields[2], ".", 3)
	major, errMajor := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if errMajor != nil || major < 2 || (major == 2 && minor < 18) {
		t.Skipf("git %s does not support protocol v2 (need >= 2.18)", fields[2])
	}
}

func TestUploadPackV2DeepenBeyondHistoryMatchesCanonicalShallowInfo(t *testing.T) {
	t.Parallel()
	requireGitV2(t)
	r := mergeHistory(t)

	req, err := io.ReadAll(v2Request(t, "fetch", nil, []string{
		"want " + r.commits["D"].String(),
		"deepen 100",
		"done",
	}))
	require.NoError(t, err)

	canonical := canonicalUploadPack(t, r, req, "GIT_PROTOCOL=version=2")
	got := serveUploadPackV2Test(t, r.st, io.NopCloser(bytes.NewReader(req)))

	for name, out := range map[string]string{"git": string(canonical), "go-git": got} {
		start := strings.Index(out, "shallow-info")
		require.NotEqual(t, -1, start, "%s response has no shallow-info section", name)
		end := strings.Index(out, "packfile")
		require.Greater(t, end, start, "%s response has no packfile section", name)
		section := out[start:end]
		require.NotContains(t, section, "shallow ", name)
		require.NotContains(t, section, "unshallow ", name)
	}
}
