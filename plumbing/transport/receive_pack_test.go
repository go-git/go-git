package transport

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const receivePackTestHash = "0123456789012345678901234567890123456789"

// receivePackRequest builds a wire-format receive-pack body for the given
// commands, with ReportStatus advertised plus any extra caps. A packfile
// follows unless every command is a Delete, because receive-pack expects one
// there; the pack is empty so that these tests stay on ref handling rather than
// pack decoding.
func receivePackRequest(t *testing.T, cmds []*packp.Command, extra ...capability.Capability) io.ReadCloser {
	t.Helper()

	// A zero-object packfile: the "PACK" signature, version 2 and an object
	// count of 0, followed by the SHA-1 of those twelve bytes.
	header := []byte("PACK\x00\x00\x00\x02\x00\x00\x00\x00")
	sum := sha1.Sum(header)

	return receivePackBody(t, cmds, append(header, sum[:]...), extra...)
}

// receivePackBody builds a wire-format receive-pack body carrying pack where
// the packfile belongs, so a caller can hand receive-pack something that will
// not decode. The pack is written only if some command needs one.
func receivePackBody(t *testing.T, cmds []*packp.Command, pack []byte, extra ...capability.Capability) io.ReadCloser {
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

	for _, cmd := range cmds {
		if cmd.Action() != packp.Delete {
			buf.Write(pack)
			break
		}
	}

	return io.NopCloser(&buf)
}

