package transport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// depthGraph stores one commit per entry of parents, each listing the
// indexes of its parents among the earlier entries, and returns their hashes.
func depthGraph(tb testing.TB, parents [][]int) (*memory.Storage, []plumbing.Hash) {
	tb.Helper()

	st := memory.NewStorage()
	sig := object.Signature{Name: "t", Email: "t@example.com", When: time.Unix(0, 0).UTC()}
	hashes := make([]plumbing.Hash, len(parents))
	for i, ps := range parents {
		c := &object.Commit{Author: sig, Committer: sig, Message: fmt.Sprintf("c%d", i)}
		for _, p := range ps {
			c.ParentHashes = append(c.ParentHashes, hashes[p])
		}
		o := &plumbing.MemoryObject{}
		require.NoError(tb, c.Encode(o))
		h, err := st.SetEncodedObject(o)
		require.NoError(tb, err)
		hashes[i] = h
	}
	return st, hashes
}

func TestClientBoundaryDepth(t *testing.T) {
	t.Parallel()

	// 0 <- 1 <- 2 <- 3 <- 4 <- 5 is the long way from the tip 6 to 0, and
	// 6 also has 1 as a parent: 0 <- 1 <- 6.
	st, h := depthGraph(t, [][]int{{}, {0}, {1}, {2}, {3}, {4}, {5, 1}})
	pick := func(idx ...int) []plumbing.Hash {
		out := make([]plumbing.Hash, len(idx))
		for i, n := range idx {
			out[i] = h[n]
		}
		return out
	}
	missing := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")

	tests := []struct {
		name     string
		heads    []plumbing.Hash
		shallows []plumbing.Hash
		want     int
	}{
		{"no shallow commits", pick(6), nil, 0},
		{"the tip itself", pick(6), pick(6), 1},
		{"shortest of two paths", pick(6), pick(0), 3},
		{"closest of two shallow commits on one line", pick(6), pick(3, 4), 3},
		{"a closer one behind a farther one in walk order", pick(6), pick(5, 1), 2},
		{"not reachable from the heads", pick(2), pick(4), 0},
		{"closest over several heads", pick(5, 3), pick(2), 2},
		{"a head that is not a commit is left out", append(pick(6), missing), pick(1), 2},
		{"a shallow commit the server lacks", pick(6), []plumbing.Hash{missing}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := clientBoundaryDepth(context.Background(), st, tc.heads, tc.shallows)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// BenchmarkClientBoundaryDepth measures a deepen-relative request against a
// long history: the client's boundary is a few commits below the tip, as
// after a clone with a small depth. In the ladder every commit has the two
// commits before it as parents, so each one is reachable along many paths.
func BenchmarkClientBoundaryDepth(b *testing.B) {
	const commits = 20000
	shapes := map[string]func(i int) []int{
		"linear": func(i int) []int {
			if i == 0 {
				return nil
			}
			return []int{i - 1}
		},
		"ladder": func(i int) []int {
			switch i {
			case 0:
				return nil
			case 1:
				return []int{0}
			}
			return []int{i - 1, i - 2}
		},
	}
	for _, shape := range []string{"linear", "ladder"} {
		parents := make([][]int, commits)
		for i := range parents {
			parents[i] = shapes[shape](i)
		}
		st, h := depthGraph(b, parents)
		heads := []plumbing.Hash{h[commits-1]}

		for _, at := range []int{10, commits - 1} {
			shallows := []plumbing.Hash{h[commits-1-at]}
			b.Run(fmt.Sprintf("%s/boundary_%d_below_tip", shape, at), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := clientBoundaryDepth(context.Background(), st, heads, shallows); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
