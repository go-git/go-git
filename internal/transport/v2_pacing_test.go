package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

// The batch sizes git 2.54.0 sends in a v2 fetch, taken from a packet
// trace of a client with 300000 commits the server lacks.
func TestNextHaveBatchFollowsGit(t *testing.T) {
	t.Parallel()

	measured := []int{
		16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384,
		18022, 19824, 21806, 23986, 26384, 29022, 31924, 35116, 38627,
	}

	batch := firstHaveBatch
	for round, want := range measured {
		assert.Equal(t, want, batch, "round %d", round+1)
		batch = nextHaveBatch(batch)
	}
}

// errStopAfterDone ends a negotiation once the client has sent "done";
// the tests here look at the requests, not at a pack.
var errStopAfterDone = errors.New("stop after done")

// traceHaveRounds runs FetchV2 with count haves against a server that
// acknowledges the haves in acked, and returns for each round how many
// haves it offered for the first time, and whether it carried "done".
func traceHaveRounds(t *testing.T, count int, acked ...int) (newHaves []int, done []bool) {
	t.Helper()

	// FetchV2 offers the haves from the end of the list.
	haves := make([]plumbing.Hash, count)
	for i := range haves {
		haves[i] = plumbing.NewHash(fmt.Sprintf("%040x", count-i))
	}
	ackable := map[plumbing.Hash]bool{}
	for _, n := range acked {
		ackable[plumbing.NewHash(fmt.Sprintf("%040x", n))] = true
	}

	offered := map[plumbing.Hash]bool{}
	acknowledged := map[plumbing.Hash]bool{}
	round := func(args *packp.FetchArgs) (*packp.FetchOutput, io.Reader, error) {
		acks := &packp.Acknowledgments{}
		fresh := 0
		for _, h := range args.Haves {
			if !offered[h] {
				offered[h] = true
				fresh++
			}
			if ackable[h] {
				acks.ACKs = append(acks.ACKs, h)
			}
		}
		// The server keeps nothing between requests, so every request has
		// to carry the haves acknowledged before it again.
		require.Equal(t, len(acknowledged), len(args.Haves)-fresh,
			"round %d repeats the haves acknowledged so far", len(newHaves)+1)
		for _, h := range acks.ACKs {
			acknowledged[h] = true
		}
		newHaves = append(newHaves, fresh)
		done = append(done, args.Done)
		if args.Done {
			return nil, nil, errStopAfterDone
		}
		return &packp.FetchOutput{Acknowledgments: acks}, nil, nil
	}

	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: haves,
	}
	err := FetchV2(context.Background(), memory.NewStorage(), req, round)
	require.ErrorIs(t, err, errStopAfterDone)
	return newHaves, done
}

// Each case is a fetch traced from git 2.54.0 with GIT_TRACE_PACKET, over
// file:// and over smart HTTP alike: the new haves per request, and the
// request that carried "done".
func TestFetchV2HaveRoundsFollowGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		haves    int
		acked    []int
		newHaves []int
	}{
		{
			name:     "nothing in common",
			haves:    6000,
			newHaves: []int{16, 32, 64, 128, 256, 512, 1024, 2048, 1920, 0},
		},
		{
			name:     "first have acknowledged, the rest unknown to the server",
			haves:    6001,
			acked:    []int{1},
			newHaves: []int{16, 32, 64, 128, 256},
		},
		{
			name:     "acknowledgement in the fourth round, then exactly 256 unacknowledged",
			haves:    6001,
			acked:    []int{131},
			newHaves: []int{16, 32, 64, 128, 256},
		},
		{
			name:     "fewer haves than the first batch",
			haves:    5,
			newHaves: []int{5, 0},
		},
		{
			name:     "as many haves as the first batch",
			haves:    16,
			newHaves: []int{16, 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newHaves, done := traceHaveRounds(t, tc.haves, tc.acked...)
			assert.Equal(t, tc.newHaves, newHaves)
			assert.Equal(t, len(done)-1, slices.Index(done, true), "only the last round carries done")
		})
	}
}
