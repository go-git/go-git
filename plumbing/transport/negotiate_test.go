package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestNegotiatePackNoChangeWithEOFOnClose(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	reader := bytes.NewReader(nil)
	writer := newMockWriteCloser(nil)
	writer.closeErr = io.EOF

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hash},
		Haves: []plumbing.Hash{hash},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, caps, false, reader, writer, req)
	require.ErrorIs(t, err, ErrNoChange)
	assert.Equal(t, "0000", writer.writeBuf.String())
}

func TestNegotiatePackNoChangeWithNonEOFCloseError(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	reader := bytes.NewReader(nil)
	writer := newMockWriteCloser(nil)
	writer.closeErr = io.ErrUnexpectedEOF

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hash},
		Haves: []plumbing.Hash{hash},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, caps, false, reader, writer, req)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoChange)
	assert.Contains(t, err.Error(), "closing writer")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestNegotiatePackCompleteWithEOFOnClose(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	reader := bytes.NewReader([]byte("0008NAK\n"))
	writer := newMockWriteCloser(nil)
	writer.closeErr = io.EOF

	hashA := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hashB := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hashA},
		Haves: []plumbing.Hash{hashB},
	}

	storer := memory.NewStorage()
	shallows, err := NegotiatePack(context.TODO(), storer, caps, false, reader, writer, req)
	require.NoError(t, err)
	assert.Nil(t, shallows)
}

func TestNegotiatePackCompleteWithNonEOFCloseError(t *testing.T) {
	t.Parallel()

	caps := capability.List{}
	reader := bytes.NewReader([]byte("0008NAK\n"))
	writer := newMockWriteCloser(nil)
	writer.closeErr = io.ErrUnexpectedEOF

	hashA := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	hashB := plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")
	req := &FetchRequest{
		Wants: []plumbing.Hash{hashA},
		Haves: []plumbing.Hash{hashB},
	}

	storer := memory.NewStorage()
	_, err := NegotiatePack(context.TODO(), storer, caps, false, reader, writer, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closing writer")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
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
