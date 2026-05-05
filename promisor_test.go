package git

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

// mockObjectFetcher records fetch calls and injects objects into the storer.
type mockObjectFetcher struct {
	calls   int
	objects map[plumbing.Hash]plumbing.EncodedObject
	storer  *memory.Storage
}

func (f *mockObjectFetcher) FetchObjects(_ context.Context, hashes []plumbing.Hash) error {
	f.calls++
	for _, h := range hashes {
		obj, ok := f.objects[h]
		if !ok {
			return plumbing.ErrObjectNotFound
		}
		if _, err := f.storer.SetEncodedObject(obj); err != nil {
			return err
		}
	}
	return nil
}

func makeObject(sto *memory.Storage, typ plumbing.ObjectType, content string) (plumbing.Hash, plumbing.EncodedObject) {
	obj := sto.NewEncodedObject()
	obj.SetType(typ)
	obj.SetSize(int64(len(content)))
	w, _ := obj.Writer()
	_, _ = w.Write([]byte(content))
	_ = w.Close()
	h, _ := sto.SetEncodedObject(obj)
	return h, obj
}

func makeBlob(sto *memory.Storage, content string) (plumbing.Hash, plumbing.EncodedObject) {
	return makeObject(sto, plumbing.BlobObject, content)
}

func TestPromiserStorer_EncodedObject_Found(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	h, _ := makeBlob(sto, "found")

	ps := newPromiserStorer(sto, &mockObjectFetcher{storer: sto})
	obj, err := ps.EncodedObject(plumbing.BlobObject, h)
	require.NoError(t, err)
	assert.Equal(t, h, obj.Hash())
}

func TestPromiserStorer_EncodedObject_FetchOnMiss(t *testing.T) {
	t.Parallel()

	// Create a blob in a temporary storage to get its hash and object.
	tmpSto := memory.NewStorage()
	h, blob := makeBlob(tmpSto, "lazy-blob")

	// The actual storer starts empty.
	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: blob},
	}

	ps := newPromiserStorer(sto, fetcher)
	obj, err := ps.EncodedObject(plumbing.BlobObject, h)
	require.NoError(t, err)
	assert.Equal(t, h, obj.Hash())
	assert.Equal(t, 1, fetcher.calls)
}

func TestPromiserStorer_EncodedObject_TreeFetchOnMiss(t *testing.T) {
	t.Parallel()

	tmpSto := memory.NewStorage()
	h, tree := makeObject(tmpSto, plumbing.TreeObject, "")

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: tree},
	}

	ps := newPromiserStorer(sto, fetcher)
	obj, err := ps.EncodedObject(plumbing.TreeObject, h)
	require.NoError(t, err)
	assert.Equal(t, h, obj.Hash())
	assert.Equal(t, 1, fetcher.calls)
}

func TestPromiserStorer_EncodedObject_CommitFetchOnMiss(t *testing.T) {
	t.Parallel()

	tmpSto := memory.NewStorage()
	h, commit := makeObject(tmpSto, plumbing.CommitObject, "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\nmsg\n")

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: commit},
	}

	ps := newPromiserStorer(sto, fetcher)
	obj, err := ps.EncodedObject(plumbing.CommitObject, h)
	require.NoError(t, err)
	assert.Equal(t, h, obj.Hash())
	assert.Equal(t, 1, fetcher.calls)
}

func TestPromiserStorer_EncodedObject_FetchFails(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	// Fetcher has no objects, so fetch will fail.
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{},
	}

	ps := newPromiserStorer(sto, fetcher)
	_, err := ps.EncodedObject(plumbing.BlobObject, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
	assert.Equal(t, 1, fetcher.calls)
}

func TestPromiserStorer_HasEncodedObject_FetchOnMiss(t *testing.T) {
	t.Parallel()

	tmpSto := memory.NewStorage()
	h, blob := makeBlob(tmpSto, "has-test")

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: blob},
	}

	ps := newPromiserStorer(sto, fetcher)
	err := ps.HasEncodedObject(h)
	require.NoError(t, err)
	assert.Equal(t, 1, fetcher.calls)
}

func TestPromiserStorer_EncodedObjectSize_FetchOnMiss(t *testing.T) {
	t.Parallel()

	tmpSto := memory.NewStorage()
	h, blob := makeBlob(tmpSto, "size-test")

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: blob},
	}

	ps := newPromiserStorer(sto, fetcher)
	sz, err := ps.EncodedObjectSize(h)
	require.NoError(t, err)
	assert.Equal(t, int64(len("size-test")), sz)
	assert.Equal(t, 1, fetcher.calls)
}

func TestFindPromisorRemote_NewStyle(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name:     "origin",
		URLs:     []string{"https://example.com/repo.git"},
		Promisor: true,
	}

	assert.Equal(t, "origin", findPromisorRemote(cfg))
}

func TestFindPromisorRemote_Legacy(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Extensions.PartialClone = "upstream"
	cfg.Remotes["upstream"] = &config.RemoteConfig{
		Name: "upstream",
		URLs: []string{"https://example.com/repo.git"},
	}

	assert.Equal(t, "upstream", findPromisorRemote(cfg))
}

