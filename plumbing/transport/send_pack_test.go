package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

func TestSendPackWithReportStatus(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)

	reportStatusResponse := strings.Join([]string{
		"000eunpack ok\n",
		"0019ok refs/heads/master\n",
		"0000",
	}, "")
	reader := io.NopCloser(bytes.NewReader([]byte(reportStatusResponse)))
	writer := newMockWriteCloser(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: io.NopCloser(&bytes.Buffer{}),
	}

	err := SendPack(context.TODO(), nil, caps, writer, reader, req)
	assert.NoError(t, err)
	assert.True(t, writer.closed)
}

func TestSendPackWithReportStatusError(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)

	reportStatusResponse := strings.Join([]string{
		"0012unpack failed\n",
		"0000",
	}, "")
	reader := io.NopCloser(bytes.NewReader([]byte(reportStatusResponse)))
	writer := newMockWriteCloser(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: io.NopCloser(&bytes.Buffer{}),
	}

	err := SendPack(context.Background(), nil, caps, writer, reader, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unpack error: failed")
}

func TestSendPackWithoutReportStatus(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()

	reader := io.NopCloser(bytes.NewReader(nil))
	writer := newMockWriteCloser(nil)

	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: io.NopCloser(&bytes.Buffer{}),
	}

	err := SendPack(context.Background(), nil, caps, writer, reader, req)
	assert.NoError(t, err)
	assert.True(t, writer.closed)
}

func TestSendPackWithProgress(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)
	caps.Add(capability.Sideband64k)

	sidebandResponse := strings.Join([]string{
		"0013\x02Progress: 50%\n",
		"0030\x01" +
			"000eunpack ok\n" +
			"0019ok refs/heads/master\n" +
			"0000",
		"0000",
	}, "")
	reader := io.NopCloser(bytes.NewReader([]byte(sidebandResponse)))
	writer := newMockWriteCloser(nil)

	progressBuf := &bytes.Buffer{}
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: io.NopCloser(&bytes.Buffer{}),
		Progress: progressBuf,
	}

	err := SendPack(context.Background(), nil, caps, writer, reader, req)
	assert.NoError(t, err)
	assert.Contains(t, progressBuf.String(), "Progress: 50%")
}

func TestSendPackWithPackfile(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)

	reportStatusResponse := strings.Join([]string{
		"000eunpack ok\n",
		"0019ok refs/heads/master\n",
		"0000",
	}, "")
	reader := io.NopCloser(bytes.NewReader([]byte(reportStatusResponse)))
	writer := newMockWriteCloser(nil)

	packfileContent := []byte("mock packfile content")
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Packfile: io.NopCloser(bytes.NewReader(packfileContent)),
	}

	err := SendPack(context.Background(), nil, caps, writer, reader, req)
	assert.NoError(t, err)
	assert.Contains(t, writer.writeBuf.String(), "mock packfile content")
}

func TestSendPackErrors(t *testing.T) {
	t.Parallel()
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)

	t.Run("EncodeError", func(t *testing.T) {
		t.Parallel()
		writer := newMockWriteCloser(nil)
		writer.writeErr = errors.New("encode error")
		reader := io.NopCloser(bytes.NewReader(nil))

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
			Packfile: io.NopCloser(&bytes.Buffer{}),
		}

		err := SendPack(context.Background(), nil, caps, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "encode error")
	})

	t.Run("PackfileCopyError", func(t *testing.T) {
		t.Parallel()
		writer := newMockWriteCloser(nil)
		reader := io.NopCloser(bytes.NewReader(nil))

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

		err := SendPack(context.Background(), nil, caps, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "packfile read error")
	})

	t.Run("WriterCloseError", func(t *testing.T) {
		t.Parallel()
		writer := newMockWriteCloser(nil)
		writer.closeErr = errors.New("writer close error")
		reader := io.NopCloser(bytes.NewReader(nil))

		req := &PushRequest{
			Commands: []*packp.Command{
				{
					Name: plumbing.ReferenceName("refs/heads/master"),
					Old:  plumbing.ZeroHash,
					New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
				},
			},
			Packfile: io.NopCloser(&bytes.Buffer{}),
		}

		err := SendPack(context.Background(), nil, caps, writer, reader, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "writer close error")
	})
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