func deleteCmd(ref plumbing.ReferenceName, hash plumbing.Hash) *packp.Command {
	return &packp.Command{Name: ref, Old: hash, New: plumbing.ZeroHash}
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
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
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
					_, refErr := i.Storer.Reference(ref)
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

	// A refused ref is reported per-command; the unpack status describes the
	// packfile only and must stay "ok".
	assert.Contains(t, out.String(), "unpack ok")
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

// funnyNames are refnames receive-pack must refuse whatever storer sits behind
// it: upstream's builtin/receive-pack.c reports "funny refname" for a command
// whose name is not under refs/ or fails its format check. Every test here
// drives ReceivePack against memory.NewStorage(), so a refusal can only come
// from the transport gate — the dotgit layer's own checks are not in the way.
var funnyNames = []plumbing.ReferenceName{
	// Root refs and top-level metadata: not under refs/ at all.
	"HEAD",
	"CONFIG",
	"config",
	"INDEX",
	"SHALLOW",
	"ORIG_HEAD",
	// Escapes from the refs/ sub-tree, spelled literally...
	"refs/../CONFIG",
	"refs/heads/../../config",
	// ...and disguised with the code points HFS+ drops during path
	// normalisation. Each component below is a ".." to the filesystem while
	// holding no literal "..", which is exactly what carries it past IsSafe
	// (a literal comparison) and Validate (rule 3, "contains ..").
	// ZERO WIDTH NON-JOINER around the dots:
	"refs/\u200c.\u200c./CONFIG",
	"refs/heads/\u200c.\u200c./\u200c.\u200c./config",
	// ZERO WIDTH NO-BREAK SPACE (the UTF-8 BOM):
	"refs/\ufeff.\ufeff.\ufeff/CONFIG",
	// LEFT-TO-RIGHT MARK:
	"refs/\u200e.\u200e./config",
	// A component the filesystem reads as a single "." aliases the directory
	// it sits in, so each of these names resolves to a ref one level up while
	// holding no literal "." component. The leading-ignorable spellings pass
	// every other gate: IsSafe compares literally, and Validate's rule 1 sees
	// a component starting with a code point rather than a dot.
	"refs/\u200c./heads/main",
	"refs/heads/\u200c./main",
	"refs/heads/\u200c.\u200c/main",
	"refs/\ufeff./heads/main",
	"refs/heads/.:$DATA/main",
	// Whitespace-containing names are covered by
	// TestReceivePackRejectsCompleteWhitespaceRefname, which checks that
	// decoding preserves the complete name for validation.
	// Malformed by check_refname_format.
	"refs/",
	"refs/heads/foo..bar",
	"refs/heads/foo.lock",
	"refs/heads/foo\\bar",
}

// benignNames are refnames this gate must let through. Each has been mistaken
// for malformed at some point. Git restricts a leading hyphen in branch and
// tag shorthands at creation time; check_refname_format permits both leading
// hyphens and "@" components in full names.
//
// refs/stash is the exception, and it is here on purpose. Real git answers
// "ng refs/stash funny refname" to a create, because it strips "refs/" before
// the format check and one level is left; go-git validates the whole name and
// accepts it. That divergence is the one documented at checkRefname.
var benignNames = []plumbing.ReferenceName{
	"refs/heads/main",
	"refs/heads/-foo",
	"refs/tags/-1.0",
	"refs/heads/@",
	"refs/remotes/origin/@",
	"refs/stash",
	"refs/notes/commits",
}

func TestReceivePackRefusesFunnyRefnameCreate(t *testing.T) {
	t.Parallel()

	for _, name := range funnyNames {
		t.Run(name.String(), func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()
			hash := plumbing.NewHash(receivePackTestHash)

			var out bytes.Buffer
			err := ReceivePack(
				context.Background(),
				st,
				receivePackRequest(t, []*packp.Command{
					{Name: name, Old: plumbing.ZeroHash, New: hash},
				}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			)
			require.ErrorIs(t, err, ErrFunnyRefname)

			assert.Contains(t, out.String(), "unpack ok")
			assert.Contains(t, out.String(), "ng "+name.String()+" funny refname")

			_, refErr := st.Reference(name)
			assert.ErrorIs(t, refErr, plumbing.ErrReferenceNotFound,
				"%q must not be created", name)
		})
	}
}

func TestReceivePackRefusesFunnyRefnameUpdate(t *testing.T) {
	t.Parallel()

	for _, name := range funnyNames {
		t.Run(name.String(), func(t *testing.T) {
			t.Parallel()

			old := plumbing.NewHash("1111111111111111111111111111111111111111")
			st := seedRef(t, name, old)

			var out bytes.Buffer
			err := ReceivePack(
				context.Background(),
				st,
				receivePackRequest(t, []*packp.Command{
					{Name: name, Old: old, New: plumbing.NewHash(receivePackTestHash)},
				}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			)
			require.ErrorIs(t, err, ErrFunnyRefname)

			assert.Contains(t, out.String(), "ng "+name.String()+" funny refname")

			got, refErr := st.Reference(name)
			require.NoError(t, refErr)
			assert.Equal(t, old, got.Hash(), "%q must not move", name)
		})
	}
}

func TestReceivePackRefusesFunnyRefnameDelete(t *testing.T) {
	t.Parallel()

	for _, name := range funnyNames {
		t.Run(name.String(), func(t *testing.T) {
			t.Parallel()

			hash := plumbing.NewHash(receivePackTestHash)
			st := seedRef(t, name, hash)

			var out bytes.Buffer
			err := ReceivePack(
				context.Background(),
				st,
				receivePackRequest(t, []*packp.Command{deleteCmd(name, hash)}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			)
			require.ErrorIs(t, err, ErrFunnyRefname)

			assert.Contains(t, out.String(), "ng "+name.String()+" funny refname")

			_, refErr := st.Reference(name)
			assert.NoError(t, refErr, "%q must not be deleted", name)
		})
	}
}

func TestReceivePackAcceptsBenignRefnames(t *testing.T) {
	t.Parallel()

	for _, name := range benignNames {
		t.Run(name.String(), func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()
			hash := plumbing.NewHash(receivePackTestHash)

			var out bytes.Buffer
			err := ReceivePack(
				context.Background(),
				st,
				receivePackRequest(t, []*packp.Command{
					{Name: name, Old: plumbing.ZeroHash, New: hash},
				}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			)
			require.NoError(t, err)

			assert.Contains(t, out.String(), "ok "+name.String())

			got, refErr := st.Reference(name)
			require.NoError(t, refErr, "%q must be created", name)
			assert.Equal(t, hash, got.Hash())
		})
	}
}

func TestReceivePackFunnyRefnameDoesNotBlockGoodRefs(t *testing.T) {
	t.Parallel()

	good := plumbing.ReferenceName("refs/heads/ok")
	hash := plumbing.NewHash(receivePackTestHash)
	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: "CONFIG", Old: plumbing.ZeroHash, New: hash},
			{Name: good, Old: plumbing.ZeroHash, New: hash},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrFunnyRefname)

	assert.Contains(t, out.String(), "ng CONFIG funny refname")
	assert.Contains(t, out.String(), "ok refs/heads/ok")

	ref, refErr := st.Reference(good)
	require.NoError(t, refErr)
	assert.Equal(t, hash, ref.Hash())
}

func TestReceivePackFunnyRefnameReportsOnSideband(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: "CONFIG", Old: plumbing.ZeroHash, New: plumbing.NewHash(receivePackTestHash)},
		}, capability.Sideband64k),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrFunnyRefname)

	demuxed := readSideband(t, &out)
	assert.Contains(t, demuxed.data.String(), "unpack ok")
	assert.Contains(t, demuxed.data.String(), "ng CONFIG funny refname")
}

// TestReceivePackFunnyRefnameWireFormat pins the bytes rather than the error,
// because the report-status wire format is what a real git client parses: the
// unpack line stays "ok" (the packfile was fine, one command was not), the
// refusal is a single "ng" line carrying ErrFunnyRefname's message verbatim,
// and the report ends with a flush.
func TestReceivePackFunnyRefnameWireFormat(t *testing.T) {
	t.Parallel()

	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: "CONFIG", Old: plumbing.ZeroHash, New: plumbing.NewHash(receivePackTestHash)},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrFunnyRefname)

	assert.Equal(t, "000eunpack ok\n001cng CONFIG funny refname\n0000", out.String())
}

