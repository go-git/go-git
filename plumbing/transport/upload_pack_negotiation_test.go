package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// negotiate serves a stateless v0/v1 request in a single round with git and
// with go-git, and returns the ACK and NAK lines of each. The request is
// written by hand because packp sorts wants and haves, and git's answers
// depend on their order. The haves u1 and u2 name objects the server does not
// have, and blob:X names the blob that commit X adds.
func negotiate(t testing.TB, r *canonicalRepo, mode string, wants, shallows []string, depth int, haves []string, done bool) (canonical, got []string) {
	t.Helper()
	unknown := map[string]plumbing.Hash{
		"u1": plumbing.NewHash("0123456789abcdef0123456789abcdef01234567"),
		"u2": plumbing.NewHash("1123456789abcdef0123456789abcdef01234567"),
	}

	var caps []string
	if mode != "" {
		caps = append(caps, mode)
	}
	if depth > 0 || len(shallows) > 0 {
		caps = append(caps, "shallow")
	}
	var req bytes.Buffer
	writeLine := func(format string, a ...any) {
		_, err := pktline.Writef(&req, format, a...)
		require.NoError(t, err)
	}
	for i, w := range r.hashes(wants) {
		if i == 0 && len(caps) > 0 {
			writeLine("want %s %s\n", w, strings.Join(caps, " "))
		} else {
			writeLine("want %s\n", w)
		}
	}
	for _, s := range r.hashes(shallows) {
		writeLine("shallow %s\n", s)
	}
	if depth > 0 {
		writeLine("deepen %d\n", depth)
	}
	require.NoError(t, pktline.WriteFlush(&req))
	for _, n := range haves {
		h, ok := unknown[n]
		if c, isBlob := strings.CutPrefix(n, "blob:"); isBlob {
			h = r.files[r.commits[c]][c]
		} else if !ok {
			h = r.commits[n]
		}
		writeLine("have %s\n", h)
	}
	if done {
		writeLine("done\n")
	} else {
		require.NoError(t, pktline.WriteFlush(&req))
	}

	// The shallow update is compared by the shallow tests, which do not
	// depend on its order.
	ackLines := func(raw []byte) []string {
		return slices.DeleteFunc(responseLines(t, raw), func(l string) bool {
			return strings.HasPrefix(l, "shallow ") || strings.HasPrefix(l, "unshallow ")
		})
	}
	return ackLines(canonicalUploadPack(t, r, req.Bytes())), ackLines(goGitUploadPack(t, r, req.Bytes()))
}

// requireReachWalk skips the test unless git decides readiness with
// can_all_from_reach_with_flag, which ok_to_give_up adopted in git 2.20.
// Older versions walk from each want separately, in request order, and share
// no visited commits between those walks, so they can withhold a "ready" or
// "continue" that later versions send.
func requireReachWalk(t testing.TB) {
	t.Helper()
	requireGitAtLeast(t, 2, 20, "the can_all_from_reach_with_flag readiness walk")
}

