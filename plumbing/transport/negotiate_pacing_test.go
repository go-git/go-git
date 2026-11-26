package transport

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/storage/memory"
)

// The number of haves git 2.54.0 has offered by the end of each round of a
// v0 fetch, taken from packet traces of clients whose commits the server
// lacks: 300000 of them over smart HTTP, 6000 over file://.
func TestNextHaveTotalFollowsGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		statelessRPC bool
		measured     []int
	}{
		{
			name:         "stateless",
			statelessRPC: true,
			measured: []int{
				16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384,
				18022, 19824, 21806, 23986, 26384, 29022, 31924, 35116, 38627, 42489,
			},
		},
		{
			name:     "stateful",
			measured: []int{16, 32, 64, 96, 128, 160, 192, 224, 256, 288},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			total := firstHaveBatch
			for round, want := range tc.measured {
				assert.Equal(t, want, total, "round %d", round+1)
				total = nextHaveTotal(tc.statelessRPC, total)
			}
		})
	}
}

// answerScript holds the server's answers to a negotiation. It notes a read
// made after the last answer was taken, which a plain buffer would report as
// the end of the stream and a decoder as an empty answer.
type answerScript struct {
	bytes.Buffer
	overread bool
}

func (s *answerScript) Read(p []byte) (int, error) {
	if s.Len() == 0 {
		s.overread = true
	}
	return s.Buffer.Read(p)
}

// requireAllAnswersRead fails unless the client read every answer and asked
// for no more.
func requireAllAnswersRead(t *testing.T, s *answerScript) {
	t.Helper()
	require.Zero(t, s.Len(), "answers left unread")
	require.False(t, s.overread, "an answer was read that the server never sent")
}

// haveRounds returns the number of have lines in each round of a written
// negotiation. A round ends at the flush or the "done" that follows its
// haves.
func haveRounds(t *testing.T, written []byte) []int {
	t.Helper()

	var rounds []int
	haves := 0
	s := pktline.NewScanner(bytes.NewReader(written))
	for s.Scan() {
		line := string(s.Bytes())
		switch {
		case strings.HasPrefix(line, "have "):
			haves++
		case strings.HasPrefix(line, "done"), s.Len() == pktline.Flush && haves > 0:
			rounds = append(rounds, haves)
			haves = 0
		}
	}
	require.NoError(t, s.Err())
	return rounds
}

// Fetches traced from git 2.54.0 with GIT_TRACE_PACKET and
// protocol.version=0, by a client whose commits the server has none of: the
// haves per round over smart HTTP and over file://. The last round carries
// "done". When the haves run out in the middle of a round, that round is the
// last; when they run out exactly at the end of one, "done" follows in a
// round without haves.
func TestNegotiatePackHaveRoundsFollowGit(t *testing.T) {
	t.Parallel()

	stateful := slices.Concat([]int{16, 16}, slices.Repeat([]int{32}, 186), []int{16})

	tests := []struct {
		name         string
		statelessRPC bool
		haves        int
		rounds       []int
	}{
		{"stateless", true, 6000, []int{16, 16, 32, 64, 128, 256, 512, 1024, 2048, 1904}},
		{"stateful", false, 6000, stateful},
		{"stateful, haves end inside the second round", false, 20, []int{16, 4}},
		{"stateful, haves end inside the third round", false, 48, []int{16, 16, 16}},
		{"stateful, haves end with the first round", false, 16, []int{16, 0}},
		{"stateful, haves end with the second round", false, 32, []int{16, 16, 0}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := &answerScript{}
			// One answer per round.
			for range tc.rounds {
				_, err := pktline.WriteString(reader, "NAK\n")
				require.NoError(t, err)
			}
			haves := make([]plumbing.Hash, tc.haves)
			for i := range haves {
				haves[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
			}
			writer := newMockWriteCloser(nil)
			req := &FetchRequest{
				Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
				Haves: haves,
			}

			_, err := NegotiatePack(context.Background(), memory.NewStorage(), capability.List{}, tc.statelessRPC, reader, writer, req)
			require.NoError(t, err)

			assert.Equal(t, tc.rounds, haveRounds(t, writer.writeBuf.Bytes()))
			requireAllAnswersRead(t, reader)
		})
	}
}