func TestReceivePackAcceptsWellFormedRefs(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash(receivePackTestHash)
	other := plumbing.NewHash("1111111111111111111111111111111111111111")

	for _, name := range []plumbing.ReferenceName{
		"refs/heads/main",
		"refs/heads/feature/nested/name",
		"refs/heads/release-1.2",
		"refs/tags/v1.0.0",
		"refs/stash",
		"refs/remotes/origin/HEAD",
		// Namespaces outside refs/heads and refs/tags that tools push to, all
		// accepted by upstream git's receive-pack.
		"refs/notes/commits",
		"refs/replace/deadbeef",
		"refs/meta/config",
		"refs/for/main",
		"refs/pull/1/head",
		"refs/keep-around/abc123",
	} {
		t.Run(name.String(), func(t *testing.T) {
			t.Parallel()

			st := memory.NewStorage()

			var out bytes.Buffer
			require.NoError(t, ReceivePack(
				context.Background(), st,
				receivePackRequest(t, []*packp.Command{
					{Name: name, Old: plumbing.ZeroHash, New: other},
				}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			))
			assert.Contains(t, out.String(), "ok "+name.String())

			out.Reset()
			require.NoError(t, ReceivePack(
				context.Background(), st,
				receivePackRequest(t, []*packp.Command{
					{Name: name, Old: other, New: hash},
				}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			))
			assert.Contains(t, out.String(), "ok "+name.String())
			ref, refErr := st.Reference(name)
			require.NoError(t, refErr)
			assert.Equal(t, hash, ref.Hash())

			out.Reset()
			require.NoError(t, ReceivePack(
				context.Background(), st,
				receivePackRequest(t, []*packp.Command{deleteCmd(name, hash)}),
				ioutil.WriteNopCloser(&out),
				&ReceivePackRequest{StatelessRPC: true},
			))
			assert.Contains(t, out.String(), "ok "+name.String())
			_, refErr = st.Reference(name)
			assert.ErrorIs(t, refErr, plumbing.ErrReferenceNotFound)
		})
	}
}

// closeCountingWriter records how often the response was closed, which is what
// ends it for a real caller's transport, and can fail the close on demand.
type closeCountingWriter struct {
	buf      bytes.Buffer
	closes   int
	closeErr error
	writeErr error
}

func (w *closeCountingWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.buf.Write(p)
}

func (w *closeCountingWriter) Close() error {
	w.closes++
	return w.closeErr
}

// receivePackRequestWithoutReportStatus builds a request that advertises no
// capabilities at all, so ReceivePack returns before it writes a status.
func receivePackRequestWithoutReportStatus(t *testing.T, cmds []*packp.Command) io.ReadCloser {
	t.Helper()

	req := &packp.UpdateRequests{Capabilities: capability.List{}, Commands: cmds}
	var buf bytes.Buffer
	require.NoError(t, req.Encode(&buf))
	return io.NopCloser(&buf)
}

// TestReceivePackClosesWriterOnEveryExit pins the contract that every return
// from ReceivePack closes the writer exactly once, the early ones that never
// write a byte included: a caller whose transport ends the response on close
// would otherwise leave a client reading a stream that never ends.
func TestReceivePackClosesWriterOnEveryExit(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)

	noBody := func(*testing.T) io.ReadCloser { return nil }

	tests := []struct {
		name    string
		body    func(*testing.T) io.ReadCloser
		opts    *ReceivePackRequest
		wantErr bool
	}{
		{
			name:    "nil reader",
			body:    noBody,
			opts:    &ReceivePackRequest{StatelessRPC: true},
			wantErr: true,
		},
		{
			name: "advertisement only",
			body: noBody,
			opts: &ReceivePackRequest{AdvertiseRefs: true, StatelessRPC: true},
		},
		{
			name: "client sends flush",
			body: func(t *testing.T) io.ReadCloser {
				var buf bytes.Buffer
				require.NoError(t, pktline.WriteFlush(&buf))
				return io.NopCloser(&buf)
			},
			opts: &ReceivePackRequest{StatelessRPC: true},
		},
		{
			name: "malformed request",
			body: func(t *testing.T) io.ReadCloser {
				var buf bytes.Buffer
				_, err := pktline.WriteString(&buf, "junk\n")
				require.NoError(t, err)
				return io.NopCloser(&buf)
			},
			opts:    &ReceivePackRequest{StatelessRPC: true},
			wantErr: true,
		},
		{
			name: "no report-status capability",
			body: func(t *testing.T) io.ReadCloser {
				return receivePackRequestWithoutReportStatus(t,
					[]*packp.Command{deleteCmd(ref, hash)})
			},
			opts: &ReceivePackRequest{StatelessRPC: true},
		},
		{
			name: "reference updated",
			body: func(t *testing.T) io.ReadCloser {
				return receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)})
			},
			opts: &ReceivePackRequest{StatelessRPC: true},
		},
		{
			name: "refname refused",
			body: func(t *testing.T) io.ReadCloser {
				return receivePackRequest(t, []*packp.Command{
					{Name: plumbing.HEAD, Old: plumbing.ZeroHash, New: hash},
				})
			},
			opts:    &ReceivePackRequest{StatelessRPC: true},
			wantErr: true,
		},
		{
			name: "pre-receive rejects",
			body: func(t *testing.T) io.ReadCloser {
				return receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)})
			},
			opts: &ReceivePackRequest{
				StatelessRPC: true,
				Hooks: ReceivePackHooks{
					PreReceive: func(context.Context, *PreReceiveInfo) error {
						return errors.New("refused by policy")
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &closeCountingWriter{}
			err := ReceivePack(context.Background(), seedRef(t, ref, hash),
				tc.body(t), w, tc.opts)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, w.closes, "writer must be closed exactly once")
		})
	}
}

