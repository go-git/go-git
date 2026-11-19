package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

type loaderFunc func(*url.URL) (storage.Storer, error)

func (f loaderFunc) Load(u *url.URL) (storage.Storer, error) { return f(u) }

func TestServeDaemonServicePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		service string
		state   func(*Backend) *ServiceState
		set     ServiceState
		allowed bool
	}{
		{name: "upload-pack default", service: transport.UploadPackService, allowed: true},
		{name: "upload-pack enabled", service: transport.UploadPackService, state: uploadPackState, set: ServiceEnabled, allowed: true},
		{name: "upload-pack disabled", service: transport.UploadPackService, state: uploadPackState, set: ServiceDisabled},
		{name: "receive-pack default", service: transport.ReceivePackService},
		{name: "receive-pack enabled", service: transport.ReceivePackService, state: receivePackState, set: ServiceEnabled, allowed: true},
		{name: "receive-pack disabled", service: transport.ReceivePackService, state: receivePackState, set: ServiceDisabled},
		{name: "upload-archive default", service: transport.UploadArchiveService},
		{name: "upload-archive enabled", service: transport.UploadArchiveService, state: uploadArchiveState, set: ServiceEnabled, allowed: true},
		{name: "upload-archive disabled", service: transport.UploadArchiveService, state: uploadArchiveState, set: ServiceDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loaded := false
			b := New(loaderFunc(func(*url.URL) (storage.Storer, error) {
				loaded = true
				return memory.NewStorage(), nil
			}))
			if tc.state != nil {
				*tc.state(b) = tc.set
			}

			var w bytes.Buffer
			err := b.Serve(t.Context(), nil, ioutil.WriteNopCloser(&w), &Request{
				URL:           &url.URL{Path: "/repo"},
				Service:       tc.service,
				AdvertiseRefs: tc.service != transport.UploadArchiveService,
				StatelessRPC:  true,
			})
			if tc.allowed {
				require.NotErrorIs(t, err, ErrServiceNotEnabled)
				require.True(t, loaded)
				return
			}
			require.ErrorIs(t, err, ErrServiceNotEnabled)
			require.EqualError(t, err, "service not enabled: /repo")
			require.False(t, loaded, "a disabled service must not load the repository")
			require.Empty(t, w.Bytes())
		})
	}
}

func TestServeRejectsUnknownServiceBeforeLoad(t *testing.T) {
	t.Parallel()
	loaded := false
	b := &Backend{Loader: loaderFunc(func(*url.URL) (storage.Storer, error) {
		loaded = true
		return memory.NewStorage(), nil
	})}
	var w bytes.Buffer
	err := b.Serve(t.Context(), nil, ioutil.WriteNopCloser(&w), &Request{
		URL:     &url.URL{Path: "/repo"},
		Service: "git-unknown",
	})
	require.ErrorIs(t, err, transport.ErrUnsupportedService)
	require.False(t, loaded, "an unknown service must not load the repository")
	require.Empty(t, w.Bytes())
}

func TestServeDaemonPushRefusedByDefault(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	hash, err := st.SetEncodedObject(obj)
	require.NoError(t, err)
	ref := plumbing.NewHashReference("refs/heads/main", hash)
	require.NoError(t, st.SetReference(ref))

	push := func(b *Backend) error {
		var r, w bytes.Buffer
		req := &packp.UpdateRequests{Commands: []*packp.Command{{Name: ref.Name(), Old: ref.Hash()}}}
		require.NoError(t, req.Encode(&r))
		return b.Serve(t.Context(), io.NopCloser(&r), ioutil.WriteNopCloser(&w), &Request{
			URL:          &url.URL{Path: "/repo"},
			Service:      transport.ReceivePackService,
			StatelessRPC: true,
		})
	}

	b := New(transport.MapLoader{"/repo": st})
	require.ErrorIs(t, push(b), ErrServiceNotEnabled)
	_, err = st.Reference(ref.Name())
	require.NoError(t, err)

	b.ReceivePack = ServiceEnabled
	require.NoError(t, push(b))
	_, err = st.Reference(ref.Name())
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func uploadPackState(b *Backend) *ServiceState    { return &b.UploadPack }
func receivePackState(b *Backend) *ServiceState   { return &b.ReceivePack }
func uploadArchiveState(b *Backend) *ServiceState { return &b.UploadArchive }

func TestServeReceivePackHooks(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	hash, err := st.SetEncodedObject(obj)
	require.NoError(t, err)
	ref := plumbing.NewHashReference("refs/heads/main", hash)
	require.NoError(t, st.SetReference(ref))

	var pre []*packp.Command
	b := New(transport.MapLoader{"/repo": st})
	b.ReceivePack = ServiceEnabled
	b.ReceivePackHooks.PreReceive = func(_ context.Context, info *transport.PreReceiveInfo) error {
		pre = info.Commands
		return errors.New("protected branch")
	}
	b.ReceivePackHooks.PostReceive = func(context.Context, *transport.PostReceiveInfo) error {
		t.Error("PostReceive ran after PreReceive refused the push")
		return nil
	}

	var r, w bytes.Buffer
	req := &packp.UpdateRequests{Commands: []*packp.Command{{Name: ref.Name(), Old: ref.Hash()}}}
	req.Capabilities.Set(capability.ReportStatus)
	require.NoError(t, req.Encode(&r))
	err = b.Serve(t.Context(), io.NopCloser(&r), ioutil.WriteNopCloser(&w), &Request{
		URL:          &url.URL{Path: "/repo"},
		Service:      transport.ReceivePackService,
		StatelessRPC: true,
	})
	require.ErrorContains(t, err, "protected branch")

	require.Len(t, pre, 1)
	require.Equal(t, ref.Name(), pre[0].Name)
	var report packp.ReportStatus
	require.NoError(t, report.Decode(&w))
	require.ErrorContains(t, report.Error(), "protected branch")
	_, err = st.Reference(ref.Name())
	require.NoError(t, err)
}
