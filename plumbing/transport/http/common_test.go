package http

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestClientSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ClientSuite))
}

type ClientSuite struct {
	suite.Suite
	Endpoint  *transport.Endpoint
	Storer    storage.Storer
	EmptyAuth transport.AuthMethod
}

func (s *ClientSuite) SetupSuite() {
	var err error
	s.Endpoint, err = transport.NewEndpoint(
		"https://github.com/git-fixtures/basic",
	)
	s.Nil(err)
	dot := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
}

func (s *UploadPackSuite) TestNewClient() {
	roundTripper := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cl := &http.Client{Transport: roundTripper}
	opts := &TransportOptions{
		Client: cl,
	}
	r, ok := NewTransport(opts).(*client)
	s.Equal(true, ok)
	s.Equal(cl, r.client)
}

func (s *ClientSuite) TestNewBasicAuth() {
	a := &BasicAuth{"foo", "qux"}

	s.Equal("http-basic-auth", a.Name())
	s.Equal("http-basic-auth - foo:*******", a.String())
}

func (s *ClientSuite) TestNewTokenAuth() {
	a := &TokenAuth{"OAUTH-TOKEN-TEXT"}

	s.Equal("http-token-auth", a.Name())
	s.Equal("http-token-auth - *******", a.String())

	// Check header is set correctly
	req, err := http.NewRequest("GET", "https://github.com/git-fixtures/basic", nil)
	s.NoError(err)
	a.SetAuth(req)
	s.Equal("Bearer OAUTH-TOKEN-TEXT", req.Header.Get("Authorization"))
}

func (s *ClientSuite) TestNewErrUnauthorized() {
	s.testNewHTTPError(http.StatusUnauthorized, ".*authentication required.*")
}

func (s *ClientSuite) TestNewErrForbidden() {
	s.testNewHTTPError(http.StatusForbidden, ".*authorization failed.*")
}

func (s *ClientSuite) TestNewErrNotFound() {
	s.testNewHTTPError(http.StatusNotFound, ".*repository not found.*")
}

func (s *ClientSuite) TestNewHTTPError40x() {
	s.testNewHTTPError(http.StatusPaymentRequired,
		"unexpected client error.*")
}

func (s *ClientSuite) Test_newSession() {
	cl := NewTransport(&TransportOptions{
		CacheMaxEntries: 2,
	}).(*client)

	insecureEP := s.Endpoint
	insecureEP.InsecureSkipTLS = true
	session, err := newSession(s.Storer, cl, insecureEP, nil, false)
	s.NoError(err)

	sessionTransport := session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
	t, ok := cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
	})
	// transport should be cached.
	s.True(ok)
	// cached transport should be the one that's used.
	s.Equal(sessionTransport, t)

	caEndpoint := insecureEP
	caEndpoint.CaBundle = []byte("this is the way")
	session, err = newSession(s.Storer, cl, caEndpoint, nil, false)
	s.NoError(err)

	sessionTransport = session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
	s.NotNil(sessionTransport.TLSClientConfig.RootCAs)
	t, ok = cl.fetchTransport(transportOptions{
		insecureSkipTLS: true,
		caBundle:        "this is the way",
	})
	// transport should be cached.
	s.True(ok)
	// cached transport should be the one that's used.
	s.Equal(sessionTransport, t)

	session, err = newSession(s.Storer, cl, caEndpoint, nil, false)
	s.NoError(err)
	sessionTransport = session.client.Transport.(*http.Transport)
	// transport that's going to be used should be cached already.
	s.Equal(sessionTransport, t)
	// no new transport got cached.
	s.Equal(2, cl.transports.Len())

	// if the cache does not exist, the transport should still be correctly configured.
	cl.transports = nil
	session, err = newSession(s.Storer, cl, insecureEP, nil, false)
	s.NoError(err)

	sessionTransport = session.client.Transport.(*http.Transport)
	s.True(sessionTransport.TLSClientConfig.InsecureSkipVerify)
}

func (s *ClientSuite) testNewHTTPError(code int, msg string) {
	req, _ := http.NewRequest("GET", "foo", nil)
	err := &Err{Status: code, URL: req.URL, Reason: msg}
	s.Regexp(msg, err.Error())
}

