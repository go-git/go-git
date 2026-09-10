package backend

import (
	"bytes"
	"io"
	"net/http/httptest"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type callerStream struct {
	bytes.Buffer
	closed bool
}

func (s *callerStream) Close() error {
	s.closed = true
	return io.ErrClosedPipe
}

func (s *callerStream) Read(p []byte) (int, error) {
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.Buffer.Read(p)
}

func (s *callerStream) Write(p []byte) (int, error) {
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.Buffer.Write(p)
}

func TestServeKeepsCallerStreamsOpen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		service   string
		advertise bool
		malformed bool
		report    bool
		sideband  bool
	}{
		{name: "receive without report", service: transport.ReceivePackService},
		{name: "receive sideband without report", service: transport.ReceivePackService, sideband: true},
		{name: "receive report", service: transport.ReceivePackService, report: true},
		{name: "receive sideband report", service: transport.ReceivePackService, report: true, sideband: true},
		{name: "receive advertisement", service: transport.ReceivePackService, advertise: true},
		{name: "receive malformed", service: transport.ReceivePackService, malformed: true},
		{name: "upload advertisement", service: transport.UploadPackService, advertise: true},
		{name: "upload pack", service: transport.UploadPackService},
		{name: "upload malformed", service: transport.UploadPackService, malformed: true},
		{name: "archive formats", service: transport.UploadArchiveService},
		{name: "archive malformed", service: transport.UploadArchiveService, malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := memory.NewStorage()
			obj := st.NewEncodedObject()
			obj.SetType(plumbing.BlobObject)
			hash, err := st.SetEncodedObject(obj)
			require.NoError(t, err)
			ref := plumbing.NewHashReference("refs/heads/main", hash)
			require.NoError(t, st.SetReference(ref))
			r, w := &callerStream{}, &callerStream{}
			switch {
			case tc.malformed:
				_, err := r.Write([]byte("bad!"))
				require.NoError(t, err)
			case tc.advertise:
			case tc.service == transport.ReceivePackService:
				req := &packp.UpdateRequests{Commands: []*packp.Command{{Name: ref.Name(), Old: ref.Hash()}}}
				if tc.report {
					req.Capabilities.Set(capability.ReportStatus)
				}
				if tc.sideband {
					req.Capabilities.Set(capability.Sideband64k)
				}
				require.NoError(t, req.Encode(r))
			case tc.service == transport.UploadPackService:
				req := &packp.UploadRequest{Wants: []plumbing.Hash{hash}}
				require.NoError(t, req.Encode(r))
				_, err := pktline.WriteString(r, "done\n")
				require.NoError(t, err)
			case tc.service == transport.UploadArchiveService:
				_, err := pktline.WriteString(r, "argument --list\n")
				require.NoError(t, err)
				require.NoError(t, pktline.WriteFlush(r))
			}

			b := New(transport.MapLoader{"/repo": st})
			err = b.Serve(t.Context(), r, w, &Request{
				URL:           &url.URL{Path: "/repo"},
				Service:       tc.service,
				AdvertiseRefs: tc.advertise,
				StatelessRPC:  true,
			})
			if tc.malformed {
				require.Error(t, err)
				require.NotErrorIs(t, err, io.ErrClosedPipe)
			} else {
				require.NoError(t, err)
			}
			require.False(t, r.closed, "Serve closed the caller's reader")
			require.False(t, w.closed, "Serve closed the caller's writer")

			switch {
			case tc.malformed:
			case tc.advertise:
				require.Contains(t, w.String(), ref.Name().String())
				require.True(t, bytes.HasSuffix(w.Bytes(), []byte("0000")))
			case tc.service == transport.ReceivePackService:
				_, err := st.Reference(ref.Name())
				require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
				switch {
				case tc.report:
					var response io.Reader = bytes.NewReader(w.Bytes())
					if tc.sideband {
						require.True(t, bytes.HasSuffix(w.Bytes(), []byte("0000")), "missing outer sideband flush")
						response = sideband.NewDemuxer(sideband.Sideband64k, response)
					}
					var report packp.ReportStatus
					require.NoError(t, report.Decode(response))
					require.NoError(t, report.Error())
					require.Len(t, report.CommandStatuses, 1)
					require.Equal(t, ref.Name(), report.CommandStatuses[0].ReferenceName)
				case tc.sideband:
					require.Equal(t, "0000", w.String())
				default:
					require.Empty(t, w.Bytes())
				}
			case tc.service == transport.UploadPackService:
				require.True(t, bytes.HasPrefix(w.Bytes(), []byte("0008NAK\n")))
				dst := memory.NewStorage()
				require.NoError(t, packfile.UpdateObjectStorage(dst, bytes.NewReader(w.Bytes()[8:])))
				require.NoError(t, dst.HasEncodedObject(hash))
			case tc.service == transport.UploadArchiveService:
				require.Contains(t, w.String(), "ACK\n")
				require.Contains(t, w.String(), "tar\n")
				require.True(t, bytes.HasSuffix(w.Bytes(), []byte("0000")))
			}

			r.Reset()
			_, err = r.Write([]byte("next request"))
			require.NoError(t, err)
			remaining, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, "next request", string(remaining))
			_, err = w.Write([]byte("caller response"))
			require.NoError(t, err)
		})
	}
}

func TestServeNilReceiveStreams(t *testing.T) {
	t.Parallel()
	b := New(transport.MapLoader{"/repo": memory.NewStorage()})
	req := &Request{URL: &url.URL{Path: "/repo"}, Service: transport.ReceivePackService, StatelessRPC: true}
	require.EqualError(t, b.Serve(t.Context(), nil, nil, req), "nil writer")
	w := &callerStream{}
	require.EqualError(t, b.Serve(t.Context(), nil, w, req), "nil reader")
	require.False(t, w.closed)
	req.AdvertiseRefs = true
	require.NoError(t, b.Serve(t.Context(), nil, w, req))
	require.False(t, w.closed)
}

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
