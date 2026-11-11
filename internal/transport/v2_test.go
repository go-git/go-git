package transport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

func TestClientCapabilitiesObjectFormat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		server []string // nil = not advertised
		want   []string // nil = not sent
	}{
		{name: "not advertised"},
		{name: "sha1", server: []string{"sha1"}, want: []string{"sha1"}},
		{name: "sha256", server: []string{"sha256"}, want: []string{"sha256"}},
		// A format the client cannot name is not echoed back.
		{name: "empty", server: []string{""}},
		{name: "list", server: []string{"sha1 sha256"}},
		{name: "unknown", server: []string{"bogus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var server capability.List
			if tc.server != nil {
				server.Add(capability.ObjectFormat, tc.server...)
			}
			caps := ClientCapabilities(server)
			assert.Equal(t, tc.want, caps.Get(capability.ObjectFormat))
		})
	}
}

// ls-refs refuses a server whose object-format go-git does not know before
// sending the command, as git's send_capabilities does.
func TestLsRefsObjectFormat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		server  []string // nil = not advertised
		present bool     // advertised without a value
		wantErr string
	}{
		{name: "not advertised"},
		{name: "bare", present: true},
		{name: "sha1", server: []string{"sha1"}},
		{name: "sha256", server: []string{"sha256"}},
		{name: "empty", server: []string{""}, wantErr: `unsupported object-format ""`},
		{name: "list", server: []string{"sha1 sha256"}, wantErr: `unsupported object-format "sha1 sha256"`},
		{name: "unknown", server: []string{"bogus"}, wantErr: `unsupported object-format "bogus"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var server capability.List
			if tc.server != nil || tc.present {
				server.Add(capability.ObjectFormat, tc.server...)
			}
			var sent bool
			cmd := func(context.Context, string, packp.CommandArgs, packp.Decoder) error {
				sent = true
				return nil
			}
			_, err := LsRefs(context.Background(), cmd, server, nil)
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.True(t, sent)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			assert.False(t, sent, "ls-refs sent to the server")
		})
	}
}

func TestCommandSupports(t *testing.T) {
	t.Parallel()

	var server capability.List
	server.Add(capability.FetchCmd, "shallow wait-for-done filter")
	server.Add(capability.LsRefs, "peel unborn")

	assert.True(t, FetchSupports(server, "filter"))
	assert.True(t, FetchSupports(server, "shallow"))
	assert.False(t, FetchSupports(server, "unborn"))
	assert.True(t, lsRefsSupportsUnborn(server))

	var bare capability.List
	bare.Add(capability.LsRefs)
	assert.False(t, lsRefsSupportsUnborn(bare))
}
