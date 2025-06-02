package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/trace"
	"github.com/stretchr/testify/assert"
)

// mockConnection implements the Connection interface for testing
type mockConnection struct {
	caps *capability.List
}

func (c *mockConnection) Close() error {
	return nil
}

func (c *mockConnection) Capabilities() *capability.List {
	return c.caps
}

func (c *mockConnection) Version() protocol.Version {
	return protocol.V1
}

func (c *mockConnection) StatelessRPC() bool {
	return false
}

func (c *mockConnection) GetRemoteRefs(ctx context.Context) ([]*plumbing.Reference, error) {
	return nil, nil
}

func (c *mockConnection) Fetch(ctx context.Context, req *FetchRequest) error {
	return nil
}

func (c *mockConnection) Push(ctx context.Context, req *PushRequest) error {
	return nil
}

// mockReadWriteCloser implements io.ReadWriteCloser for testing
type mockReadWriteCloser struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
	readErr  error
	writeErr error
	closeErr error
}

func newMockRWC(readData []byte) *mockReadWriteCloser {
	return &mockReadWriteCloser{
		readBuf:  bytes.NewBuffer(readData),
		writeBuf: &bytes.Buffer{},
	}
}

func (rw *mockReadWriteCloser) Read(p []byte) (int, error) {
	if rw.readErr != nil {
		return 0, rw.readErr
	}
	return rw.readBuf.Read(p)
}

func (rw *mockReadWriteCloser) Write(p []byte) (int, error) {
	if rw.writeErr != nil {
		return 0, rw.writeErr
	}
	return rw.writeBuf.Write(p)
}

func (rw *mockReadWriteCloser) Close() error {
	rw.closed = true
	return rw.closeErr
}

