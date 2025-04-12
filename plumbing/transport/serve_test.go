package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/stretchr/testify/require"
)

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
	var out bytes.Buffer
	dot := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(t.TempDir))
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	err := fun(
		context.TODO(),
		st,
		io.NopCloser(bytes.NewBuffer(nil)),
		ioutil.WriteNopCloser(&out),
		&T{
			GitProtocol:   proto,
			AdvertiseRefs: true,
			StatelessRPC:  stateless,
		},
	)
	require.NoError(t, err)
	require.Greater(t, out.Len(), 0)
	return &out
}