// TestReceivePackClosesWriterWhenStatusFails covers the exit a failed status
// write takes. The report never reaches the client there, which leaves the
// close as the only thing that can end the response.
func TestReceivePackClosesWriterWhenStatusFails(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	writeErr := errors.New("connection reset")

	w := &closeCountingWriter{writeErr: writeErr}
	err := ReceivePack(context.Background(), seedRef(t, ref, hash),
		receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
		w, &ReceivePackRequest{StatelessRPC: true})

	require.ErrorIs(t, err, writeErr)
	assert.Equal(t, 1, w.closes, "writer must be closed when the status write fails")
}

// TestReceivePackCloseErrorYieldsToRequestError keeps the close from speaking
// over the exchange it ends: a refused ref describes the push, while a failure
// to hang up only describes the caller's own writer.
func TestReceivePackCloseErrorYieldsToRequestError(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	closeErr := errors.New("broken pipe")

	t.Run("surfaces when nothing else failed", func(t *testing.T) {
		t.Parallel()

		w := &closeCountingWriter{closeErr: closeErr}
		err := ReceivePack(context.Background(), seedRef(t, ref, hash),
			receivePackRequest(t, []*packp.Command{deleteCmd(ref, hash)}),
			w, &ReceivePackRequest{StatelessRPC: true})

		require.ErrorIs(t, err, closeErr)
		assert.Contains(t, err.Error(), "closing writer")
		assert.Contains(t, w.buf.String(), "ok refs/heads/main")
	})

	t.Run("yields to a refused refname", func(t *testing.T) {
		t.Parallel()

		w := &closeCountingWriter{closeErr: closeErr}
		err := ReceivePack(context.Background(), seedRef(t, ref, hash),
			receivePackRequest(t, []*packp.Command{
				{Name: plumbing.HEAD, Old: plumbing.ZeroHash, New: hash},
			}),
			w, &ReceivePackRequest{StatelessRPC: true})

		require.ErrorIs(t, err, ErrFunnyRefname)
		assert.NotErrorIs(t, err, closeErr)
		assert.Equal(t, 1, w.closes)
	})
}

// TestReceivePackReportsStatusInCommandOrder pins the order of the report, not
// just its contents: git emits one status line per command in the order the
// commands arrived, and a client pairing them up positionally depends on that.
// The statuses are collected in a map, so the lines have to be driven off the
// command list to come out in a stable order at all.
func TestReceivePackReportsStatusInCommandOrder(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash(receivePackTestHash)
	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: "refs/heads/one", Old: plumbing.ZeroHash, New: hash},
			{Name: "CONFIG", Old: plumbing.ZeroHash, New: hash},
			{Name: "refs/heads/two", Old: plumbing.ZeroHash, New: hash},
			{Name: "refs/heads/three", Old: plumbing.ZeroHash, New: hash},
			{Name: "HEAD", Old: plumbing.ZeroHash, New: hash},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrFunnyRefname)

	assert.Equal(t, "000eunpack ok\n"+
		"0016ok refs/heads/one\n"+
		"001cng CONFIG funny refname\n"+
		"0016ok refs/heads/two\n"+
		"0018ok refs/heads/three\n"+
		"001ang HEAD funny refname\n"+
		"0000", out.String())
}

