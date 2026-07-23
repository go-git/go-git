package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const receivePackTestHash = "0123456789012345678901234567890123456789"

// receivePackRequest builds a wire-format receive-pack body for the given
// commands, with ReportStatus advertised plus any extra caps. Tests use
// Delete-only commands so no packfile follows, which keeps focus on hook
// plumbing rather than pack handling.
func receivePackRequest(t *testing.T, cmds []*packp.Command, extra ...capability.Capability) io.ReadCloser {
	t.Helper()

	caps := capability.List{}
	caps.Add(capability.ReportStatus)
	for _, c := range extra {
		caps.Add(c)
	}

	req := &packp.UpdateRequests{
		Capabilities: caps,
		Commands:     cmds,
	}

	var buf bytes.Buffer
	require.NoError(t, req.Encode(&buf))
	return io.NopCloser(&buf)
}

func deleteCmd(ref plumbing.ReferenceName, hash plumbing.Hash) *packp.Command {
	return &packp.Command{Name: ref, Old: hash, New: plumbing.ZeroHash}
}

func seedRef(t *testing.T, ref plumbing.ReferenceName, hash plumbing.Hash) storage.Storer {
	t.Helper()
	st := memory.NewStorage()
	require.NoError(t, st.SetReference(t.Context(), plumbing.NewHashReference(ref, hash)))
	return st
}

func TestReceivePackNilHooksDeleteRef(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "unpack ok")
	assert.Contains(t, out.String(), "ok refs/heads/main")

	_, err = st.Reference(t.Context(), ref)
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestReceivePackPreReceiveAllowsUpdate(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	var (
		out  bytes.Buffer
		info *PreReceiveInfo
	)
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PreReceive: func(_ context.Context, i *PreReceiveInfo) error {
					info = i
					return nil
				},
			},
		},
	)
	require.NoError(t, err)

	require.NotNil(t, info)
	assert.Same(t, st, info.Storer)
	assert.NotNil(t, info.Progress)
	assert.Empty(t, info.PushOptions)
	require.Len(t, info.Commands, 1)
	assert.Equal(t, ref, info.Commands[0].Name)
	assert.Equal(t, packp.Delete, info.Commands[0].Action())
	assert.Contains(t, out.String(), "ok refs/heads/main")
}

func TestReceivePackPreReceiveRejectsRef(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	postReceiveCalled := false
	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PreReceive: func(context.Context, *PreReceiveInfo) error {
					return errors.New("policy blocks main")
				},
				PostReceive: func(context.Context, *PostReceiveInfo) error {
					postReceiveCalled = true
					return nil
				},
			},
		},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy blocks main")

	assert.Contains(t, out.String(), "unpack ok")
	assert.Contains(t, out.String(), "ng refs/heads/main policy blocks main")
	assert.False(t, postReceiveCalled, "PostReceive must not run when PreReceive rejects")

	got, err := st.Reference(t.Context(), ref)
	require.NoError(t, err)
	assert.Equal(t, hash, got.Hash(), "ref must not move when PreReceive rejects")
}

func TestReceivePackPostReceiveRunsAfterUpdate(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	var (
		out        bytes.Buffer
		info       *PostReceiveInfo
		refMissing bool
	)
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PostReceive: func(_ context.Context, i *PostReceiveInfo) error {
					info = i
					_, refErr := i.Storer.Reference(t.Context(), ref)
					refMissing = errors.Is(refErr, plumbing.ErrReferenceNotFound)
					return nil
				},
			},
		},
	)
	require.NoError(t, err)

	require.NotNil(t, info)
	require.Len(t, info.Commands, 1)
	assert.Equal(t, ref, info.Commands[0].Name)
	assert.NotNil(t, info.Progress)
	assert.True(t, refMissing, "ref must be gone by the time PostReceive runs")
}

func TestReceivePackPostReceivePartialSuccess(t *testing.T) {
	t.Parallel()

	good := plumbing.ReferenceName("refs/heads/good")
	bad := plumbing.ReferenceName("refs/heads/bad")
	hash := plumbing.NewHash(receivePackTestHash)
	// Only seed `good`; deleting `bad` will fail with ErrUpdateReference.
	st := seedRef(t, good, hash)

	var (
		out  bytes.Buffer
		info *PostReceiveInfo
	)
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			deleteCmd(good, hash),
			deleteCmd(bad, hash),
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PostReceive: func(_ context.Context, i *PostReceiveInfo) error {
					info = i
					return nil
				},
			},
		},
	)
	require.ErrorIs(t, err, ErrUpdateReference)

	require.NotNil(t, info)
	require.Len(t, info.Commands, 1, "PostReceive must only see refs that applied")
	assert.Equal(t, good, info.Commands[0].Name)

	assert.Contains(t, out.String(), "ok refs/heads/good")
	assert.Contains(t, out.String(), "ng refs/heads/bad")
}

func TestReceivePackPreReceiveWritesProgressOnSideband(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}, capability.Sideband64k),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PreReceive: func(_ context.Context, info *PreReceiveInfo) error {
					_, _ = io.WriteString(info.Progress, "policy check passed\n")
					return nil
				},
			},
		},
	)
	require.NoError(t, err)

	demuxed := readSideband(t, &out)
	assert.Contains(t, demuxed.progress.String(), "policy check passed")
	assert.Contains(t, demuxed.data.String(), "ok refs/heads/main")
}

type sidebandPayload struct {
	data     bytes.Buffer
	progress bytes.Buffer
}

func readSideband(t *testing.T, r io.Reader) sidebandPayload {
	t.Helper()
	var p sidebandPayload
	demux := sideband.NewDemuxer(sideband.Sideband64k, r)
	demux.Progress = &p.progress
	_, err := io.Copy(&p.data, demux)
	require.NoError(t, err)
	return p
}
