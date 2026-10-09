package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func formatStorage(t *testing.T, f config.ObjectFormat) *memory.Storage {
	t.Helper()
	return memory.NewStorage(memory.WithObjectFormat(f))
}

// objectFormatPushBody encodes one command with ReportStatus and, when
// client is not "", object-format=<client>. pack follows the commands.
func objectFormatPushBody(t *testing.T, cmd *packp.Command, client string, pack []byte) io.ReadCloser {
	t.Helper()
	caps := capability.List{}
	caps.Add(capability.ReportStatus)
	if client != "" {
		caps.Set(capability.ObjectFormat, client)
	}
	req := &packp.UpdateRequests{Capabilities: caps, Commands: []*packp.Command{cmd}}
	var buf bytes.Buffer
	require.NoError(t, req.Encode(&buf))
	buf.Write(pack)
	return io.NopCloser(&buf)
}

func TestReceivePackAdvertisesObjectFormat(t *testing.T) {
	t.Parallel()
	for _, f := range []config.ObjectFormat{config.SHA1, config.SHA256} {
		t.Run(f.String(), func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, AdvertiseRefs(context.Background(), formatStorage(t, f),
				ioutil.WriteNopCloser(&buf), ReceivePackService, false, protocol.V0))
			assert.Contains(t, buf.String(), "object-format="+f.String())
		})
	}
}

func TestReceivePackObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repo    config.ObjectFormat
		client  string // "" = not sent
		wantErr string // "" = accepted
	}{
		{repo: config.SHA1, client: ""},
		{repo: config.SHA1, client: "sha1"},
		{repo: config.SHA1, client: "sha256", wantErr: "unsupported object format 'sha256'"},
		{repo: config.SHA256, client: "", wantErr: "unsupported object format 'sha1'"},
		{repo: config.SHA256, client: "sha1", wantErr: "unsupported object format 'sha1'"},
		{repo: config.SHA256, client: "sha256"},
	}

	for _, tc := range tests {
		t.Run(tc.repo.String()+"/"+tc.client, func(t *testing.T) {
			t.Parallel()
			st := formatStorage(t, tc.repo)
			ref := plumbing.ReferenceName("refs/heads/main")
			var out bytes.Buffer

			if tc.wantErr == "" {
				// Accepted: a delete needs no pack and must succeed.
				old := formatHash(tc.repo, "1")
				require.NoError(t, st.SetReference(plumbing.NewHashReference(ref, old)))
				err := ReceivePack(context.Background(), st,
					objectFormatPushBody(t, &packp.Command{Name: ref, Old: old, New: plumbing.ZeroHash}, tc.client, nil),
					ioutil.WriteNopCloser(&out), &ReceivePackRequest{StatelessRPC: true})
				require.NoError(t, err)
				assert.Contains(t, out.String(), "ok refs/heads/main")
				return
			}

			// Rejected: a create with bytes that are not a pack. Reading them
			// would fail as a pack error, so the error proves the check ran
			// first and nothing was stored.
			clientFormat := config.ObjectFormat(tc.client)
			if clientFormat == config.UnsetObjectFormat {
				clientFormat = config.SHA1
			}
			cmd := &packp.Command{Name: ref, Old: plumbing.ZeroHash, New: formatHash(clientFormat, "2")}
			err := ReceivePack(context.Background(), st,
				objectFormatPushBody(t, cmd, tc.client, []byte("not a pack")),
				ioutil.WriteNopCloser(&out), &ReceivePackRequest{StatelessRPC: true})
			require.ErrorContains(t, err, tc.wantErr)
			var el *pktline.ErrorLine
			assert.True(t, errors.As(err, &el))
			assert.Contains(t, out.String(), "ERR "+tc.wantErr)
			assert.Empty(t, st.Objects)
			_, refErr := st.Reference(ref)
			assert.ErrorIs(t, refErr, plumbing.ErrReferenceNotFound)
		})
	}
}

// The rejection is framed as the client will read it: on the error side-band,
// then a flush, when the client asked for side-band-64k or side-band, and as
// an ERR packet otherwise.
func TestReceivePackObjectFormatRejectionFraming(t *testing.T) {
	t.Parallel()
	const text = "unsupported object format 'sha1'"

	errPkt := func(t *testing.T) string {
		var b bytes.Buffer
		_, err := pktline.Writef(&b, "ERR %s\n", text)
		require.NoError(t, err)
		return b.String()
	}
	bandPkt := func(t *testing.T) string {
		var b bytes.Buffer
		_, err := pktline.Write(&b, append([]byte{byte(sideband.ErrorMessage)}, text...))
		require.NoError(t, err)
		require.NoError(t, pktline.WriteFlush(&b))
		return b.String()
	}

	tests := []struct {
		name string
		caps []string
		want func(*testing.T) string
	}{
		{name: "no side-band", want: errPkt},
		{name: "side-band-64k", caps: []string{capability.Sideband64k}, want: bandPkt},
		{name: "side-band", caps: []string{capability.Sideband}, want: bandPkt},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps := capability.List{}
			caps.Add(capability.ReportStatus)
			for _, c := range tc.caps {
				caps.Add(c)
			}
			caps.Set(capability.ObjectFormat, "sha1")
			cmd := &packp.Command{Name: "refs/heads/main", Old: plumbing.ZeroHash, New: formatHash(config.SHA1, "2")}
			var body bytes.Buffer
			require.NoError(t, (&packp.UpdateRequests{Capabilities: caps, Commands: []*packp.Command{cmd}}).Encode(&body))
			body.WriteString("not a pack")

			var out bytes.Buffer
			err := ReceivePack(context.Background(), formatStorage(t, config.SHA256),
				io.NopCloser(&body), ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true})
			require.EqualError(t, err, text)
			assert.Equal(t, tc.want(t), out.String())
		})
	}
}