// Fetches traced from git 2.54.0 over smart HTTP with protocol.version=0, in
// which the server acknowledges one have as common and nothing after it: the
// first have offered, and the 131st. The client had 6000 further commits the
// server lacks, and the server a branch the client shares nothing with, so
// that it never answers "ready".
//
// git checks the unacknowledged haves between rounds. The round that takes
// them past 256 is sent whole, and "done" follows in a request with no new
// haves; exactly 256 is not enough to stop. Every request repeats the common
// have, which is the extra have counted from the round after the
// acknowledgement on.
func TestNegotiatePackGivesUpLikeGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// common is the position, counted from 1, of the have the server
		// acknowledges, and ackRound the round it is offered in.
		common, ackRound int
		rounds           []int
	}{
		{"first have common", 1, 1, []int{16, 16 + 1, 32 + 1, 64 + 1, 128 + 1, 256 + 1, 0 + 1}},
		{"131st have common", 131, 5, []int{16, 16, 32, 64, 128, 256 + 1, 512 + 1, 0 + 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			haves := make([]plumbing.Hash, 6001)
			for i := range haves {
				haves[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
			}
			// Haves are offered from the end of the list.
			common := haves[len(haves)-tc.common]

			reader := &answerScript{}
			for round := 1; round <= len(tc.rounds); round++ {
				if round == tc.ackRound {
					_, err := pktline.WriteString(reader, "ACK "+common.String()+" common\n")
					require.NoError(t, err)
				}
				_, err := pktline.WriteString(reader, "NAK\n")
				require.NoError(t, err)
			}
			writer := newMockWriteCloser(nil)
			req := &FetchRequest{
				Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
				Haves: haves,
			}

			_, err := NegotiatePack(context.Background(), memory.NewStorage(), capability.List{}, true, reader, writer, req)
			require.NoError(t, err)

			written := writer.writeBuf.Bytes()
			assert.Equal(t, tc.rounds, haveRounds(t, written))
			assert.Equal(t, 1, strings.Count(string(written), "done\n"), "done is sent once, in the last request")
			requireAllAnswersRead(t, reader)
		})
	}
}

// Fetches traced from git 2.54.0 over file:// with protocol.version=0. The
// client had 6000 commits the server lacks, offered newest first, with one
// or two commits the server has placed among them; the server had a branch
// the client shares nothing with, so that it never answers "ready".
//
// On a stateful connection git writes a round before it has read the answer
// to the one before. An acknowledgement is therefore seen one round late,
// and the count of unacknowledged haves starts again when it is read, the
// round already written included. Nine more rounds of 32 take the count
// past 256, and "done" follows in a round of its own.
func TestNegotiatePackStatefulGivesUpLikeGit(t *testing.T) {
	t.Parallel()

	rounds := func(of32 int) []int {
		return slices.Concat([]int{16, 16}, slices.Repeat([]int{32}, of32), []int{0})
	}

	tests := []struct {
		name string
		// common are the positions, counted from 1, of the haves the
		// server acknowledges.
		common []int
		rounds []int
	}{
		{"1st have", []int{1}, rounds(9)},
		{"16th have, last of the first round", []int{16}, rounds(9)},
		{"17th have, first of the second round", []int{17}, rounds(10)},
		{"33rd have", []int{33}, rounds(11)},
		{"131st have", []int{131}, rounds(14)},
		{"300th have, after more than 256 unacknowledged", []int{300}, rounds(19)},
		{"1st and 200th have", []int{1, 200}, rounds(16)},
		{"20th and 40th have", []int{20, 40}, rounds(11)},
		{"1st and 320th have, the second in the last round offered", []int{1, 320}, rounds(9)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			haves := make([]plumbing.Hash, 6001)
			for i := range haves {
				haves[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
			}

			// One answer per round of haves, then the answer to "done".
			reader := &answerScript{}
			offered := 0
			for _, n := range tc.rounds[:len(tc.rounds)-1] {
				for _, pos := range tc.common {
					if pos > offered && pos <= offered+n {
						// Haves are offered from the end of the list.
						_, err := pktline.WriteString(reader, "ACK "+haves[len(haves)-pos].String()+" common\n")
						require.NoError(t, err)
					}
				}
				offered += n
				_, err := pktline.WriteString(reader, "NAK\n")
				require.NoError(t, err)
			}
			_, err := pktline.WriteString(reader, "NAK\n")
			require.NoError(t, err)

			writer := newMockWriteCloser(nil)
			req := &FetchRequest{
				Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
				Haves: haves,
			}

			_, err = NegotiatePack(context.Background(), memory.NewStorage(), capability.List{}, false, reader, writer, req)
			require.NoError(t, err)

			assert.Equal(t, tc.rounds, haveRounds(t, writer.writeBuf.Bytes()))
			requireAllAnswersRead(t, reader)
		})
	}
}

// A server that knows neither multi_ack nor multi_ack_detailed answers a
// round with NAK until a have is common, then with one acknowledgement
// without a status, and after that with nothing, not even for "done". The
// client has written the next round by the time it reads that
// acknowledgement; it then sends "done" on its own and waits for no further
// answer.
func TestNegotiatePackStopsAtAcknowledgementWithoutStatus(t *testing.T) {
	t.Parallel()

	haves := make([]plumbing.Hash, 100)
	for i := range haves {
		haves[i] = plumbing.NewHash(fmt.Sprintf("%040x", i+1))
	}
	// Haves are offered from the end of the list; the 20th is in round two.
	common := haves[len(haves)-20]

	reader := &answerScript{}
	_, err := pktline.WriteString(reader, "NAK\n")
	require.NoError(t, err)
	_, err = pktline.WriteString(reader, "ACK "+common.String()+"\n")
	require.NoError(t, err)

	writer := newMockWriteCloser(nil)
	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: haves,
	}

	_, err = NegotiatePack(context.Background(), memory.NewStorage(), capability.List{}, false, reader, writer, req)
	require.NoError(t, err)

	assert.Equal(t, []int{16, 16, 32, 0}, haveRounds(t, writer.writeBuf.Bytes()))
	requireAllAnswersRead(t, reader)
}