func TestUploadPackV1NegotiationMatchesCanonical(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"", "multi_ack", "multi_ack_detailed"} {
		for _, tc := range []struct {
			name     string
			wants    []string
			haves    []string
			shallows []string
			depth    int
			// reachWalk marks cases whose ACKs depend on requireReachWalk.
			reachWalk bool
		}{
			{name: "unknown have", wants: []string{"D"}, haves: []string{"u1"}},
			{name: "ancestor have", wants: []string{"D"}, haves: []string{"C"}},
			{name: "have on another branch", wants: []string{"Y"}, haves: []string{"C"}},
			// Upstream counts a commit's parents as had before checking the
			// commit itself, which decides when plain ACKs stop.
			{name: "have and its parent", wants: []string{"D"}, haves: []string{"M", "C"}},
			{name: "parent before child", wants: []string{"D"}, haves: []string{"C", "M"}},
			{name: "unknown then ancestor", wants: []string{"D"}, haves: []string{"u1", "C"}},
			// Upstream takes any object it has as common, so the final ACK
			// after "done" can name a blob.
			{name: "ancestor then blob", wants: []string{"D"}, haves: []string{"C", "blob:A"}},
			{name: "blob then unknown", wants: []string{"D"}, haves: []string{"blob:A", "u1"}},
			{name: "two wants anchored through a parent", wants: []string{"D", "Y"}, haves: []string{"C"}},
			{name: "two wants, one unrelated", wants: []string{"D", "Z"}, haves: []string{"C"}},
			{name: "two wants through a shared ancestor", wants: []string{"D", "Y"}, haves: []string{"A"}},
			{name: "descendant of the want", wants: []string{"M"}, haves: []string{"D"}},
			// W2 is older than the common C2, so the walk from W1 stops at it,
			// yet upstream still finds W2's parent D among the had commits
			// when it walks from W2 itself.
			{name: "want and its parent older than the common commit", wants: []string{"W1", "W2"}, haves: []string{"C2"}, reachWalk: true},
			{name: "want and its parent older than the common commit, parent first", wants: []string{"W2", "W1"}, haves: []string{"C2"}, reachWalk: true},
			// Upstream walks the wants oldest first, so W2 is known to reach
			// D by the time the walk from W3 stops at it.
			{name: "want whose only parent is an older want", wants: []string{"W3", "W2"}, haves: []string{"C2"}, reachWalk: true},
			// Upstream send_unshallow adds the parents of the unshallowed M to
			// the wants before negotiation, so C and Y must reach a common
			// commit too.
			{name: "deepen a shallow clone", wants: []string{"D"}, haves: []string{"D"}, shallows: []string{"M"}, depth: 3},
			// Upstream grafts the client's shallow commits before negotiating,
			// so the shallow D does not make its parent M, which F forks from,
			// count as had.
			{name: "fork below a shallow tip", wants: []string{"F"}, haves: []string{"D"}, shallows: []string{"D"}},
			{name: "fork below a shallow tip, then unknown", wants: []string{"F"}, haves: []string{"D", "u1"}, shallows: []string{"D"}},
			// The root K is already had as N's parent, so it adds no common
			// commit, but it lowers the cutoff below L, and the walk from V
			// now reaches K: upstream answers the second u1 with "ready".
			{name: "had root that lowers the cutoff", wants: []string{"V"}, haves: []string{"N", "u1", "K", "u1"}},
			// R moves no cutoff and has no parents, but is a new common
			// commit that W reaches: upstream answers the second u1 with
			// "ready".
			{name: "newer common root", wants: []string{"W"}, haves: []string{"O", "u1", "R", "u1"}},
		} {
			for _, done := range []bool{false, true} {
				name := mode + "/" + tc.name
				if done {
					name += "/done"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					// The gated cases send only known haves, for which
					// upstream reports readiness solely in the "ACK ready"
					// that multi_ack_detailed sends at a flush.
					if tc.reachWalk && mode == "multi_ack_detailed" && !done {
						requireReachWalk(t)
					}
					r := mergeHistory(t)
					r.branch("side", "Y")
					r.branch("merge", "M")
					r.commit("Z")
					r.branch("z", "Z")
					r.commit("W2", "D")
					r.commit("C2", "D")
					r.commit("W1", "W2", "C2")
					r.commit("W3", "W2")
					r.branch("w1", "W1")
					r.branch("w2", "W2")
					r.branch("w3", "W3")
					r.commit("F", "M")
					r.branch("f", "F")
					r.commit("O")
					r.branch("o", "O")
					r.commit("R")
					r.commit("W", "R")
					r.branch("w", "W")
					r.commit("K")
					r.commit("L", "K")
					r.commit("N", "K")
					r.commit("V", "L")
					r.branch("n", "N")
					r.branch("v", "V")

					canonical, got := negotiate(t, r, mode, tc.wants, tc.shallows, tc.depth, tc.haves, done)
					require.Equal(t, canonical, got)
				})
			}
		}
	}
}