func TestFindPromisorRemote_None(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://example.com/repo.git"},
	}

	assert.Equal(t, "", findPromisorRemote(cfg))
}

func TestFindPromisorRemote_NewStyleOverLegacy(t *testing.T) {
	t.Parallel()

	cfg := config.NewConfig()
	cfg.Extensions.PartialClone = "legacy-remote"
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name:     "origin",
		URLs:     []string{"https://example.com/repo.git"},
		Promisor: true,
	}
	cfg.Remotes["legacy-remote"] = &config.RemoteConfig{
		Name: "legacy-remote",
		URLs: []string{"https://example.com/legacy.git"},
	}

	// New style (remote.*.promisor) should take priority over legacy.
	assert.Equal(t, "origin", findPromisorRemote(cfg))
}

func TestWrapStorerIfPromisor(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	r, err := Init(sto, nil)
	require.NoError(t, err)

	// No promisor remote: storer should not be wrapped.
	wrapped := wrapStorerIfPromisor(sto, nil)
	assert.Equal(t, sto, wrapped)

	// Add a promisor remote.
	_, err = r.CreateRemote(&config.RemoteConfig{
		Name:     "origin",
		URLs:     []string{"https://example.com/repo.git"},
		Promisor: true,
	})
	require.NoError(t, err)

	wrapped = wrapStorerIfPromisor(sto, nil)
	_, ok := wrapped.(*promiserStorer)
	assert.True(t, ok, "storer should be wrapped as promiserStorer")
}

func TestOpenWrapsStorerIfPromisor(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	r, err := Init(sto, nil)
	require.NoError(t, err)

	_, err = r.CreateRemote(&config.RemoteConfig{
		Name:     "origin",
		URLs:     []string{"https://example.com/repo.git"},
		Promisor: true,
	})
	require.NoError(t, err)

	opened, err := Open(sto, nil)
	require.NoError(t, err)

	_, ok := getPromiserStorer(opened.Storer)
	assert.True(t, ok, "Open should wrap promisor repositories regardless of storage source")
}

func TestPromiserStorer_PackfileWriter_NotSupported(t *testing.T) {
	t.Parallel()

	// memory.Storage does not implement PackfileWriter, so the
	// wrapper should not expose it either.
	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{storer: sto, objects: map[plumbing.Hash]plumbing.EncodedObject{}}
	s := newPromiserStorer(sto, fetcher)

	_, ok := s.(storer.PackfileWriter)
	assert.False(t, ok, "promiserStorer wrapping memory.Storage should NOT implement storer.PackfileWriter")
}

func TestPromiserStorerPW_PackfileWriter(t *testing.T) {
	t.Parallel()

	// Use filesystem.Storage as inner which implements PackfileWriter.
	dir := t.TempDir()
	fs := osfs.New(dir)
	fsSto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	fetcher := &mockObjectFetcher{storer: memory.NewStorage(), objects: map[plumbing.Hash]plumbing.EncodedObject{}}
	s := newPromiserStorer(fsSto, fetcher)

	pw, ok := s.(storer.PackfileWriter)
	assert.True(t, ok, "promiserStorerPW wrapping filesystem.Storage should implement storer.PackfileWriter")

	if ok {
		wc, err := pw.PackfileWriter()
		require.NoError(t, err)
		assert.NotNil(t, wc)
		_ = wc.Close()
	}
}

func TestGetPromiserStorer(t *testing.T) {
	t.Parallel()

	// promiserStorer (memory inner)
	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{storer: sto, objects: map[plumbing.Hash]plumbing.EncodedObject{}}
	s := newPromiserStorer(sto, fetcher)
	ps, ok := getPromiserStorer(s)
	assert.True(t, ok)
	assert.NotNil(t, ps)

	// promiserStorerPW (filesystem inner)
	dir := t.TempDir()
	fs := osfs.New(dir)
	fsSto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	s2 := newPromiserStorer(fsSto, fetcher)
	ps2, ok2 := getPromiserStorer(s2)
	assert.True(t, ok2)
	assert.NotNil(t, ps2)

	// plain storer (not wrapped)
	_, ok3 := getPromiserStorer(sto)
	assert.False(t, ok3)
}

type fetchNegotiationTransport struct {
	session *fetchNegotiationSession
}

func (t *fetchNegotiationTransport) Handshake(context.Context, *transport.Request) (transport.Session, error) {
	return t.session, nil
}

type fetchNegotiationSession struct {
	refs       []*plumbing.Reference
	objects    map[plumbing.Hash]plumbing.EncodedObject
	fetchCalls int
	wants      []plumbing.Hash
}

func (s *fetchNegotiationSession) Capabilities() *capability.List {
	return capability.NewList()
}

func (s *fetchNegotiationSession) GetRemoteRefs(context.Context) ([]*plumbing.Reference, error) {
	return s.refs, nil
}

