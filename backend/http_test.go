package backend

import (
	"io"
	"net/http/httptest"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type fixturesLoader struct {
	testing.TB
}

var _ transport.Loader = &fixturesLoader{}

// Load implements transport.Loader.
func (f *fixturesLoader) Load(ep *url.URL) (storage.Storer, error) {
	url := "https://github.com/git-fixtures/" + ep.Path
	fix := fixtures.ByURL(url).One()
	require.NotNil(f.TB, fix, "fixture not found for %s", url)
	dot, err := fix.DotGit(fixtures.WithTargetDir(f.TempDir))
	if err != nil {
		return nil, err
	}
	st := filesystem.NewStorage(dot, nil)
	// Any storage returned by Load in the application need to be closed by the caller,
	// do not add close here otherwise you will either double close, or hide a missing close
	return st, nil
}

func TestNilLoaderBackend(t *testing.T) {
	t.Parallel()
	h := New(nil)
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
000000c76ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD` + "\x00" + `agent=` + capability.DefaultAgent() + ` ofs-delta side-band-64k multi_ack multi_ack_detailed side-band no-progress shallow object-format=sha1 symref=HEAD:refs/heads/master
003fe8d3ffab552895c19b9fcf7aa264d277cde33881 refs/heads/branch
003f6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master
00466ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/HEAD
0048e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch
00486ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master
003e6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/tags/v1.0.0
0000`
	h := New(&fixturesLoader{t})

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
	t.Parallel()
	testInfoRefs(t, false)
}

func TestSmartInfoRefs(t *testing.T) {
	t.Parallel()
	testInfoRefs(t, true)
}

type tagLoader struct {
	testing.TB
	tag          string
	objectFormat string
}

var _ transport.Loader = &tagLoader{}

func (l *tagLoader) Load(_ *url.URL) (storage.Storer, error) {
	of := l.objectFormat
	if of == "" {
		of = "sha1"
	}

	fix := fixtures.ByTag(l.tag).ByObjectFormat(of).One()
	require.NotNil(l.TB, fix, "fixture not found for tag %s", l.tag)

	dot, err := fix.DotGit(fixtures.WithTargetDir(l.TempDir))
	if err != nil {
		return nil, err
	}
	st := filesystem.NewStorage(dot, nil)
	// Any storage returned by Load in the application need to be closed by the caller,
	// do not add close here otherwise you will either double close, or hide a missing close

	if l.objectFormat != "" {
		cfg, err := st.Config()
		require.NoError(l.TB, err)

		want := config.ObjectFormat(l.objectFormat)
		if want == config.SHA1 {
			want = config.UnsetObjectFormat
		}
		require.Equal(l.TB, want, cfg.Extensions.ObjectFormat)
	}

	return st, nil
}

func TestSmartInfoRefsObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tag               string
		wantObjectFormat  string
		forceObjectFormat string
	}{
		{
			name:             "sha1 (unset)",
			tag:              ".git",
			wantObjectFormat: "sha1",
		},
		{
			name:              "sha1",
			tag:               ".git",
			forceObjectFormat: "sha1",
			wantObjectFormat:  "sha1",
		},
		{
			name:              "sha256",
			tag:               ".git",
			forceObjectFormat: "sha256",
			wantObjectFormat:  "sha256",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := New(&tagLoader{TB: t, tag: tc.tag, objectFormat: tc.forceObjectFormat})

			req := httptest.NewRequest("GET", "/repo.git/info/refs?service=git-upload-pack", nil)
			w := httptest.NewRecorder()
			b.ServeHTTP(w, req)
			res := w.Result()
			require.Equal(t, 200, res.StatusCode)

			bts, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			require.NoError(t, res.Body.Close())

			expectedCap := "object-format=" + tc.wantObjectFormat
			require.Contains(t, string(bts), expectedCap,
				"expected object-format=%s capability in response", tc.wantObjectFormat)
		})
	}
}

func TestUnsupportedService(t *testing.T) {
	t.Parallel()

	h := New(&fixturesLoader{t})

	req := httptest.NewRequest("GET", "/basic.git/info/refs?service=invalid-service", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, 404, res.StatusCode)
}

func TestInvalidService(t *testing.T) {
	t.Parallel()

	h := New(&fixturesLoader{t})

	req := httptest.NewRequest("POST", "/basic.git/git-your-face", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, 404, res.StatusCode)
}
