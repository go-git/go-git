package backend

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func newPolicyBackend(t testing.TB) *Backend {
	return newBackend(t, transport.MapLoader{"repo.git": memory.NewStorage()})
}

func infoRefs(service string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/repo.git/info/refs?service="+service, nil)
}

func rpc(service string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/repo.git/"+service, strings.NewReader("0000"))
	req.Header.Set("Content-Type", fmt.Sprintf("application/x-%s-request", service))
	return req
}

func serveHTTP(b *Backend, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	return w
}

func TestServeHTTPPushWithoutAuthorizationRefused(t *testing.T) {
	t.Parallel()
	for _, req := range []*http.Request{
		infoRefs(transport.ReceivePackService),
		rpc(transport.ReceivePackService),
	} {
		req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
		w := serveHTTP(newPolicyBackend(t), req)
		require.Equal(t, http.StatusForbidden, w.Code, req.URL)
	}
}

func TestServeHTTPServicePolicy(t *testing.T) {
	t.Parallel()
	errAuth := errors.New("ldap lookup secret-host failed")
	for _, tc := range []struct {
		name      string
		req       *http.Request
		configure func(*Backend)
		authorize func(*http.Request, string) error
		want      int
	}{
		{name: "upload-pack anonymous", req: infoRefs(transport.UploadPackService), want: http.StatusOK},
		{name: "upload-pack disabled", req: infoRefs(transport.UploadPackService), configure: func(b *Backend) { b.UploadPack = ServiceDisabled }, want: http.StatusForbidden},
		{name: "upload-pack unauthorized", req: infoRefs(transport.UploadPackService), authorize: func(*http.Request, string) error { return ErrUnauthorized }, want: http.StatusUnauthorized},
		{name: "receive-pack authorized", req: infoRefs(transport.ReceivePackService), authorize: func(*http.Request, string) error { return nil }, want: http.StatusOK},
		{name: "receive-pack enabled anonymous", req: infoRefs(transport.ReceivePackService), configure: func(b *Backend) { b.ReceivePack = ServiceEnabled }, want: http.StatusOK},
		{name: "receive-pack disabled authorized", req: infoRefs(transport.ReceivePackService), configure: func(b *Backend) { b.ReceivePack = ServiceDisabled }, authorize: func(*http.Request, string) error { return nil }, want: http.StatusForbidden},
		{name: "receive-pack unauthorized", req: rpc(transport.ReceivePackService), authorize: func(*http.Request, string) error { return ErrUnauthorized }, want: http.StatusUnauthorized},
		{name: "receive-pack forbidden", req: rpc(transport.ReceivePackService), authorize: func(*http.Request, string) error { return ErrForbidden }, want: http.StatusForbidden},
		{name: "receive-pack wrapped forbidden", req: rpc(transport.ReceivePackService), authorize: func(*http.Request, string) error { return fmt.Errorf("read-only user: %w", ErrForbidden) }, want: http.StatusForbidden},
		{name: "receive-pack authorize failure", req: rpc(transport.ReceivePackService), authorize: func(*http.Request, string) error { return errAuth }, want: http.StatusInternalServerError},
		{name: "upload-archive anonymous", req: rpc(transport.UploadArchiveService), want: http.StatusForbidden},
		{name: "upload-archive enabled anonymous", req: rpc(transport.UploadArchiveService), configure: func(b *Backend) { b.UploadArchive = ServiceEnabled }, want: http.StatusOK},
		{name: "upload-archive authorized", req: rpc(transport.UploadArchiveService), authorize: func(*http.Request, string) error { return nil }, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newPolicyBackend(t)
			if tc.configure != nil {
				tc.configure(b)
			}
			b.Authorize = tc.authorize
			w := serveHTTP(b, tc.req)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			if tc.want == http.StatusUnauthorized {
				require.NotEmpty(t, w.Header().Get("WWW-Authenticate"))
			} else {
				require.Empty(t, w.Header().Get("WWW-Authenticate"))
			}
			require.NotContains(t, w.Body.String(), errAuth.Error())
		})
	}
}

func TestServeHTTPAuthorizeReceivesService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		req     *http.Request
		service string
	}{
		{infoRefs(transport.UploadPackService), transport.UploadPackService},
		{infoRefs(transport.ReceivePackService), transport.ReceivePackService},
		{rpc(transport.UploadPackService), transport.UploadPackService},
		{rpc(transport.ReceivePackService), transport.ReceivePackService},
		{rpc(transport.UploadArchiveService), transport.UploadArchiveService},
	} {
		var got []string
		b := newPolicyBackend(t)
		b.Authorize = func(r *http.Request, service string) error {
			require.Same(t, tc.req, r)
			got = append(got, service)
			return ErrForbidden
		}
		w := serveHTTP(b, tc.req)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Equal(t, []string{tc.service}, got)
	}
}

func TestServeHTTPDisabledServiceSkipsAuthorize(t *testing.T) {
	t.Parallel()
	b := newPolicyBackend(t)
	b.ReceivePack = ServiceDisabled
	b.Authorize = func(*http.Request, string) error {
		t.Error("Authorize called for a disabled service")
		return nil
	}
	w := serveHTTP(b, rpc(transport.ReceivePackService))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeHTTPPrefixMatchesPathBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prefix string
		path   string
		want   int
	}{
		{prefix: "/git", path: "/git/repo.git/info/refs", want: http.StatusOK},
		{prefix: "/git/", path: "/git/repo.git/info/refs", want: http.StatusOK},
		{prefix: "/git", path: "/gitx/repo.git/info/refs", want: http.StatusNotFound},
		{prefix: "/git", path: "/repo.git/info/refs", want: http.StatusNotFound},
		{prefix: "/repo", path: "/repository.git/info/refs", want: http.StatusNotFound},
	} {
		b := newBackend(t, transport.MapLoader{
			"repo.git":   memory.NewStorage(),
			"x/repo.git": memory.NewStorage(),
			"sitory.git": memory.NewStorage(),
		})
		b.Prefix = tc.prefix
		w := serveHTTP(b, httptest.NewRequest(http.MethodGet, tc.path+"?service=git-upload-pack", nil))
		require.Equal(t, tc.want, w.Code, "prefix %q path %q", tc.prefix, tc.path)
	}
}
