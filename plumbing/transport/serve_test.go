package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

func testServe[T UploadPackOptions | ReceivePackOptions](
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

func testAdvertise[T UploadPackOptions | ReceivePackOptions](
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
	dot := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir))
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	return testServe(t, st, fun, io.NopCloser(bytes.NewBuffer(nil)), &T{
		GitProtocol:   proto,
		AdvertiseRefs: true,
		StatelessRPC:  stateless,
	})
}
