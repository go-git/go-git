package http

import (
	"io"
	"net/http/httptest"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/stretchr/testify/require"
)

type fixturesLoader struct{
	testing.TB
}

var _ transport.Loader = &fixturesLoader{}

// Load implements transport.Loader.
func (f *fixturesLoader) Load(ep *transport.Endpoint) (storage.Storer, error) {
	url := "https://github.com/git-fixtures/" + ep.Path
	fix := fixtures.ByURL(url).One()
	require.NotNil(f.TB, fix, "fixture not found for %s", url)
	dot := fix.DotGit(fixtures.WithTargetDir(f.TempDir))
	st := filesystem.NewStorage(dot, nil)
	return st, nil
}

func TestNilLoaderHandler(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, 404, res.StatusCode)
}

func testInfoRefs(t testing.TB, isSmart bool) {
	expectedDumb := `6ecf0ef2c2dffb796033e5a02219af86ec6584e5	refs/heads/master
6ecf0ef2c2dffb796033e5a02219af86ec6584e5	refs/remotes/origin/HEAD
e8d3ffab552895c19b9fcf7aa264d277cde33881	refs/remotes/origin/branch
6ecf0ef2c2dffb796033e5a02219af86ec6584e5	refs/remotes/origin/master
`
	expectedSmart := `001e# service=git-upload-pack
000000b46ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD`+"\x00"+`agent=`+capability.DefaultAgent()+` ofs-delta side-band-64k multi_ack multi_ack_detailed side-band no-progress shallow symref=HEAD:refs/heads/master
003fe8d3ffab552895c19b9fcf7aa264d277cde33881 refs/heads/branch
003f6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master
00466ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/HEAD
0048e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch
00486ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master
003e6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/tags/v1.0.0
0000`
	h := &Handler{
		Loader: &fixturesLoader{t},
	}

	urlPath := "/basic.git/info/refs"
	if isSmart {
		urlPath += "?service=git-upload-pack"
	}
	req := httptest.NewRequest("GET", urlPath, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, 200, res.StatusCode)

	bts, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	if isSmart {
		require.Equal(t, expectedSmart, string(bts))
	} else {
		require.Equal(t, expectedDumb, string(bts))
	}
}

func TestDumbInfoRefs(t *testing.T) {
	testInfoRefs(t, false)
}

func TestSmartInfoRefs(t *testing.T) {
	testInfoRefs(t, true)
}
