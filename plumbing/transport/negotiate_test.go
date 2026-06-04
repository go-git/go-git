package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestNegotiatePackCloseBehavior(t *testing.T) {
	t.Parallel()

	hashA := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hashB := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")

	tests := []struct {
		name            string
		reader          []byte
		closeErr        error
		req             *FetchRequest
		wantErrIs       error
		wantErrText     string
		wantFlush       string
		wantNoErr       bool
		wantShallowsNil bool
	}{
		{
			name:      "no_change_eof_on_close",
			closeErr:  io.EOF,
			req:       &FetchRequest{Wants: []plumbing.Hash{hashA}, Haves: []plumbing.Hash{hashA}},
			wantErrIs: ErrNoChange,
			wantFlush: "0000",
		},
		{
			name:        "no_change_non_eof_on_close",
			closeErr:    io.ErrUnexpectedEOF,
			req:         &FetchRequest{Wants: []plumbing.Hash{hashA}, Haves: []plumbing.Hash{hashA}},
			wantErrText: "closing writer",
			wantFlush:   "0000",
		},
		{
			name:            "complete_eof_on_close",
			reader:          []byte("0008NAK\n"),
			closeErr:        io.EOF,
			req:             &FetchRequest{Wants: []plumbing.Hash{hashA}, Haves: []plumbing.Hash{hashB}},
			wantNoErr:       true,
			wantShallowsNil: true,
		},
		{
			name:            "complete_non_eof_on_close",
			reader:          []byte("0008NAK\n"),
			closeErr:        io.ErrUnexpectedEOF,
			req:             &FetchRequest{Wants: []plumbing.Hash{hashA}, Haves: []plumbing.Hash{hashB}},
			wantErrText:     "closing writer",
			wantShallowsNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			writer := newMockWriteCloser(nil)
			writer.closeErr = tt.closeErr

			shallows, err := NegotiatePack(
				context.TODO(),
				memory.NewStorage(),
				capability.List{},
				false,
				bytes.NewReader(tt.reader),
				writer,
				tt.req,
			)

			switch {
			case tt.wantNoErr:
				require.NoError(t, err)
			case tt.wantErrIs != nil:
				require.ErrorIs(t, err, tt.wantErrIs)
			default:
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrText)
				assert.ErrorIs(t, err, tt.closeErr)
			}

			if tt.wantFlush != "" {
				assert.Equal(t, tt.wantFlush, writer.writeBuf.String())
			}
			if tt.wantShallowsNil {
				assert.Nil(t, shallows)
			}
		})
	}
}

func TestNextFlush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		statelessRPC bool
		in           int
		want         int
	}{
		{name: "stateless_initial", statelessRPC: true, in: 16, want: 32},
		{name: "stateless_growth", statelessRPC: true, in: 32, want: 64},
		{name: "stateless_large_boundary", statelessRPC: true, in: 16384, want: 18022},
		{name: "stateless_large_growth", statelessRPC: true, in: 32768, want: 36044},
		{name: "stateful_initial", statelessRPC: false, in: 16, want: 32},
		{name: "stateful_pipe_safe_boundary", statelessRPC: false, in: 32, want: 64},
		{name: "stateful_linear_growth_64", statelessRPC: false, in: 64, want: 96},
		{name: "stateful_linear_growth_96", statelessRPC: false, in: 96, want: 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, nextFlush(tt.statelessRPC, tt.in))
		})
	}
}

func TestNegotiatePackMultiRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		statelessRPC bool
		wantCount    int
		haveCount    int
		flushCount   int
	}{
		{
			name:         "stateful",
			statelessRPC: false,
			wantCount:    1,
			haveCount:    17,
			flushCount:   2,
		},
		{
			name:         "stateless",
			statelessRPC: true,
			wantCount:    2,
			haveCount:    17,
			flushCount:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			writer := negotiatePackMultiRound(t, tt.statelessRPC)
			out := writer.writeBuf.String()

			assert.Equal(t, tt.wantCount, strings.Count(out, "want "))
			assert.Equal(t, tt.haveCount, strings.Count(out, "have "))
			assert.Equal(t, tt.flushCount, strings.Count(out, "0000"))
		})
	}
}

