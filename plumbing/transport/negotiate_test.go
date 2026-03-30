package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage/memory"
)

// TestNegotiatePackNoChangeWithEOFOnClose tests that NegotiatePack returns
// ErrNoChange when wants == haves and the writer's Close returns io.EOF.
//
// This simulates the scenario where the server (e.g. git-upload-pack) receives
// a flush-pkt with no want lines and exits cleanly before the client closes
// the writer, causing Close to return io.EOF.
//
// See: https://github.com/go-git/go-git/issues/1854
func TestNegotiatePackNoChangeWithEOFOnClose(t *testing.T) {
	t.Parallel()

	caps := capability.NewList()
	conn := &mockConnection{caps: caps}

	reader := bytes.NewReader(nil)
	writer := newMockRWC(nil)
	writer.closeErr = io.EOF // Simulate server already closed the connection

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hash},
		Haves: []plumbing.Hash{hash},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, conn, reader, writer, req)

	require.ErrorIs(t, err, ErrNoChange)

	// Verify a flush-pkt was written before closing
	assert.Equal(t, "0000", writer.writeBuf.String())
}

// TestNegotiatePackNoChangeWithNonEOFCloseError tests that NegotiatePack
// propagates non-EOF errors from the writer's Close.
func TestNegotiatePackNoChangeWithNonEOFCloseError(t *testing.T) {
	t.Parallel()

	caps := capability.NewList()
	conn := &mockConnection{caps: caps}

	reader := bytes.NewReader(nil)
	writer := newMockRWC(nil)
	writer.closeErr = io.ErrUnexpectedEOF // Non-EOF error should be propagated

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hash},
		Haves: []plumbing.Hash{hash},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, conn, reader, writer, req)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoChange)
	assert.Contains(t, err.Error(), "closing writer")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// TestNegotiatePackCompleteWithEOFOnClose tests that NegotiatePack succeeds
// when the writer's Close returns io.EOF after a normal negotiation completes.
//
// This covers L273, the writer close after the negotiation loop finishes with
// done=true. The server may close the connection after sending the final NAK,
// causing Close to return io.EOF.
func TestNegotiatePackCompleteWithEOFOnClose(t *testing.T) {
	t.Parallel()

	caps := capability.NewList()
	conn := &mockConnection{caps: caps}

	// Server responds with NAK (no common objects found)
	reader := bytes.NewReader([]byte("0008NAK\n"))
	writer := newMockRWC(nil)
	writer.closeErr = io.EOF // Simulate server closed the connection

	hashA := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hashB := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hashA},
		Haves: []plumbing.Hash{hashB},
	}

	storer := memory.NewStorage()
	shallows, err := NegotiatePack(context.TODO(), storer, conn, reader, writer, req)

	require.NoError(t, err)
	assert.Nil(t, shallows)
}

// TestNegotiatePackCompleteWithNonEOFCloseError tests that NegotiatePack
// propagates non-EOF errors from the writer's Close after a normal negotiation.
func TestNegotiatePackCompleteWithNonEOFCloseError(t *testing.T) {
	t.Parallel()

	caps := capability.NewList()
	conn := &mockConnection{caps: caps}

	// Server responds with NAK (no common objects found)
	reader := bytes.NewReader([]byte("0008NAK\n"))
	writer := newMockRWC(nil)
	writer.closeErr = io.ErrUnexpectedEOF

	hashA := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hashB := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hashA},
		Haves: []plumbing.Hash{hashB},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, conn, reader, writer, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "closing writer")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