// TestReceivePackUnpackFailure covers the exit taken when the packfile does not
// decode, which is the one failure exit with no test of its own. It has to
// behave like the other two: report the reason to the client, terminate the
// sideband stream, and hand the caller the error rather than a nil that reads
// as a clean push.
func TestReceivePackUnpackFailure(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash(receivePackTestHash)
	cmds := []*packp.Command{
		{Name: "refs/heads/main", Old: plumbing.ZeroHash, New: hash},
	}
	badPack := []byte("this is not a packfile")

	t.Run("plain", func(t *testing.T) {
		t.Parallel()

		st := memory.NewStorage()

		var out bytes.Buffer
		err := ReceivePack(
			context.Background(), st,
			receivePackBody(t, cmds, badPack),
			ioutil.WriteNopCloser(&out),
			&ReceivePackRequest{StatelessRPC: true},
		)
		require.Error(t, err)

		// The unpack line carries the reason, and no command is reported at
		// all: nothing was attempted, so there is no per-ref outcome to give.
		assert.Contains(t, out.String(), "unpack ")
		assert.NotContains(t, out.String(), "refs/heads/main")
		assert.Contains(t, out.String(), err.Error(),
			"the error handed back must be the one reported to the client")

		_, refErr := st.Reference("refs/heads/main")
		assert.ErrorIs(t, refErr, plumbing.ErrReferenceNotFound)
	})

	t.Run("sideband", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := ReceivePack(
			context.Background(), memory.NewStorage(),
			receivePackBody(t, cmds, badPack, capability.Sideband64k),
			ioutil.WriteNopCloser(&out),
			&ReceivePackRequest{StatelessRPC: true},
		)
		require.Error(t, err)

		// ReportStatus.Encode ends with a flush, but on this path that flush is
		// muxed into band 1 like the rest of the report. The byte stream needs
		// a bare flush after it, or the client is left demuxing a sideband
		// stream with no terminator.
		assert.True(t, strings.HasSuffix(out.String(), "\x0100000000"),
			"sideband stream must end with a muxed flush then a bare one, got %q", out.String())

		demuxed := readSideband(t, &out)
		assert.Contains(t, demuxed.data.String(), "unpack ")
	})
}

// TestReceivePackRefusesDuplicateRefname pins the bytes, because one status
// line per command is the property at stake: a client pairs the lines with the
// commands it sent positionally, so two commands must produce two lines even
// when both name the same ref and both carry the same reason.
func TestReceivePackRefusesDuplicateRefname(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	old := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, old)

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: ref, Old: old, New: plumbing.NewHash("1111111111111111111111111111111111111111")},
			{Name: ref, Old: old, New: plumbing.NewHash("2222222222222222222222222222222222222222")},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrDuplicateRefname)
	// The caller is told which name was duplicated; the wire reason is not.
	assert.Contains(t, err.Error(), `"refs/heads/main"`)

	assert.Equal(t, "000eunpack ok\n"+
		"003cng refs/heads/main multiple updates for ref not allowed\n"+
		"003cng refs/heads/main multiple updates for ref not allowed\n"+
		"0000", out.String())

	// Neither command ran, so the ref still holds what it held before.
	got, refErr := st.Reference(ref)
	require.NoError(t, refErr)
	assert.Equal(t, old, got.Hash())
}

// TestReceivePackDuplicateRefnameRefusesWholePush covers the refs that named a
// reference only once. Git batches the updates into a transaction that the
// duplicate aborts, so those refs do not move either and are answered "ng"
// alongside it; a push applies as sent or not at all.
func TestReceivePackDuplicateRefnameRefusesWholePush(t *testing.T) {
	t.Parallel()

	dup := plumbing.ReferenceName("refs/heads/dup")
	innocent := plumbing.ReferenceName("refs/heads/innocent")
	hash := plumbing.NewHash(receivePackTestHash)
	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: dup, Old: plumbing.ZeroHash, New: hash},
			{Name: innocent, Old: plumbing.ZeroHash, New: hash},
			{Name: dup, Old: plumbing.ZeroHash, New: hash},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrDuplicateRefname)

	assert.Equal(t, "000eunpack ok\n"+
		"003bng refs/heads/dup multiple updates for ref not allowed\n"+
		"0040ng refs/heads/innocent multiple updates for ref not allowed\n"+
		"003bng refs/heads/dup multiple updates for ref not allowed\n"+
		"0000", out.String())

	for _, n := range []plumbing.ReferenceName{dup, innocent} {
		_, refErr := st.Reference(n)
		assert.ErrorIs(t, refErr, plumbing.ErrReferenceNotFound, "ref %q", n)
	}
}

// TestReceivePackDuplicateRefnameMixedActions covers a duplicate whose two
// commands disagree about the action. Real git splits deletes from updates into
// separate transactions, so there the delete lands while every command still
// reports failure; refusing the request before anything runs keeps the report
// and the repository saying the same thing.
func TestReceivePackDuplicateRefnameMixedActions(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	old := plumbing.NewHash(receivePackTestHash)
	st := seedRef(t, ref, old)

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: ref, Old: old, New: plumbing.NewHash("1111111111111111111111111111111111111111")},
			deleteCmd(ref, old),
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrDuplicateRefname)

	got, refErr := st.Reference(ref)
	require.NoError(t, refErr, "the delete must not have run")
	assert.Equal(t, old, got.Hash(), "the update must not have run")
}

