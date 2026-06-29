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

// receivePackRequest builds a wire-format receive-pack body for a Delete of
// the named ref at hash, with ReportStatus advertised plus any extra caps.
// Delete-only requests skip the packfile, which keeps tests focused on the
// hook plumbing rather than pack handling.
func receivePackRequest(t *testing.T, ref plumbing.ReferenceName, hash plumbing.Hash, extra ...capability.Capability) io.ReadCloser {
	t.Helper()

	caps := capability.List{}
	caps.Add(capability.ReportStatus)
	for _, c := range extra {
		caps.Add(c)
	}

	req := &packp.UpdateRequests{
		Capabilities: caps,
		Commands: []*packp.Command{{
			Name: ref,
			Old:  hash,
			New:  plumbing.ZeroHash,
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, req.Encode(&buf))
	return io.NopCloser(&buf)
}

func seedRef(t *testing.T, ref plumbing.ReferenceName, hash plumbing.Hash) storage.Storer {
	t.Helper()
	st := memory.NewStorage()
	require.NoError(t, st.SetReference(plumbing.NewHashReference(ref, hash)))
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
		receivePackRequest(t, ref, hash),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "unpack ok")
	assert.Contains(t, out.String(), "ok refs/heads/main")

	_, err = st.Reference(ref)
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestReceivePackPreReceiveAllowsUpdate(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, hash)

	var (
		out      bytes.Buffer
		seen     []*packp.Command
		gotStore storage.Storer
	)
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, ref, hash),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: &ReceivePackHooks{
				PreReceive: func(_ context.Context, s storage.Storer, cmds []*packp.Command, _ io.Writer) error {
					gotStore = s
					seen = cmds
					return nil
				},
			},
		},
	)
	require.NoError(t, err)

	assert.Same(t, st, gotStore)
	require.Len(t, seen, 1)
	assert.Equal(t, ref, seen[0].Name)
	assert.Equal(t, packp.Delete, seen[0].Action())
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
		receivePackRequest(t, ref, hash),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: &ReceivePackHooks{
				PreReceive: func(context.Context, storage.Storer, []*packp.Command, io.Writer) error {
					return errors.New("policy blocks main")
				},
				PostReceive: func(context.Context, storage.Storer, []*packp.Command) {
					postReceiveCalled = true
				},
			},
		},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy blocks main")

	assert.Contains(t, out.String(), "unpack ok")
	assert.Contains(t, out.String(), "ng refs/heads/main policy blocks main")
	assert.False(t, postReceiveCalled, "PostReceive must not run when PreReceive rejects")

	got, err := st.Reference(ref)
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
		postCmds   []*packp.Command
		refMissing bool
	)
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, ref, hash),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: &ReceivePackHooks{
				PostReceive: func(_ context.Context, s storage.Storer, cmds []*packp.Command) {
					postCmds = cmds
					_, refErr := s.Reference(ref)
					refMissing = errors.Is(refErr, plumbing.ErrReferenceNotFound)
				},
			},
		},
	)
	require.NoError(t, err)

	require.Len(t, postCmds, 1)
	assert.Equal(t, ref, postCmds[0].Name)
	assert.True(t, refMissing, "ref must be gone by the time PostReceive runs")
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
		receivePackRequest(t, ref, hash, capability.Sideband64k),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: &ReceivePackHooks{
				PreReceive: func(_ context.Context, _ storage.Storer, _ []*packp.Command, progress io.Writer) error {
					_, _ = io.WriteString(progress, "policy check passed\n")
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
	if _, err := io.Copy(&p.data, demux); err != nil && !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	return p
}
