package transport

import (
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUpdateRequestsWithReportStatus(t *testing.T) {
	// Create capabilities with ReportStatus
	caps := capability.NewList()
	caps.Add(capability.ReportStatus)

	// Create a push request
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify ReportStatus capability is set
	assert.True(t, upreq.Capabilities.Supports(capability.ReportStatus))

	// Verify commands are properly set
	require.Len(t, upreq.Commands, 1)
	assert.Equal(t, plumbing.ReferenceName("refs/heads/master"), upreq.Commands[0].Name)
	assert.Equal(t, plumbing.ZeroHash, upreq.Commands[0].Old)
	assert.Equal(t, plumbing.NewHash("0123456789012345678901234567890123456789"), upreq.Commands[0].New)
}

func TestBuildUpdateRequestsWithoutReportStatus(t *testing.T) {
	// Create capabilities without ReportStatus
	caps := capability.NewList()

	// Create a push request
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify ReportStatus capability is not set
	assert.False(t, upreq.Capabilities.Supports(capability.ReportStatus))
}

func TestBuildUpdateRequestsWithProgress(t *testing.T) {
	// Create capabilities with Sideband64k
	caps := capability.NewList()
	caps.Add(capability.Sideband64k)

	// Create a push request with progress
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Progress: &mockWriter{},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify Sideband64k capability is set
	assert.True(t, upreq.Capabilities.Supports(capability.Sideband64k))
	assert.False(t, upreq.Capabilities.Supports(capability.Sideband))
	assert.False(t, upreq.Capabilities.Supports(capability.NoProgress))
}

func TestBuildUpdateRequestsWithProgressFallback(t *testing.T) {
	// Create capabilities with Sideband but not Sideband64k
	caps := capability.NewList()
	caps.Add(capability.Sideband)

	// Create a push request with progress
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Progress: &mockWriter{},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify Sideband capability is set but not Sideband64k
	assert.False(t, upreq.Capabilities.Supports(capability.Sideband64k))
	assert.True(t, upreq.Capabilities.Supports(capability.Sideband))
	assert.False(t, upreq.Capabilities.Supports(capability.NoProgress))
}

func TestBuildUpdateRequestsWithNoProgress(t *testing.T) {
	// Create capabilities with NoProgress
	caps := capability.NewList()
	caps.Add(capability.NoProgress)

	// Create a push request without progress
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify NoProgress capability is set
	assert.True(t, upreq.Capabilities.Supports(capability.NoProgress))
}

func TestBuildUpdateRequestsWithAtomic(t *testing.T) {
	// Create capabilities with Atomic
	caps := capability.NewList()
	caps.Add(capability.Atomic)

	// Create a push request with Atomic
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Atomic: true,
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify Atomic capability is set
	assert.True(t, upreq.Capabilities.Supports(capability.Atomic))
}

func TestBuildUpdateRequestsWithAtomicNotSupported(t *testing.T) {
	// Create capabilities without Atomic
	caps := capability.NewList()

	// Create a push request with Atomic
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
		Atomic: true,
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify Atomic capability is not set
	assert.False(t, upreq.Capabilities.Supports(capability.Atomic))
}

func TestBuildUpdateRequestsWithAgent(t *testing.T) {
	// Create capabilities with Agent
	caps := capability.NewList()
	caps.Set(capability.Agent, capability.DefaultAgent())

	// Create a push request
	req := &PushRequest{
		Commands: []*packp.Command{
			{
				Name: plumbing.ReferenceName("refs/heads/master"),
				Old:  plumbing.ZeroHash,
				New:  plumbing.NewHash("0123456789012345678901234567890123456789"),
			},
		},
	}

	// Build update requests
	upreq := buildUpdateRequests(caps, req)

	// Verify Agent capability is set
	assert.True(t, upreq.Capabilities.Supports(capability.Agent))

	// Verify agent value is set to default agent
	val := upreq.Capabilities.Get(capability.Agent)
	assert.Equal(t, []string{capability.DefaultAgent()}, val)
}

// mockWriter is a simple io.Writer implementation for testing
type mockWriter struct {
	data []byte
}

func (w *mockWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}