// TestSendPackWithReportStatus tests the SendPack function with ReportStatus capability
func TestSendPackWithReportStatus(t *testing.T) {
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	conn := &mockConnection{caps: caps}

	// Create a mock reader with a valid report status response
	reportStatusResponse := strings.Join([]string{
		"000eunpack ok\n",            // "unpack ok\n"
		"0019ok refs/heads/master\n", // "ok refs/heads/master\n"
		"0000",                       // flush-pkt
	}, "")
	reader := newMockRWC([]byte(reportStatusResponse))
	writer := newMockRWC(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	storer := memory.NewStorage()
	err := SendPack(context.TODO(), storer, conn, writer, reader, req)

	assert.NoError(t, err)

	// Verify the reader and writer were closed
	assert.True(t, reader.closed)
	assert.True(t, writer.closed)
}

// TestSendPackWithReportStatusError tests the SendPack function with an error in the report status
func TestSendPackWithReportStatusError(t *testing.T) {
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	conn := &mockConnection{caps: caps}

	// Create a mock reader with an error report status response
	reportStatusResponse := strings.Join([]string{
		"0012unpack failed\n", // "unpack failed\n"
		"0000",                // flush-pkt
	}, "")
	reader := newMockRWC([]byte(reportStatusResponse))
	writer := newMockRWC(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	// Call SendPack
	storer := memory.NewStorage()
	err := SendPack(context.Background(), storer, conn, writer, reader, req)

	// Verify an error was returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unpack error: failed")

	// Verify the reader and writer were closed
	assert.True(t, reader.closed)
	assert.True(t, writer.closed)
}

// TestSendPackWithoutReportStatus tests the SendPack function without ReportStatus capability
func TestSendPackWithoutReportStatus(t *testing.T) {
	// Create a mock connection without ReportStatus capability
	caps := capability.NewList()
	conn := &mockConnection{caps: caps}

	reader := newMockRWC(nil)
	writer := newMockRWC(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	storer := memory.NewStorage()
	err := SendPack(context.Background(), storer, conn, writer, reader, req)

	assert.NoError(t, err)

	// Verify the writer was closed but not the reader (since we don't read without ReportStatus)
	assert.False(t, reader.closed)
	assert.True(t, writer.closed)
}

func init() {
	trace.SetTarget(trace.General | trace.Packet)
}

func TestSendPackWithProgress(t *testing.T) {
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	caps.Add(capability.Sideband64k)
	conn := &mockConnection{caps: caps}

	// Create a mock reader with a sideband-encoded report status response
	// This simulates a response with progress messages and a report status
	sidebandResponse := strings.Join([]string{
		// Sideband progress message (channel 2) fake progress
		"0013\x02Progress: 50%\n", // "Progress: 50%\n"
		// Sideband pack data message (channel 1) with report-status
		"0030\x01" +
			"000eunpack ok\n" + // "unpack ok\n"
			"0019ok refs/heads/master\n" + // "ok refs/heads/master\n"
			"0000", // flush-pkt
		// Flush-pkt to terminate the sideband message.
		"0000", // flush-pkt
	}, "")
	reader := newMockRWC([]byte(sidebandResponse))
	writer := newMockRWC(nil)

	// Create a progress buffer to capture progress messages
	progressBuf := &bytes.Buffer{}

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Progress: progressBuf,
	}

	storer := memory.NewStorage()
	err := SendPack(context.Background(), storer, conn, writer, reader, req)

	assert.NoError(t, err)

	// Verify progress was captured
	assert.Contains(t, progressBuf.String(), "Progress: 50%")
}

// TestSendPackWithPackfile tests the SendPack function with a packfile
func TestSendPackWithPackfile(t *testing.T) {
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	conn := &mockConnection{caps: caps}

	// Create a mock reader with a valid report status response
	reportStatusResponse := strings.Join([]string{
		"000eunpack ok\n",            // "unpack ok\n"
		"0019ok refs/heads/master\n", // "ok refs/heads/master\n"
		"0000",                       // flush-pkt
	}, "")
	reader := newMockRWC([]byte(reportStatusResponse))
	writer := newMockRWC(nil)

	// Create a packfile
	packfileContent := []byte("mock packfile content")
	packfile := io.NopCloser(bytes.NewReader(packfileContent))

	// Create a push request with a packfile
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: packfile,
	}

	storer := memory.NewStorage()
	err := SendPack(context.Background(), storer, conn, writer, reader, req)

	assert.NoError(t, err)

	// Verify the packfile was written
	assert.Contains(t, writer.writeBuf.String(), "mock packfile content")
}

// TestSendPackErrors tests various error conditions in SendPack
func TestSendPackErrors(t *testing.T) {
	// Create a mock connection with ReportStatus capability
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	conn := &mockConnection{caps: caps}

	// Test case: error encoding update requests
	t.Run("EncodeError", func(t *testing.T) {
		writer := newMockRWC(nil)
		writer.writeErr = errors.New("encode error")
		reader := newMockRWC(nil)

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
		}

		storer := memory.NewStorage()
		err := SendPack(context.Background(), storer, conn, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "encode error")
	})

	// Test case: error copying packfile
	t.Run("PackfileCopyError", func(t *testing.T) {
		writer := newMockRWC(nil)
		reader := newMockRWC(nil)

		// Create a packfile that returns an error on read
		errPackfile := &errorReader{err: errors.New("packfile read error")}

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
			Packfile: io.NopCloser(errPackfile),
		}

		storer := memory.NewStorage()
		err := SendPack(context.Background(), storer, conn, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "packfile read error")
	})

	// Test case: error closing writer
	t.Run("WriterCloseError", func(t *testing.T) {
		writer := newMockRWC(nil)
		writer.closeErr = errors.New("writer close error")
		reader := newMockRWC(nil)

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
		}

		storer := memory.NewStorage()
		err := SendPack(context.Background(), storer, conn, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "writer close error")
	})

	// Test case: error decoding report status
	t.Run("ReportStatusDecodeError", func(t *testing.T) {
		// Create invalid report status response (missing flush)
		invalidResponse := strings.Join([]string{
			"000eunpack ok\n", // "unpack ok\n"
		}, "")
		reader := newMockRWC([]byte(invalidResponse))
		writer := newMockRWC(nil)

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
		}

		storer := memory.NewStorage()
		err := SendPack(context.Background(), storer, conn, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode report-status")
	})

	// Test case: error closing reader
	t.Run("ReaderCloseError", func(t *testing.T) {
		// Create valid report status response
		validResponse := strings.Join([]string{
			"000eunpack ok\n", // "unpack ok\n"
			"0000",            // flush-pkt
		}, "")
		reader := newMockRWC([]byte(validResponse))
		reader.closeErr = errors.New("reader close error")
		writer := newMockRWC(nil)

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
		}

		storer := memory.NewStorage()
		err := SendPack(context.Background(), storer, conn, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "closing reader: reader close error")
	})
}

// errorReader is a simple io.Reader that always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, r.err
}