func (s *ClientSuite) TestSetAuth() {
	auth := &BasicAuth{}
	_, err := DefaultTransport.NewSession(s.Storer, s.Endpoint, auth)
	s.NoError(err)
}

func (s *ClientSuite) TestCheckError() {
	for code := http.StatusOK; code < http.StatusMultipleChoices; code++ {
		s.Run(fmt.Sprintf("HTTP Status: %d", code), func() {
			s.NoError(checkError(&http.Response{StatusCode: code}))
		})
	}

	statusCodesTests := []struct {
		code      int
		errType   error
		isWrapped bool
	}{
		{
			http.StatusUnauthorized,
			transport.ErrAuthenticationRequired,
			true,
		},
		{
			http.StatusForbidden,
			transport.ErrAuthorizationFailed,
			true,
		},
		{
			http.StatusNotFound,
			transport.ErrRepositoryNotFound,
			true,
		},
		{
			-1, // Unexpected status code
			&Err{},
			false,
		},
	}

	reason := "some reason for failing"

	for _, test := range statusCodesTests {
		s.Run(fmt.Sprintf("HTTP Error Status: %d", test.code), func() {
			req, _ := http.NewRequest("GET", "foo", nil)
			res := &http.Response{
				Request:    req,
				StatusCode: test.code,
				Body:       io.NopCloser(strings.NewReader(reason)),
			}
			err := checkError(res)
			s.Error(err)

			if test.isWrapped {
				s.Equal(errors.Is(err, test.errType), true)
			}

			var httpErr *Err
			s.Equal(errors.As(err, &httpErr), true)

			s.Equal(test.code, httpErr.Status)
			s.Equal(req.URL, httpErr.URL)
			s.Equal(reason, httpErr.Reason)
		})
	}
}

type mockAuth struct{}

func (*mockAuth) Name() string   { return "" }
func (*mockAuth) String() string { return "" }

func (s *ClientSuite) TestSetAuthWrongType() {
	_, err := DefaultTransport.NewSession(s.Storer, s.Endpoint, &mockAuth{})
	s.Equal(transport.ErrInvalidAuthMethod, err)
}

func newEndpoint(t testing.TB, port int, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", port, name))
	require.NoError(t, err)

	return ep
}

func setupServer(t testing.TB, smart bool) (base string, port int) {
	l := test.ListenTCP(t)

	port = l.Addr().(*net.TCPAddr).Port
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-%d", port))

	require.NoError(t, os.MkdirAll(base, 0o755))

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)

	var server *http.Server
	if smart {
		server = &http.Server{
			// TODO: Implement a go-git middleware and use it here.
			Handler: &cgi.Handler{
				Path: filepath.Join(strings.Trim(string(out), "\n"), "git-http-backend"),
				Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", base)},
			},
		}
	} else {
		server = &http.Server{
			Handler: http.FileServer(http.Dir(base)),
		}
	}

	done := make(chan struct{})

	go func() {
		defer func() { close(done) }()
		require.ErrorIs(t, server.Serve(l), http.ErrServerClosed)
	}()

	t.Cleanup(func() {
		require.NoError(t, server.Close())
		<-done
	})

	return base, port
}

func TestFilterHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    http.Header
		expected http.Header
	}{
		{
			name:     "empty headers",
			input:    http.Header{},
			expected: http.Header{},
		},
		{
			name: "only safe headers",
			input: http.Header{
				"User-Agent":   []string{"go-git/1.0"},
				"Content-Type": []string{"application/json"},
				"Git-Protocol": []string{"version=2"},
			},
			expected: http.Header{
				"User-Agent":   []string{"go-git/1.0"},
				"Content-Type": []string{"application/json"},
				"Git-Protocol": []string{"version=2"},
			},
		},
		{
			name: "only sensitive headers",
			input: http.Header{
				"Authorization":       []string{"Bearer secret-token"},
				"Cookie":              []string{"session=abc123"},
				"X-Auth-Token":        []string{"secret"},
				"Proxy-Authorization": []string{"Basic creds"},
			},
			expected: http.Header{},
		},
		{
			name: "mixed headers",
			input: http.Header{
				"User-Agent":        []string{"go-git/1.0"},
				"Authorization":     []string{"Bearer secret-token"},
				"Content-Type":      []string{"application/x-git-upload-pack-request"},
				"Cookie":            []string{"session=abc123"},
				"Git-Protocol":      []string{"version=2"},
				"Content-Length":    []string{"1234"},
				"Transfer-Encoding": []string{"chunked"},
			},
			expected: http.Header{
				"User-Agent":        []string{"go-git/1.0"},
				"Content-Type":      []string{"application/x-git-upload-pack-request"},
				"Git-Protocol":      []string{"version=2"},
				"Content-Length":    []string{"1234"},
				"Transfer-Encoding": []string{"chunked"},
			},
		},
		{
			name: "case insensitive matching",
			input: http.Header{
				"user-agent":    []string{"go-git/1.0"},
				"CONTENT-TYPE":  []string{"application/json"},
				"authorization": []string{"Bearer secret"},
			},
			expected: http.Header{
				"user-agent":   []string{"go-git/1.0"},
				"CONTENT-TYPE": []string{"application/json"},
			},
		},
		{
			name: "all safe headers",
			input: http.Header{
				"User-Agent":        []string{"go-git/1.0"},
				"Host":              []string{"github.com"},
				"Accept":            []string{"application/x-git-upload-pack-result"},
				"Content-Type":      []string{"application/x-git-upload-pack-request"},
				"Content-Length":    []string{"1234"},
				"Cache-Control":     []string{"no-cache"},
				"Git-Protocol":      []string{"version=2"},
				"Transfer-Encoding": []string{"chunked"},
				"Content-Encoding":  []string{"gzip"},
			},
			expected: http.Header{
				"User-Agent":        []string{"go-git/1.0"},
				"Host":              []string{"github.com"},
				"Accept":            []string{"application/x-git-upload-pack-result"},
				"Content-Type":      []string{"application/x-git-upload-pack-request"},
				"Content-Length":    []string{"1234"},
				"Cache-Control":     []string{"no-cache"},
				"Git-Protocol":      []string{"version=2"},
				"Transfer-Encoding": []string{"chunked"},
				"Content-Encoding":  []string{"gzip"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := filterHeaders(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactedURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    *url.URL
		expected string
	}{
		{
			name:     "nil URL",
			input:    nil,
			expected: "",
		},
		{
			name: "URL without userinfo",
			input: &url.URL{
				Scheme: "https",
				Host:   "github.com",
				Path:   "/go-git/go-git",
			},
			expected: "https://github.com/go-git/go-git",
		},
		{
			name: "URL with username only",
			input: &url.URL{
				Scheme: "https",
				User:   url.User("git"),
				Host:   "github.com",
				Path:   "/go-git/go-git",
			},
			expected: "https://git@github.com/go-git/go-git",
		},
		{
			name: "URL with username and password",
			input: &url.URL{
				Scheme: "https",
				User:   url.UserPassword("git", "secret-password"),
				Host:   "github.com",
				Path:   "/go-git/go-git",
			},
			expected: "https://git:REDACTED@github.com/go-git/go-git",
		},
		{
			name: "URL with empty password",
			input: &url.URL{
				Scheme: "https",
				User:   url.UserPassword("git", ""),
				Host:   "github.com",
				Path:   "/go-git/go-git",
			},
			expected: "https://git:REDACTED@github.com/go-git/go-git",
		},
		{
			name: "URL with query parameters",
			input: &url.URL{
				Scheme:   "https",
				User:     url.UserPassword("user", "pass"),
				Host:     "github.com",
				Path:     "/repo",
				RawQuery: "service=git-upload-pack",
			},
			expected: "https://user:REDACTED@github.com/repo?service=git-upload-pack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := redactedURL(tt.input)
			require.Equal(t, tt.expected, result)

			// Verify original URL is not modified
			if tt.input != nil && tt.input.User != nil {
				if origPass, hasPass := tt.input.User.Password(); hasPass {
					// Original password should still be intact
					require.NotEqual(t, "REDACTED", origPass, "original URL should not be modified")
				}
			}
		})
	}
}