func (s *fetchNegotiationSession) Fetch(_ context.Context, st storage.Storer, req *transport.FetchRequest) error {
	s.fetchCalls++
	s.wants = append([]plumbing.Hash(nil), req.Wants...)
	for _, h := range req.Wants {
		obj, ok := s.objects[h]
		if !ok {
			return plumbing.ErrObjectNotFound
		}
		if _, err := st.SetEncodedObject(obj); err != nil {
			return err
		}
	}
	return nil
}

func (s *fetchNegotiationSession) Push(context.Context, storage.Storer, *transport.PushRequest) error {
	return nil
}

func (s *fetchNegotiationSession) Close() error {
	return nil
}

func TestRemoteFetchDoesNotLazyFetchWhenComputingWants(t *testing.T) {
	t.Parallel()

	tmpSto := memory.NewStorage()
	h, commit := makeObject(tmpSto, plumbing.CommitObject, "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\nmsg\n")

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{
		storer:  sto,
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: commit},
	}
	wrapped := newPromiserStorer(sto, fetcher)

	session := &fetchNegotiationSession{
		refs: []*plumbing.Reference{
			plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), h),
		},
		objects: map[plumbing.Hash]plumbing.EncodedObject{h: commit},
	}
	remote := NewRemote(wrapped, &config.RemoteConfig{
		Name: "origin",
		URLs: []string{"promisor-test://repo"},
	})

	err := remote.FetchContext(context.Background(), &FetchOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/main:refs/remotes/origin/main"),
		},
		ClientOptions: []client.Option{
			client.WithTransport("promisor-test", &fetchNegotiationTransport{session: session}),
		},
	})
	require.NoError(t, err)

	assert.Equal(t, 0, fetcher.calls)
	assert.Equal(t, 1, session.fetchCalls)
	assert.Equal(t, []plumbing.Hash{h}, session.wants)
}

func TestPromiserStorer_PackedObjectStorer(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{storer: sto, objects: map[plumbing.Hash]plumbing.EncodedObject{}}
	s := newPromiserStorer(sto, fetcher)

	pos, ok := s.(storer.PackedObjectStorer)
	assert.True(t, ok, "promiserStorer should implement storer.PackedObjectStorer")

	packs, err := pos.ObjectPacks()
	require.NoError(t, err)
	assert.Empty(t, packs)
}

func TestPromiserStorer_LooseObjectStorer(t *testing.T) {
	t.Parallel()

	sto := memory.NewStorage()
	fetcher := &mockObjectFetcher{storer: sto, objects: map[plumbing.Hash]plumbing.EncodedObject{}}
	s := newPromiserStorer(sto, fetcher)

	los, ok := s.(storer.LooseObjectStorer)
	assert.True(t, ok, "promiserStorer should implement storer.LooseObjectStorer")

	err := los.ForEachObjectHash(func(_ plumbing.Hash) error { return nil })
	assert.NoError(t, err)
}

func TestShouldFetch(t *testing.T) {
	t.Parallel()

	assert.True(t, shouldFetch(plumbing.BlobObject))
	assert.True(t, shouldFetch(plumbing.AnyObject))
	assert.True(t, shouldFetch(plumbing.TreeObject))
	assert.True(t, shouldFetch(plumbing.CommitObject))
	assert.True(t, shouldFetch(plumbing.TagObject))
	assert.False(t, shouldFetch(plumbing.OFSDeltaObject))
}

type markerWriteCloser struct {
	bytes.Buffer
	promisor bool
}

func (w *markerWriteCloser) Close() error { return nil }

func (w *markerWriteCloser) SetPromisor(v bool) {
	w.promisor = v
}

type markerPackfileStorage struct {
	*memory.Storage
	writer *markerWriteCloser
}

func (s *markerPackfileStorage) PackfileWriter() (io.WriteCloser, error) {
	return s.writer, nil
}

func TestWithPromisorPackfileWriterMarksWriter(t *testing.T) {
	t.Parallel()

	sto := &markerPackfileStorage{
		Storage: memory.NewStorage(),
		writer:  &markerWriteCloser{},
	}

	wrapped := withPromisorPackfileWriter(sto)
	pw, ok := wrapped.(storer.PackfileWriter)
	require.True(t, ok)

	w, err := pw.PackfileWriter()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	assert.True(t, sto.writer.promisor)
}

func TestFetchFilterFromConfig(t *testing.T) {
	t.Parallel()

	rc := &config.RemoteConfig{
		Name:               "origin",
		Promisor:           true,
		PartialCloneFilter: "blob:none",
	}

	assert.Equal(t, "blob:limit=1m", string(fetchFilterFromConfig(rc, "blob:limit=1m")))
	assert.Equal(t, "blob:none", string(fetchFilterFromConfig(rc, "")))
	assert.Equal(t, "", string(fetchFilterFromConfig(&config.RemoteConfig{Promisor: true}, "")))
	assert.Equal(t, "", string(fetchFilterFromConfig(&config.RemoteConfig{PartialCloneFilter: "blob:none"}, "")))
}