// TestUploadPackV1NegotiationOnSkewedHistoryMatchesCanonical covers commits
// dated before their parents, or at the same second as the cutoff. Upstream's
// readiness walk then depends on what earlier walks in the same request
// marked, so it can say "ready" for one have and not for a later one.
func TestUploadPackV1NegotiationOnSkewedHistoryMatchesCanonical(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		build func(at func(minutes int, name string, parents ...string))
		wants []string
		haves []string
		// reachWalk marks cases whose ACKs depend on requireReachWalk.
		reachWalk bool
	}{
		{
			// S is dated before its ancestor W2. Each unknown have walks
			// again from scratch, so X reaching Cm through S in the first
			// walk does not carry over to the walk from T.
			name: "commit that reached a common one in an earlier walk",
			build: func(at func(int, string, ...string)) {
				at(1, "A")
				at(2, "W2", "A")
				at(3, "Cm", "A")
				at(4, "X", "W2")
				at(-60, "S", "X", "Cm")
				at(11, "T", "X")
				at(12, "U", "Cm")
			},
			wants: []string{"S", "W2", "T"},
			haves: []string{"Cm", "u1", "U", "u2"},
		},
		{
			// Cm2 lowers the cutoff below W, so the walk from S1 now visits X
			// before the walk from W can mark it.
			name: "ready withdrawn after the cutoff moves earlier",
			build: func(at func(int, string, ...string)) {
				at(1, "A")
				at(2, "W", "A")
				at(3, "Cm2", "A")
				at(6, "X", "W")
				at(4, "Y", "X")
				at(5, "Cm", "A")
				at(-60, "S1", "Y", "Cm")
				at(10, "T", "X")
			},
			wants:     []string{"S1", "W", "T"},
			haves:     []string{"Cm", "u1", "Cm2", "u2"},
			reachWalk: true,
		},
		{
			// Returning to P after C1 reached Cm, upstream still walks P's
			// other parent C2 and leaves X visited but unmarked, so the
			// walk from T cannot reach Cm through it.
			name: "parent walked after a sibling reached a common commit",
			build: func(at func(int, string, ...string)) {
				at(1, "A")
				at(2, "W", "A")
				at(3, "Cm", "A")
				at(6, "X", "W")
				at(5, "C1", "A")
				at(7, "C2", "X")
				at(8, "P", "C1", "C2")
				at(-60, "S", "P")
				at(10, "T", "X")
			},
			wants: []string{"S", "W", "T"},
			haves: []string{"Cm", "u1"},
		},
		{
			// P is dated the same second as the common Cm. Upstream skips
			// only parents dated before the cutoff, so the walk from W goes
			// through P and reaches A.
			name: "parent dated the same second as the cutoff",
			build: func(at func(int, string, ...string)) {
				at(1, "A")
				at(2, "Cm", "A")
				at(2, "P", "A")
				at(3, "W", "P")
			},
			wants: []string{"W"},
			haves: []string{"Cm", "u1"},
		},
	} {
		for _, mode := range []string{"", "multi_ack", "multi_ack_detailed"} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				// Only multi_ack and multi_ack_detailed answer an unknown
				// have with readiness.
				if tc.reachWalk && mode != "" {
					requireReachWalk(t)
				}
				r := newCanonicalRepo(t)
				base := r.when
				tc.build(func(minutes int, name string, parents ...string) {
					r.when = base.Add(time.Duration(minutes-1) * time.Minute)
					r.commit(name, parents...)
				})
				for _, n := range slices.Concat(tc.wants, tc.haves) {
					if _, ok := r.commits[n]; ok {
						r.branch(strings.ToLower(n), n)
					}
				}

				canonical, got := negotiate(t, r, mode, tc.wants, nil, 0, tc.haves, false)
				require.Equal(t, canonical, got)
			})
		}
	}
}

// shallowServerHistory builds a repository that is itself shallow:
//
//	P - X - W   (main)
//	 \
//	  M         (m)
//	 /
//	Q
//
// with P deleted and X and M listed in its shallow file, so neither has
// parents for upstream.
func shallowServerHistory(t testing.TB) *canonicalRepo {
	t.Helper()
	r := newCanonicalRepo(t)
	p := r.commit("P")
	r.commit("Q")
	r.commit("M", "P", "Q")
	r.commit("X", "P")
	r.commit("W", "X")
	r.branch("main", "W")
	r.branch("m", "M")
	require.NoError(t, r.st.SetShallow(r.hashes([]string{"X", "M"})))
	hex := p.String()
	require.NoError(t, os.Remove(filepath.Join(r.dir, "objects", hex[:2], hex[2:])))
	// Serve from storage that never read P.
	r.st = filesystem.NewStorage(osfs.New(r.dir), cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = r.st.Close() })
	return r
}

// TestUploadPackV1NegotiationOnShallowServerMatchesCanonical checks that the
// server's own shallow commits count as parentless: M's real parent P is not
// had, so the walk from W stops at X without reaching a common commit, and
// upstream answers neither M nor the unknown have that follows it with
// readiness.
func TestUploadPackV1NegotiationOnShallowServerMatchesCanonical(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "multi_ack", "multi_ack_detailed"} {
		for _, haves := range [][]string{{"M"}, {"M", "u1"}} {
			t.Run(mode+"/"+strings.Join(haves, ","), func(t *testing.T) {
				t.Parallel()
				r := shallowServerHistory(t)
				canonical, got := negotiate(t, r, mode, []string{"W"}, nil, 0, haves, false)
				require.Equal(t, canonical, got)
				if mode == "multi_ack_detailed" {
					require.Equal(t, []string{"ACK " + r.commits["M"].String() + " common", "NAK"}, got)
				}
			})
		}
	}
}

