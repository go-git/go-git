package transport

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestObjectFormatNilStorer(t *testing.T) {
	t.Parallel()
	assert.Equal(t, config.DefaultObjectFormat, objectFormat(nil))
	assert.Equal(t, config.DefaultObjectFormat, objectFormat((*memory.Storage)(nil)))
}

// SendPack ignored its storer before it checked the object format, so a
// caller passing a typed nil storer must not panic now.
func TestSendPackTypedNilStorer(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Set(capability.ReportStatus)
	var st *memory.Storage
	assert.NotPanics(t, func() { _, _ = sendPackCaptured(t, st, caps) })
}

// sendPackCaptured runs SendPack for a single delete command (no packfile)
// and returns what the client wrote.
func sendPackCaptured(t *testing.T, st storage.Storer, caps capability.List) (string, error) {
	t.Helper()
	writer := newMockWriteCloser(nil)
	reader := io.NopCloser(bytes.NewReader(nil))
	hexLen := config.DefaultObjectFormat.HexSize()
	if st != nil {
		hexLen = objectFormat(st).HexSize()
	}
	req := &PushRequest{
		Commands: []*packp.Command{{
			Name: plumbing.ReferenceName("refs/heads/main"),
			Old:  plumbing.NewHash(strings.Repeat("1", hexLen)),
			New:  plumbing.ZeroHash,
		}},
	}
	err := SendPack(context.Background(), st, caps, writer, reader, req)
	return writer.writeBuf.String(), err
}

func TestSendPackObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		local      config.ObjectFormat
		advertised []string // nil: server does not advertise object-format
		want       string   // expected "object-format=<v>" in the request, "" for none
	}{
		{name: "sha1, not advertised", local: config.SHA1},
		{name: "sha1, advertised sha1", local: config.SHA1, advertised: []string{"sha1"}, want: "object-format=sha1"},
		{name: "sha256, advertised sha256", local: config.SHA256, advertised: []string{"sha256"}, want: "object-format=sha256"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage(memory.WithObjectFormat(tc.local))
			caps := capability.List{}
			if tc.advertised != nil {
				caps.Set(capability.ObjectFormat, tc.advertised...)
			}

			out, err := sendPackCaptured(t, st, caps)
			require.NoError(t, err)
			if tc.want == "" {
				assert.NotContains(t, out, "object-format")
			} else {
				assert.Contains(t, out, tc.want)
			}
		})
	}
}

func TestSendPackObjectFormatNilStorer(t *testing.T) {
	t.Parallel()
	caps := capability.List{}
	caps.Set(capability.ObjectFormat, "sha1")
	out, err := sendPackCaptured(t, nil, caps)
	require.NoError(t, err)
	assert.Contains(t, out, "object-format=sha1")
}

func TestSendPackSizesZeroIDs(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage(memory.WithObjectFormat(config.SHA256))
	caps := capability.List{}
	caps.Set(capability.ObjectFormat, "sha256")
	newID := plumbing.NewHash(strings.Repeat("2", 64))
	cmd := &packp.Command{Name: "refs/heads/main", Old: plumbing.ZeroHash, New: newID}
	req := &PushRequest{Commands: []*packp.Command{cmd}, Packfile: io.NopCloser(&bytes.Buffer{})}

	writer := newMockWriteCloser(nil)
	require.NoError(t, SendPack(context.Background(), st, caps, writer, io.NopCloser(bytes.NewReader(nil)), req))
	assert.Contains(t, writer.writeBuf.String(), strings.Repeat("0", 64)+" "+newID.String()+" refs/heads/main")
	assert.Equal(t, plumbing.ZeroHash, req.Commands[0].Old, "the request's commands must not be modified")
}

func TestSizeZeroIDsReturnsInputWhenNothingToResize(t *testing.T) {
	t.Parallel()
	zero := []*packp.Command{{Name: "refs/heads/main", Old: plumbing.ZeroHash, New: formatHash(config.SHA1, "2")}}
	full := []*packp.Command{{Name: "refs/heads/main", Old: formatHash(config.SHA256, "1"), New: formatHash(config.SHA256, "2")}}

	for name, tc := range map[string]struct {
		cmds []*packp.Command
		f    config.ObjectFormat
	}{
		"sha1 with a zero id":    {zero, config.SHA1},
		"sha256 without zero id": {full, config.SHA256},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := sizeZeroIDs(tc.cmds, tc.f)
			require.Len(t, out, len(tc.cmds))
			assert.Same(t, &tc.cmds[0], &out[0], "the input slice must be returned as is")
		})
	}
}

func TestSizeZeroIDsCopiesWhenResizing(t *testing.T) {
	t.Parallel()
	newID := formatHash(config.SHA256, "2")
	cmds := []*packp.Command{
		{Name: "refs/heads/a", Old: plumbing.ZeroHash, New: newID},
		{Name: "refs/heads/b", Old: newID, New: plumbing.ZeroHash},
	}

	out := sizeZeroIDs(cmds, config.SHA256)
	require.Len(t, out, 2)
	assert.Equal(t, strings.Repeat("0", 64), out[0].Old.String())
	assert.Equal(t, newID, out[0].New)
	assert.Equal(t, newID, out[1].Old)
	assert.Equal(t, strings.Repeat("0", 64), out[1].New.String())
	for i := range cmds {
		assert.NotSame(t, cmds[i], out[i])
	}
	assert.Equal(t, plumbing.ZeroHash, cmds[0].Old, "the input commands must not be modified")
	assert.Equal(t, plumbing.ZeroHash, cmds[1].New, "the input commands must not be modified")
}

func formatHash(f config.ObjectFormat, digit string) plumbing.Hash {
	return plumbing.NewHash(strings.Repeat(digit, f.HexSize()))
}
