package transport

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

func TestBuildUpdateRequestsWithReportStatus(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Add(capability.ReportStatus)

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
	}

	upreq := buildUpdateRequests(caps, req)
	assert.True(t, upreq.Capabilities.Supports(capability.ReportStatus))
	require.Len(t, upreq.Commands, 1)
	assert.Equal(t, plumbing.ReferenceName("refs/heads/master"), upreq.Commands[0].Name)
}

func TestBuildUpdateRequestsWithoutReportStatus(t *testing.T) {
	t.Parallel()
	caps := capability.List{}

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
	}

	upreq := buildUpdateRequests(caps, req)
	assert.False(t, upreq.Capabilities.Supports(capability.ReportStatus))
}

func TestBuildUpdateRequestsWithProgress(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Add(capability.Sideband64k)

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
		Progress: &mockWriter{},
	}

	upreq := buildUpdateRequests(caps, req)
	assert.True(t, upreq.Capabilities.Supports(capability.Sideband64k))
	assert.False(t, upreq.Capabilities.Supports(capability.Sideband))
}

func TestBuildUpdateRequestsWithProgressFallback(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Add(capability.Sideband)

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
		Progress: &mockWriter{},
	}

	upreq := buildUpdateRequests(caps, req)
	assert.False(t, upreq.Capabilities.Supports(capability.Sideband64k))
	assert.True(t, upreq.Capabilities.Supports(capability.Sideband))
}

func TestBuildUpdateRequestsWithQuiet(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Add(capability.Quiet)

	req := &PushRequest{
		Progress: io.Discard,
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
		Quiet: true,
	}

	upreq := buildUpdateRequests(caps, req)
	assert.True(t, upreq.Capabilities.Supports(capability.Quiet))
}

func TestBuildUpdateRequestsWithAtomic(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Add(capability.Atomic)

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
		Atomic: true,
	}

	upreq := buildUpdateRequests(caps, req)
	assert.True(t, upreq.Capabilities.Supports(capability.Atomic))
}

func TestBuildUpdateRequestsWithAtomicNotSupported(t *testing.T) {
	t.Parallel()
	caps := capability.List{}

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
		Atomic: true,
	}

	upreq := buildUpdateRequests(caps, req)
	assert.False(t, upreq.Capabilities.Supports(capability.Atomic))
}

func TestBuildUpdateRequestsWithAgent(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Set(capability.Agent, capability.DefaultAgent())

	req := &PushRequest{
		Commands: []*packp.Command{
			{Name: "refs/heads/master", Old: plumbing.ZeroHash, New: plumbing.NewHash("0123456789012345678901234567890123456789")},
		},
	}

	upreq := buildUpdateRequests(caps, req)
	assert.True(t, upreq.Capabilities.Supports(capability.Agent))
}

type mockWriter struct {
	data []byte
}

func (w *mockWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}