// TestReceivePackDuplicateRefnameSkipsHooks asserts the request is refused
// before PreReceive. A hook decides policy and may have side effects of its
// own, so it is not consulted about a push that cannot apply whatever it
// answers.
func TestReceivePackDuplicateRefnameSkipsHooks(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := memory.NewStorage()

	var preCalled, postCalled bool
	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: ref, Old: plumbing.ZeroHash, New: hash},
			{Name: ref, Old: plumbing.ZeroHash, New: hash},
		}),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{
			StatelessRPC: true,
			Hooks: ReceivePackHooks{
				PreReceive: func(context.Context, *PreReceiveInfo) error {
					preCalled = true
					return nil
				},
				PostReceive: func(context.Context, *PostReceiveInfo) error {
					postCalled = true
					return nil
				},
			},
		},
	)
	require.ErrorIs(t, err, ErrDuplicateRefname)
	assert.False(t, preCalled, "PreReceive must not run for an unapplyable push")
	assert.False(t, postCalled, "PostReceive must not run when no ref moved")
}

// TestReceivePackDuplicateRefnameOnSideband checks the refusal takes the same
// route as the other reporting exits: muxed into band 1, then the flush that
// ends the stream the client is demuxing.
func TestReceivePackDuplicateRefnameOnSideband(t *testing.T) {
	t.Parallel()

	ref := plumbing.ReferenceName("refs/heads/main")
	hash := plumbing.NewHash(receivePackTestHash)
	st := memory.NewStorage()

	var out bytes.Buffer
	err := ReceivePack(
		context.Background(),
		st,
		receivePackRequest(t, []*packp.Command{
			{Name: ref, Old: plumbing.ZeroHash, New: hash},
			{Name: ref, Old: plumbing.ZeroHash, New: hash},
		}, capability.Sideband64k),
		ioutil.WriteNopCloser(&out),
		&ReceivePackRequest{StatelessRPC: true},
	)
	require.ErrorIs(t, err, ErrDuplicateRefname)

	demuxed := readSideband(t, &out)
	assert.Contains(t, demuxed.data.String(), "unpack ok")
	assert.Equal(t, 2, strings.Count(demuxed.data.String(),
		"ng refs/heads/main multiple updates for ref not allowed"))
}

func TestDuplicateRefname(t *testing.T) {
	t.Parallel()

	cmd := func(n string) *packp.Command {
		return &packp.Command{Name: plumbing.ReferenceName(n)}
	}

	for _, tc := range []struct {
		name string
		cmds []*packp.Command
		want string
	}{
		{name: "nil", cmds: nil, want: ""},
		{name: "one", cmds: []*packp.Command{cmd("refs/heads/a")}, want: ""},
		{
			name: "distinct",
			cmds: []*packp.Command{cmd("refs/heads/a"), cmd("refs/heads/b")},
			want: "",
		},
		{
			name: "adjacent",
			cmds: []*packp.Command{cmd("refs/heads/a"), cmd("refs/heads/a")},
			want: "refs/heads/a",
		},
		{
			name: "apart",
			cmds: []*packp.Command{cmd("refs/heads/a"), cmd("refs/heads/b"), cmd("refs/heads/a")},
			want: "refs/heads/a",
		},
		{
			name: "reports the second name repeated, not the first seen",
			cmds: []*packp.Command{cmd("refs/heads/a"), cmd("refs/heads/b"), cmd("refs/heads/b")},
			want: "refs/heads/b",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := duplicateRefname(tc.cmds)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, plumbing.ReferenceName(tc.want), got)
		})
	}
}

func storeReceiveObject(t *testing.T, st storage.Storer, content string) plumbing.Hash {
	t.Helper()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	require.NoError(t, err)
	_, err = io.WriteString(w, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	hash, err := st.SetEncodedObject(obj)
	require.NoError(t, err)
	return hash
}

func receiveExpectedOld(t *testing.T, st storage.Storer, cmd *packp.Command) (string, error) {
	t.Helper()
	var response bytes.Buffer
	err := ReceivePack(context.Background(), st, receivePackRequest(t, []*packp.Command{cmd}),
		ioutil.WriteNopCloser(&response), &ReceivePackRequest{StatelessRPC: true})
	return response.String(), err
}

func TestReceivePackChecksExpectedOld(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"update", "delete"} {
		for _, matches := range []bool{false, true} {
			name := action + "/mismatch"
			if matches {
				name = action + "/match"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				st := memory.NewStorage()
				current := storeReceiveObject(t, st, "current")
				old := storeReceiveObject(t, st, "stale")
				if matches {
					old = current
				}
				next := storeReceiveObject(t, st, "next")
				if action == "delete" {
					next = plumbing.ZeroHash
				}
				require.NoError(t, st.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/main"), current)))
				out, err := receiveExpectedOld(t, st, &packp.Command{Name: plumbing.ReferenceName("refs/tags/main"), Old: old, New: next})
				if !matches {
					assert.ErrorIs(t, err, storage.ErrReferenceHasChanged)
					assert.Contains(t, out, "ng refs/tags/main ")
					ref, err := st.Reference(plumbing.ReferenceName("refs/tags/main"))
					require.NoError(t, err)
					assert.Equal(t, current, ref.Hash())
					return
				}
				require.NoError(t, err)
				assert.Contains(t, out, "ok refs/tags/main\n")
				ref, err := st.Reference(plumbing.ReferenceName("refs/tags/main"))
				if action == "delete" {
					assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
				} else {
					require.NoError(t, err)
					assert.Equal(t, next, ref.Hash())
				}
			})
		}
	}
}