// receive-pack reads the first object-format value, as parse_feature_value
// does; a bare object-format is the empty format.
func TestReceivePackObjectFormatErrorValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		vals    []string // nil = not sent
		present bool
		wantErr string
	}{
		{wantErr: "unsupported object format 'sha1'"},
		{present: true, wantErr: "unsupported object format ''"},
		{present: true, vals: []string{"sha256", "sha1"}},
		{present: true, vals: []string{"sha1", "sha256"}, wantErr: "unsupported object format 'sha1'"},
		{present: true, vals: []string{"x\x1b[2J\nfatal: \xff"}, wantErr: "unsupported object format 'x?[2J?fatal: ?'"},
	}
	for _, tc := range tests {
		caps := capability.List{}
		if tc.present {
			caps.Add(capability.ObjectFormat, tc.vals...)
		}
		el := receivePackObjectFormatError(config.SHA256, caps)
		if tc.wantErr == "" {
			assert.Nil(t, el, "%v", tc.vals)
			continue
		}
		require.NotNil(t, el, "%v", tc.vals)
		assert.Equal(t, tc.wantErr, el.Text)
	}
}

// A request that decodes to zero commands (only a shallow line) carries no
// capabilities; git checks nothing then, and the request succeeds without a
// response. A bare "0000" body never reaches Decode, so it would not exercise
// the guard.
func TestReceivePackObjectFormatNoCommands(t *testing.T) {
	t.Parallel()
	var body bytes.Buffer
	_, err := pktline.Writef(&body, "shallow %s\n", strings.Repeat("a", 64))
	require.NoError(t, err)
	require.NoError(t, pktline.WriteFlush(&body))

	var out bytes.Buffer
	err = ReceivePack(context.Background(), formatStorage(t, config.SHA256),
		io.NopCloser(&body), ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true})
	require.NoError(t, err)
	assert.Empty(t, out.String(), "a request without commands gets no response")
}

func TestUploadPackV2ObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repo    config.ObjectFormat
		headers []string // nil = no object-format line
		wantErr string
	}{
		{repo: config.SHA1},
		{repo: config.SHA1, headers: []string{"object-format=sha1"}},
		{repo: config.SHA1, headers: []string{"object-format=sha256"}, wantErr: "mismatched object format: server sha1; client sha256"},
		{repo: config.SHA1, headers: []string{"object-format=bogus"}, wantErr: "unknown object format 'bogus'"},
		{repo: config.SHA1, headers: []string{"object-format=bogus", "object-format=sha1"}, wantErr: "unknown object format 'bogus'"},
		// Control characters and invalid UTF-8 in the echoed value are replaced.
		{repo: config.SHA1, headers: []string{"object-format=sha\x1b[0m\n\t\xff"}, wantErr: "unknown object format 'sha?[0m???'"},
		{repo: config.SHA1, headers: []string{"object-format"}, wantErr: "object-format capability requires an argument"},
		{repo: config.SHA256, wantErr: "mismatched object format: server sha256; client sha1"},
		{repo: config.SHA256, headers: []string{"object-format=sha1"}, wantErr: "mismatched object format: server sha256; client sha1"},
		{repo: config.SHA256, headers: []string{"object-format=sha256"}},
		// serve.c keeps the last object-format line.
		{repo: config.SHA256, headers: []string{"object-format=sha1", "object-format=sha256"}},
		{repo: config.SHA256, headers: []string{"object-format=sha256", "object-format=sha1"}, wantErr: "mismatched object format: server sha256; client sha1"},
	}

	for _, command := range []string{"ls-refs", "fetch"} {
		for _, tc := range tests {
			t.Run(command+"/"+tc.repo.String()+"/"+strings.Join(tc.headers, ","), func(t *testing.T) {
				t.Parallel()
				var args []string
				if command == "fetch" {
					// No wants: an accepted fetch emits nothing, which is
					// enough to tell acceptance from rejection.
					args = []string{"done"}
				}
				var out bytes.Buffer
				err := UploadPack(context.Background(), formatStorage(t, tc.repo),
					v2Request(t, command, tc.headers, args), ioutil.WriteNopCloser(&out),
					&UploadPackRequest{GitProtocol: "version=2", StatelessRPC: true})

				if tc.wantErr == "" {
					require.NoError(t, err)
					assert.NotContains(t, out.String(), "ERR ")
					return
				}
				require.ErrorContains(t, err, tc.wantErr)
				var el *pktline.ErrorLine
				assert.True(t, errors.As(err, &el))
				assert.Contains(t, out.String(), "ERR "+tc.wantErr)
			})
		}
	}
}
