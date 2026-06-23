package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

// advRefs encodes a minimal v0/v1 reference advertisement. When version > 0 a
// "version N" pktline is prepended, mirroring what a server emits when it
// honours a GIT_PROTOCOL request for that version.
func advRefs(t *testing.T, version int) []byte {
	t.Helper()
	head := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	ar := &packp.AdvRefs{
		References: []*plumbing.Reference{
			plumbing.NewHashReference(plumbing.HEAD, head),
			plumbing.NewHashReference("refs/heads/master", head),
		},
	}
	ar.Capabilities.Add(capability.OFSDelta)

	var buf bytes.Buffer
	if version > 0 {
		_, err := pktline.Writef(&buf, "version %d\n", version)
		require.NoError(t, err)
	}
	require.NoError(t, ar.Encode(&buf))
	return buf.Bytes()
}

// nopWriteCloser adapts a Writer to a WriteCloser with a no-op Close, for the
// unused write side of a stream session under test.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// TestNewStreamSessionVersionFallback verifies that the session built by
// NewStreamSession is driven by the version the server actually advertises,
// not by what the client requested. A client defaulting to v2 must fall back
// to a v0/v1 StreamSession when the server replies with a v0/v1 advertisement.
func TestNewStreamSessionVersionFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version int
	}{
		{name: "v0 advertisement", version: 0},
		{name: "v1 advertisement", version: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn := &testConn{
				r:     bytes.NewReader(advRefs(t, tt.version)),
				w:     nopWriteCloser{io.Discard},
				close: func() error { return nil },
			}

			session, err := NewStreamSession(conn, UploadPackService)
			require.NoError(t, err)

			ss, ok := session.(*StreamSession)
			require.True(t, ok, "expected v0/v1 *StreamSession, got %T", session)

			refs, err := ss.GetRemoteRefs(context.Background(), nil)
			require.NoError(t, err)
			assert.Len(t, refs, 2)
		})
	}
}