func TestReceivePackCreatePreservesDanglingSymbolicReference(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	alias := plumbing.NewSymbolicReference("refs/tags/alias", "refs/tags/missing")
	require.NoError(t, st.SetReference(alias))
	next := storeReceiveObject(t, st, "next")
	out, err := receiveExpectedOld(t, st, &packp.Command{Name: alias.Name(), Old: plumbing.ZeroHash, New: next})
	assert.ErrorIs(t, err, ErrUpdateReference)
	assert.Contains(t, out, "ng refs/tags/alias ")
	actual, err := st.Reference(alias.Name())
	require.NoError(t, err)
	assert.Equal(t, alias, actual)
	_, err = st.Reference(alias.Target())
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

type receiveConcurrentReferenceStorage struct {
	storage.Storer
	change func() error
	called bool
}

func (s *receiveConcurrentReferenceStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	s.called = true
	if err := s.change(); err != nil {
		return err
	}
	return s.Storer.CheckAndSetReference(ref, old)
}

func TestReceivePackUpdatePreservesConcurrentReferenceChange(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"change", "delete"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			base := memory.NewStorage()
			old := storeReceiveObject(t, base, "old")
			next := storeReceiveObject(t, base, "next")
			concurrent := storeReceiveObject(t, base, "concurrent")
			require.NoError(t, base.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/main"), old)))
			st := &receiveConcurrentReferenceStorage{Storer: base, change: func() error {
				if action == "delete" {
					return base.RemoveReference(plumbing.ReferenceName("refs/tags/main"))
				}
				return base.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/main"), concurrent))
			}}
			out, err := receiveExpectedOld(t, st, &packp.Command{Name: plumbing.ReferenceName("refs/tags/main"), Old: old, New: next})
			assert.True(t, st.called)
			require.Error(t, err)
			assert.Contains(t, out, "ng refs/tags/main ")
			ref, readErr := base.Reference(plumbing.ReferenceName("refs/tags/main"))
			if action == "delete" {
				assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
				assert.ErrorIs(t, readErr, plumbing.ErrReferenceNotFound)
			} else {
				assert.ErrorIs(t, err, storage.ErrReferenceHasChanged)
				require.NoError(t, readErr)
				assert.Equal(t, concurrent, ref.Hash())
			}
		})
	}
}

func TestReceivePackChangesTerminalSymbolicReference(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"update", "delete"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage()
			old := storeReceiveObject(t, st, "old")
			next := storeReceiveObject(t, st, "next")
			if action == "delete" {
				next = plumbing.ZeroHash
			}
			alias := plumbing.NewSymbolicReference("refs/tags/alias", plumbing.ReferenceName("refs/tags/main"))
			require.NoError(t, st.SetReference(alias))
			require.NoError(t, st.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/main"), old)))
			out, err := receiveExpectedOld(t, st, &packp.Command{Name: alias.Name(), Old: old, New: next})
			require.NoError(t, err)
			assert.Contains(t, out, "ok refs/tags/alias\n")
			actual, err := st.Reference(alias.Name())
			require.NoError(t, err)
			assert.Equal(t, alias, actual)
			terminal, err := st.Reference(plumbing.ReferenceName("refs/tags/main"))
			if action == "delete" {
				assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
			} else {
				require.NoError(t, err)
				assert.Equal(t, next, terminal.Hash())
			}
		})
	}
}

type receiveOldObjectErrorStorage struct {
	storage.Storer
	old    plumbing.Hash
	err    error
	called bool
}

func (s *receiveOldObjectErrorStorage) EncodedObject(typ plumbing.ObjectType, hash plumbing.Hash) (plumbing.EncodedObject, error) {
	if hash == s.old {
		s.called = true
		return nil, s.err
	}
	return s.Storer.EncodedObject(typ, hash)
}

func TestReceivePackDeleteOldObjectLookup(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		name := "read-error"
		if missing {
			name = "missing-object"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := memory.NewStorage()
			current := storeReceiveObject(t, base, "current")
			old := plumbing.NewHash(receivePackTestHash)
			require.NoError(t, base.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/main"), current)))
			lookupErr := errors.New("object read failed")
			if missing {
				lookupErr = plumbing.ErrObjectNotFound
			}
			st := &receiveOldObjectErrorStorage{Storer: base, old: old, err: lookupErr}
			out, err := receiveExpectedOld(t, st, deleteCmd(plumbing.ReferenceName("refs/tags/main"), old))
			assert.True(t, st.called)
			ref, readErr := base.Reference(plumbing.ReferenceName("refs/tags/main"))
			if missing {
				require.NoError(t, err)
				assert.Contains(t, out, "ok refs/tags/main\n")
				assert.ErrorIs(t, readErr, plumbing.ErrReferenceNotFound)
			} else {
				assert.ErrorIs(t, err, lookupErr)
				assert.Contains(t, out, "ng refs/tags/main object read failed\n")
				require.NoError(t, readErr)
				assert.Equal(t, current, ref.Hash())
			}
		})
	}
}

