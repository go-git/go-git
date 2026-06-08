package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func testServe[T UploadPackRequest | ReceivePackRequest](
	t testing.TB,
	st storage.Storer,
	fun func(
		ctx context.Context,
		st storage.Storer,
		r io.ReadCloser,
		w io.WriteCloser,
		opts *T,
	) error,
	r io.ReadCloser,
	opts *T,
) *bytes.Buffer {
	var out bytes.Buffer
	err := fun(
		context.TODO(),
		st,
		r,
		ioutil.WriteNopCloser(&out),
		opts,
	)
	require.NoError(t, err)
	require.Greater(t, out.Len(), 0)
	return &out
}

func testAdvertise[T UploadPackRequest | ReceivePackRequest](
	t testing.TB,
	fun func(
		ctx context.Context,
		st storage.Storer,
		r io.ReadCloser,
		w io.WriteCloser,
		opts *T,
	) error,
	proto string,
	stateless bool,
) *bytes.Buffer {
	dot, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir))
	if err != nil {
		t.Fatal(err)
	}
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	defer func() { _ = st.Close() }()
	opts := new(T)
	switch o := any(opts).(type) {
	case *UploadPackRequest:
		o.GitProtocol = proto
		o.AdvertiseRefs = true
		o.StatelessRPC = stateless
	case *ReceivePackRequest:
		o.GitProtocol = proto
		o.AdvertiseRefs = true
		o.StatelessRPC = stateless
	}
	return testServe(t, st, fun, io.NopCloser(bytes.NewBuffer(nil)), opts)
}