// TestUploadPackV2NegotiationOnShallowServerMatchesCanonical is the v2
// counterpart of TestUploadPackV1NegotiationOnShallowServerMatchesCanonical.
func TestUploadPackV2NegotiationOnShallowServerMatchesCanonical(t *testing.T) {
	t.Parallel()
	// Versions before protocol v2 parse the command as a v0 want line.
	requireGitAtLeast(t, 2, 18, "protocol v2")
	r := shallowServerHistory(t)

	req, err := io.ReadAll(v2Request(t, "fetch", nil, []string{
		"want " + r.commits["W"].String(),
		"have " + r.commits["M"].String(),
	}))
	require.NoError(t, err)

	canonical := canonicalUploadPack(t, r, req, "GIT_PROTOCOL=version=2")
	got := serveUploadPackV2Test(t, r.st, io.NopCloser(bytes.NewReader(req)))
	require.Equal(t, string(canonical), got)
	require.NotContains(t, got, "ready")
}

// mustCommonHaves returns newCommonHaves(st, wants, grafts), failing tb on an
// error.
func mustCommonHaves(tb testing.TB, st storage.Storer, wants, grafts []plumbing.Hash) *commonHaves {
	tb.Helper()
	c, err := newCommonHaves(st, wants, grafts)
	require.NoError(tb, err)
	return c
}

// countingStorer counts the objects read through it.
type countingStorer struct {
	storage.Storer
	reads int
}

func (s *countingStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	s.reads++
	return s.Storer.EncodedObject(t, h)
}

func TestCommonHavesKeepsReadinessForHavesThatCannotChangeIt(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	st := &countingStorer{Storer: r.st}
	common := mustCommonHaves(t, st, r.hashes([]string{"C"}), nil)

	_, err := common.add(r.commits["Y"])
	require.NoError(t, err)
	ready, err := common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.False(t, ready)

	_, err = common.add(r.commits["Y"])
	require.NoError(t, err)
	_, err = common.add(r.files[r.commits["A"]]["A"])
	require.NoError(t, err)
	reads := st.reads
	ready, err = common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.False(t, ready)
	require.Equal(t, reads, st.reads, "a repeated have or a blob must not trigger another walk")

	_, err = common.add(r.commits["B"])
	require.NoError(t, err)
	ready, err = common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.True(t, ready)

	_, err = common.add(r.commits["B"])
	require.NoError(t, err)
	reads = st.reads
	ready, err = common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, reads, st.reads, "a repeated have must not trigger another walk")
}

func TestCommonHavesWalksAgainWithoutReadingCommitsAgain(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	st := &countingStorer{Storer: r.st}
	common := mustCommonHaves(t, st, r.hashes([]string{"D"}), nil)

	// A blob sets no cutoff, so the first walk reads all of D's history
	// without reaching a common commit.
	_, err := common.add(r.files[r.commits["A"]]["A"])
	require.NoError(t, err)
	ready, err := common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.False(t, ready)

	// Each new common commit forces another walk, but neither the have nor
	// the walk reads a commit the first walk already read.
	for _, n := range []string{"Y", "X", "C"} {
		reads := st.reads
		_, err = common.add(r.commits[n])
		require.NoError(t, err)
		_, err = common.okToGiveUp(t.Context())
		require.NoError(t, err)
		require.Equal(t, reads, st.reads, "walk after %s read commits again", n)
	}
	require.True(t, common.ready)
}