func TestNegotiatePackStatelessRequeuesACKCommon(t *testing.T) {
	t.Parallel()

	commonHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	reader := bytes.NewBuffer(nil)
	_, err := pktline.WriteString(reader, "ACK "+commonHash.String()+" common\n")
	require.NoError(t, err)
	_, err = pktline.WriteString(reader, "NAK\n")
	require.NoError(t, err)
	_, err = pktline.WriteString(reader, "NAK\n")
	require.NoError(t, err)

	writer := newMockWriteCloser(nil)
	haves := append([]plumbing.Hash{plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, makeSyntheticHaves(15)...)
	haves = append(haves, commonHash)
	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: haves,
	}

	_, err = NegotiatePack(context.TODO(), memory.NewStorage(), capability.List{}, true, reader, writer, req)
	require.NoError(t, err)

	assert.Equal(t, 2, strings.Count(writer.writeBuf.String(), "have "+commonHash.String()), "ACK_common haves should be re-sent on subsequent stateless rounds")
}

func TestApplyServerACKs(t *testing.T) {
	t.Parallel()

	commonHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	tests := []struct {
		name                   string
		statelessRPC           bool
		acks                   []packp.ACK
		initialCommon          map[plumbing.Hash]struct{}
		initialStatelessCommon []plumbing.Hash
		initialGotContinue     bool
		initialGotReady        bool
		initialInVein          int
		wantGotContinue        bool
		wantGotReady           bool
		wantInVein             int
		wantStatelessCommon    []plumbing.Hash
		wantCommonContains     []plumbing.Hash
	}{
		{
			name:                "ack_continue_resets_in_vein",
			acks:                []packp.ACK{{Status: packp.ACKContinue}},
			initialCommon:       map[plumbing.Hash]struct{}{},
			initialInVein:       123,
			wantGotContinue:     true,
			wantGotReady:        false,
			wantInVein:          0,
			wantStatelessCommon: nil,
		},
		{
			name:                "stateless_ack_common_requeues_and_resets",
			statelessRPC:        true,
			acks:                []packp.ACK{{Hash: commonHash, Status: packp.ACKCommon}},
			initialCommon:       map[plumbing.Hash]struct{}{},
			initialInVein:       77,
			wantGotContinue:     true,
			wantGotReady:        false,
			wantInVein:          0,
			wantStatelessCommon: []plumbing.Hash{commonHash},
			wantCommonContains:  []plumbing.Hash{commonHash},
		},
		{
			name:                   "duplicate_stateless_ack_common_does_not_duplicate",
			statelessRPC:           true,
			acks:                   []packp.ACK{{Hash: commonHash, Status: packp.ACKCommon}, {Hash: commonHash, Status: packp.ACKCommon}},
			initialCommon:          map[plumbing.Hash]struct{}{commonHash: {}},
			initialStatelessCommon: []plumbing.Hash{commonHash},
			initialGotContinue:     true,
			initialInVein:          55,
			wantGotContinue:        true,
			wantGotReady:           false,
			wantInVein:             55,
			wantStatelessCommon:    []plumbing.Hash{commonHash},
			wantCommonContains:     []plumbing.Hash{commonHash},
		},
		{
			name:                "ack_ready_sets_ready_and_resets",
			acks:                []packp.ACK{{Status: packp.ACKReady}},
			initialCommon:       map[plumbing.Hash]struct{}{},
			initialInVein:       42,
			wantGotContinue:     true,
			wantGotReady:        true,
			wantInVein:          0,
			wantStatelessCommon: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			common := mapsClone(tt.initialCommon)
			statelessCommon := append([]plumbing.Hash(nil), tt.initialStatelessCommon...)
			gotContinue := tt.initialGotContinue
			gotReady := tt.initialGotReady
			inVein := tt.initialInVein

			applyServerACKs(tt.statelessRPC, tt.acks, common, &statelessCommon, &gotContinue, &gotReady, &inVein)

			assert.Equal(t, tt.wantGotContinue, gotContinue)
			assert.Equal(t, tt.wantGotReady, gotReady)
			assert.Equal(t, tt.wantInVein, inVein)
			assert.Equal(t, tt.wantStatelessCommon, statelessCommon)
			for _, h := range tt.wantCommonContains {
				assert.Contains(t, common, h)
			}
		})
	}
}

