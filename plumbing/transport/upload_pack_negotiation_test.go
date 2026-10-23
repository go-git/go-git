package transport

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/storage"
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

					canonical, got := negotiate(t, r, mode, tc.wants, tc.shallows, tc.depth, tc.haves, done)
					require.Equal(t, canonical, got)
				})
			}
		}
	}
}

// TestUploadPackV1NegotiationOnSkewedHistoryMatchesCanonical covers commits
// dated before their parents. Upstream's readiness walk then depends on what
// earlier walks in the same request marked, so it can say "ready" for one
// have and not for a later one.
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
	common := newCommonHaves(st, r.hashes([]string{"C"}), nil)

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
	common := newCommonHaves(st, r.hashes([]string{"D"}), nil)

	// A blob sets no cutoff, so the first walk reads all of D's history
	// without reaching a common commit.
	_, err := common.add(r.files[r.commits["A"]]["A"])
	require.NoError(t, err)
	ready, err := common.okToGiveUp(t.Context())
	require.NoError(t, err)
	require.False(t, ready)

	// Each new common commit forces another walk, which only reads the
	// have itself.
	for _, n := range []string{"Y", "X", "C"} {
		reads := st.reads
		_, err = common.add(r.commits[n])
		require.NoError(t, err)
		_, err = common.okToGiveUp(t.Context())
		require.NoError(t, err)
		require.Equal(t, reads+1, st.reads, "walk after %s read commits again", n)
	}
	require.True(t, common.ready)
}

func TestCommonHavesStopsWalkingWhenContextIsDone(t *testing.T) {
	t.Parallel()
	r := mergeHistory(t)
	common := newCommonHaves(r.st, r.hashes([]string{"D"}), nil)
	_, err := common.add(r.files[r.commits["A"]]["A"])
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = common.okToGiveUp(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