func TestCommonHavesStopsWalkingWhenContextIsDone(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	common := mustCommonHaves(t, r.st, r.hashes([]string{"D"}), nil)
	_, err := common.add(r.files[r.commits["A"]]["A"])
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = common.okToGiveUp(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// TestCommonHavesReadsEachHaveOnce checks that a repeated have, common or not,
// is answered without reading it again, as upstream keeps parsed objects.
func TestCommonHavesReadsEachHaveOnce(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	st := &countingStorer{Storer: r.st}
	common := mustCommonHaves(t, st, r.hashes([]string{"D"}), nil)

	blob := r.files[r.commits["A"]]["A"]
	unknown := plumbing.NewHash(strings.Repeat("1", 40))
	for range 80 {
		had, err := common.add(blob)
		require.NoError(t, err)
		require.True(t, had)
		had, err = common.add(unknown)
		require.NoError(t, err)
		require.False(t, had)
	}
	require.Equal(t, 2, st.reads)
	require.Equal(t, blob, common.last)
}

// TestCommonHavesPeelsWantsOnce checks that wants that peel to no commit are
// not peeled again on every walk.
func TestCommonHavesPeelsWantsOnce(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	st := &countingStorer{Storer: r.st}
	common := mustCommonHaves(t, st, []plumbing.Hash{r.files[r.commits["A"]]["A"]}, nil)

	_, err := common.add(r.commits["C"])
	require.NoError(t, err)
	_, err = common.okToGiveUp(t.Context())
	require.NoError(t, err)

	_, err = common.add(r.commits["X"])
	require.NoError(t, err)
	reads := st.reads
	_, err = common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.Equal(t, reads, st.reads, "the wants were peeled again")
}

// TestCommonHavesIgnoresCommitDatedZero checks that a common commit dated at
// the epoch does not pin the cutoff, as upstream reads an oldest_have of zero
// as unset.
func TestCommonHavesIgnoresCommitDatedZero(t *testing.T) {
	t.Parallel()
	r := newCanonicalRepo(t)
	r.when = time.Unix(0, 0).Add(-time.Minute)
	r.commit("A")
	r.when = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	r.commit("B", "A")
	r.commit("C", "B")
	common := mustCommonHaves(t, r.st, r.hashes([]string{"C"}), nil)

	_, err := common.add(r.commits["A"])
	require.NoError(t, err)
	require.Zero(t, common.oldest)
	_, err = common.add(r.commits["B"])
	require.NoError(t, err)
	require.Equal(t, r.when.Add(-time.Minute).Unix(), common.oldest)
}

// cancelingStorer cancels a context on its first object read.
type cancelingStorer struct {
	countingStorer
	cancel context.CancelFunc
}

func (s *cancelingStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	s.cancel()
	return s.countingStorer.EncodedObject(t, h)
}

// TestUploadPackStopsReadingHavesWhenContextIsDone checks that the haves of a
// request are no longer looked up once its context is done.
func TestUploadPackStopsReadingHavesWhenContextIsDone(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	haves := make([]plumbing.Hash, 0, 100)
	for i := range 100 {
		haves = append(haves, plumbing.NewHash(fmt.Sprintf("%040x", i+1)))
	}
	req := encodeV1Request(t, &packp.UploadRequest{Wants: r.hashes([]string{"D"})}, haves...)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	st := &cancelingStorer{countingStorer: countingStorer{Storer: r.st}, cancel: cancel}
	err := UploadPack(ctx, st, io.NopCloser(bytes.NewReader(req)), ioutil.WriteNopCloser(io.Discard),
		&UploadPackRequest{GitProtocol: "version=1", StatelessRPC: true})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, st.reads, "haves were looked up after the context was done")
}

// blobRefusingStorer fails to read any blob, as reading one in full is what
// a have naming a large blob must not cost.
type blobRefusingStorer struct {
	*memory.Storage
}

func (s blobRefusingStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.Storage.EncodedObject(t, h)
	if err == nil && obj.Type() == plumbing.BlobObject {
		return nil, errors.New("blob read in full")
	}
	return obj, err
}

func TestCommonHavesLooksUpTheTypeOfABlobHave(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	blob := st.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("content"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	h, err := st.SetEncodedObject(blob)
	require.NoError(t, err)

	common := mustCommonHaves(t, blobRefusingStorer{st}, nil, nil)
	had, err := common.add(h)
	require.NoError(t, err)
	assert.True(t, had)
}

// A have that is already in theyHave, as an earlier have's parent, can
// still bring new parents, and so change readiness without moving the
// cutoff. The answer okToGiveUp gave before must not be reused.
func TestCommonHavesWalksAgainForNewParentsOfAKnownHave(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	root := benchCommit(t, st, 1)
	a := benchCommit(t, st, 2, root)
	want := benchCommit(t, st, 3, a)
	// x is newer than c, its child, so adding x does not move the cutoff.
	x := benchCommit(t, st, 20, a)
	c := benchCommit(t, st, 10, x)

	common := mustCommonHaves(t, st, []plumbing.Hash{want}, nil)
	had, err := common.add(c)
	require.NoError(t, err)
	require.True(t, had)
	ready, err := common.okToGiveUp(context.Background())
	require.NoError(t, err)
	require.False(t, ready, "want does not reach c or x")

	had, err = common.add(x)
	require.NoError(t, err)
	require.True(t, had)
	ready, err = common.okToGiveUp(context.Background())
	require.NoError(t, err)
	assert.True(t, ready, "x's parent a is the want's parent")
}

// A parent the server lacks still counts as had when the client has it, as
// the parent of a common commit, as upstream's THEY_HAVE flag marks it.
func TestCommonHavesCountsAMissingParentTheClientHas(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	missing := plumbing.NewHash("1111111111111111111111111111111111111111")
	want := benchCommit(t, st, 3, missing)
	c := benchCommit(t, st, 2, missing)

	common := mustCommonHaves(t, st, []plumbing.Hash{want}, nil)
	had, err := common.add(c)
	require.NoError(t, err)
	require.True(t, had)
	ready, err := common.okToGiveUp(context.Background())
	require.NoError(t, err)
	assert.True(t, ready)
}

// unreadableStorer fails to read the object bad in full, as a corrupt or
// unreachable object would, while still telling its type.
type unreadableStorer struct {
	*memory.Storage
	bad plumbing.Hash
}

var errUnreadable = errors.New("object cannot be read")

func (s unreadableStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if h == s.bad {
		return nil, errUnreadable
	}
	return s.Storage.EncodedObject(t, h)
}

// tagObject stores an annotated tag of target, of type typ, and returns its
// hash.
func tagObject(t *testing.T, st *memory.Storage, target plumbing.Hash, typ plumbing.ObjectType) plumbing.Hash {
	t.Helper()
	tag := &object.Tag{Name: "t", Tagger: object.Signature{Name: "t", Email: "t@t"}, Target: target, TargetType: typ}
	o := st.NewEncodedObject()
	require.NoError(t, tag.Encode(o))
	h, err := st.SetEncodedObject(o)
	require.NoError(t, err)
	return h
}

// TestCommonHavesWantsThatDoNotPeelToACommit checks which wants okToGiveUp
// skips, as upstream can_all_from_reach_with_flag does: one that peels to an
// object that is not a commit, or to a tag whose target is missing. A want
// that cannot be read is an error rather than skipped, where upstream dies
// on a want it cannot parse.
func TestCommonHavesWantsThatDoNotPeelToACommit(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	root := benchCommit(t, st, 1)
	want := benchCommit(t, st, 2, root)
	other := benchCommit(t, st, 3)
	blob := st.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	blobHash, err := st.SetEncodedObject(blob)
	require.NoError(t, err)
	missing := plumbing.NewHash("1111111111111111111111111111111111111111")
	otherTag := tagObject(t, st, other, plumbing.CommitObject)

	for _, tc := range []struct {
		name  string
		wants []plumbing.Hash
		bad   plumbing.Hash
		ready bool
		err   error
	}{
		{name: "commit", wants: []plumbing.Hash{want}, ready: true},
		{name: "unrelated commit", wants: []plumbing.Hash{want, other}},
		{name: "tag of an unrelated commit", wants: []plumbing.Hash{want, otherTag}},
		{name: "blob", wants: []plumbing.Hash{want, blobHash}, ready: true},
		{name: "tag of a blob", wants: []plumbing.Hash{want, tagObject(t, st, blobHash, plumbing.BlobObject)}, ready: true},
		{name: "tag of a missing object", wants: []plumbing.Hash{want, tagObject(t, st, missing, plumbing.CommitObject)}, ready: true},
		{name: "missing", wants: []plumbing.Hash{want, missing}, err: plumbing.ErrObjectNotFound},
		{name: "unreadable commit", wants: []plumbing.Hash{want, other}, bad: other, err: errUnreadable},
		{name: "unreadable tag", wants: []plumbing.Hash{want, otherTag}, bad: otherTag, err: errUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			common := mustCommonHaves(t, unreadableStorer{Storage: st, bad: tc.bad}, tc.wants, nil)
			had, err := common.add(root)
			require.NoError(t, err)
			require.True(t, had)
			ready, err := common.okToGiveUp(t.Context())
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.False(t, ready)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.ready, ready)
		})
	}
}