func TestNegotiatePackStatelessClampsAfterGotContinue(t *testing.T) {
	t.Parallel()

	reader := bytes.NewBuffer(nil)
	continueHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	var err error
	_, err = pktline.WriteString(reader, "ACK "+continueHash.String()+" continue\n")
	require.NoError(t, err)
	for range 4 {
		_, err = pktline.WriteString(reader, "NAK\n")
		require.NoError(t, err)
	}

	writer := newMockWriteCloser(nil)
	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: makeSyntheticHaves(600),
	}

	_, err = NegotiatePack(context.TODO(), memory.NewStorage(), capability.List{}, true, reader, writer, req)
	require.NoError(t, err)

	assert.LessOrEqual(t, strings.Count(writer.writeBuf.String(), "have "), initialFlush+maxInVein, "post-continue stateless negotiation should clamp new haves to the remaining in-vein budget")
}

func TestNegotiatePackStopsImmediatelyOnACKReady(t *testing.T) {
	t.Parallel()

	readyHash := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	reader := bytes.NewBuffer(nil)
	_, err := pktline.WriteString(reader, "ACK "+readyHash.String()+" ready\n")
	require.NoError(t, err)

	writer := newMockWriteCloser(nil)
	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: makeSyntheticHaves(40),
	}

	_, err = NegotiatePack(context.TODO(), memory.NewStorage(), capability.List{}, true, reader, writer, req)
	require.NoError(t, err)

	out := writer.writeBuf.String()
	assert.Equal(t, 2, strings.Count(out, "want "), "ACK_ready should send only the terminal done request after the first round")
	assert.Equal(t, initialFlush, strings.Count(out, "have "), "ACK_ready should stop after the first batch")
}

func negotiatePackMultiRound(t *testing.T, statelessRPC bool) *mockWriteCloser {
	t.Helper()

	caps := capability.List{}
	reader := bytes.NewReader([]byte("0008NAK\n0008NAK\n"))
	writer := newMockWriteCloser(nil)

	req := &FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves: makeSyntheticHaves(17),
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, caps, statelessRPC, reader, writer, req)
	require.NoError(t, err)
	return writer
}

func makeSyntheticHaves(n int, keep ...plumbing.Hash) []plumbing.Hash {
	haves := make([]plumbing.Hash, 0, n)
	haves = append(haves, keep...)
	for i := len(haves); i < n; i++ {
		hash := fmt.Sprintf("%040x", i+1)
		haves = append(haves, plumbing.NewHash(strings.ReplaceAll(hash, "0", "a")))
	}
	return haves
}

func mapsClone(in map[plumbing.Hash]struct{}) map[plumbing.Hash]struct{} {
	out := make(map[plumbing.Hash]struct{}, len(in))
	maps.Copy(out, in)
	return out
}

// mockWriteCloser implements io.WriteCloser for testing.
type mockWriteCloser struct {
	writeBuf *bytes.Buffer
	writeErr error
	closeErr error
	closed   bool
}

func newMockWriteCloser(_ []byte) *mockWriteCloser {
	return &mockWriteCloser{
		writeBuf: &bytes.Buffer{},
	}
}

func (rw *mockWriteCloser) Write(p []byte) (int, error) {
	if rw.writeErr != nil {
		return 0, rw.writeErr
	}
	return rw.writeBuf.Write(p)
}

func (rw *mockWriteCloser) Close() error {
	rw.closed = true
	return rw.closeErr
}