func TestReceivePackWithoutReportStatus(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"create", "update", "delete", "sideband-delete", "hook-rejection", "invalid-name"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage()
			name := plumbing.ReferenceName("refs/tags/main")
			old := plumbing.NewHash(receivePackTestHash)
			newHash := plumbing.NewHash("1111111111111111111111111111111111111111")
			if action == "create" {
				old = plumbing.ZeroHash
			} else {
				require.NoError(t, st.SetReference(plumbing.NewHashReference(name, old)))
			}
			if action == "delete" || action == "sideband-delete" || action == "hook-rejection" || action == "invalid-name" {
				newHash = plumbing.ZeroHash
			}
			commandName := name
			if action == "invalid-name" {
				commandName = "refs/tags/main.lock"
			}
			var request bytes.Buffer
			caps := ""
			if action == "sideband-delete" {
				caps = "side-band-64k"
			}
			_, err := pktline.Writef(&request, "%s %s %s\x00%s\n", old, newHash, commandName, caps)
			require.NoError(t, err)
			require.NoError(t, pktline.WriteFlush(&request))
			if !newHash.IsZero() {
				header := []byte("PACK\x00\x00\x00\x02\x00\x00\x00\x00")
				sum := sha1.Sum(header)
				request.Write(header)
				request.Write(sum[:])
			}
			preCalled, postCalled := false, false
			var applied []*packp.Command
			rejected := errors.New("policy rejected update")
			opts := &ReceivePackRequest{StatelessRPC: true, Hooks: ReceivePackHooks{
				PreReceive: func(context.Context, *PreReceiveInfo) error {
					preCalled = true
					if action == "hook-rejection" {
						return rejected
					}
					return nil
				},
				PostReceive: func(_ context.Context, info *PostReceiveInfo) error {
					postCalled = true
					applied = info.Commands
					return nil
				},
			}}
			response := &closeCountingWriter{}
			err = ReceivePack(context.Background(), st, io.NopCloser(&request), response, opts)
			assert.True(t, preCalled)
			assert.Equal(t, 1, response.closes)
			if action == "sideband-delete" {
				assert.Equal(t, "0000", response.buf.String())
			} else {
				assert.Empty(t, response.buf.String())
			}
			if action == "hook-rejection" || action == "invalid-name" {
				want := rejected
				if action == "invalid-name" {
					want = ErrFunnyRefname
				}
				assert.ErrorIs(t, err, want)
				assert.Empty(t, applied)
				ref, err := st.Reference(name)
				require.NoError(t, err)
				assert.Equal(t, old, ref.Hash())
				return
			}
			require.NoError(t, err)
			assert.True(t, postCalled)
			require.Len(t, applied, 1)
			ref, err := st.Reference(name)
			if action == "delete" || action == "sideband-delete" {
				assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
			} else {
				require.NoError(t, err)
				assert.Equal(t, newHash, ref.Hash())
			}
		})
	}
}

func TestReceivePackRejectsCompleteWhitespaceRefname(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash(receivePackTestHash)
	alias := plumbing.ReferenceName("refs/heads/main")
	for _, name := range []string{
		"refs/heads/main extra", "refs/heads/main\textra", "refs/heads/main\nextra",
		"refs/heads/main\rextra", "refs/heads/main\vextra", "refs/heads/main\fextra",
		"refs/heads/main ", "refs/heads/main\n", " refs/heads/main",
	} {
		for _, action := range []string{"create", "update", "delete"} {
			t.Run(action+"/"+name, func(t *testing.T) {
				t.Parallel()

				st := memory.NewStorage()
				oldHash, newHash := plumbing.ZeroHash, hash
				if action != "create" {
					require.NoError(t, st.SetReference(plumbing.NewHashReference(alias, hash)))
					oldHash = hash
				}
				if action == "delete" {
					newHash = plumbing.ZeroHash
				}

				var request, response bytes.Buffer
				_, err := pktline.Writef(&request, "%s %s %s\x00report-status\n", oldHash, newHash, name)
				require.NoError(t, err)
				require.NoError(t, pktline.WriteFlush(&request))
				if action != "delete" {
					header := []byte("PACK\x00\x00\x00\x02\x00\x00\x00\x00")
					sum := sha1.Sum(header)
					request.Write(header)
					request.Write(sum[:])
				}

				err = ReceivePack(context.Background(), st, io.NopCloser(&request),
					ioutil.WriteNopCloser(&response), &ReceivePackRequest{StatelessRPC: true})
				assert.ErrorIs(t, err, ErrFunnyRefname)
				assert.Contains(t, response.String(), "ng "+name+" funny refname\n")
				ref, err := st.Reference(alias)
				if action == "create" {
					assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
				} else {
					require.NoError(t, err)
					assert.Equal(t, hash, ref.Hash())
				}
			})
		}
	}
}
