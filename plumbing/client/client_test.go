package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v6/plumbing/transport"
	xhttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	xssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

func TestNew_BuiltinSchemes(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Close()

	for _, scheme := range []string{"file", "git", "http", "https", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q should be registered", scheme)
		assert.NotNil(t, tr, "transport for %q should not be nil", scheme)
	}
}

func TestNew_ConnectorSchemes(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Close()

	for _, scheme := range []string{"file", "git", "ssh"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connector)
		assert.True(t, ok, "scheme %q should implement Connector", scheme)
	}
}

func TestNew_HTTPNotConnector(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Close()

	for _, scheme := range []string{"http", "https"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err)
		_, ok := tr.(transport.Connector)
		assert.False(t, ok, "scheme %q should NOT implement Connector", scheme)
	}
}

func TestNew_UnsupportedScheme(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Close()

	_, err := c.Transport("ftp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")
}

func TestWithTransport(t *testing.T) {
	t.Parallel()

	custom := &mockTransport{}
	c := New(WithTransport("custom", custom))
	defer c.Close()

	tr, err := c.Transport("custom")
	require.NoError(t, err)
	assert.Equal(t, custom, tr)
}

func TestWithTransport_OverrideBuiltin(t *testing.T) {
	t.Parallel()

	custom := &mockTransport{}
	c := New(WithTransport("ssh", custom))
	defer c.Close()

	tr, err := c.Transport("ssh")
	require.NoError(t, err)
	assert.Equal(t, custom, tr)
}

func TestWithSSHAuth(t *testing.T) {
	t.Parallel()

	auth := &xssh.Password{
		User:     "git",
		Password: "secret",
		HostKeyCallbackHelper: xssh.HostKeyCallbackHelper{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		},
	}

	c := New(WithSSHAuth(auth))
	defer c.Close()

	tr, err := c.Transport("ssh")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

// WithHTTPAuth installs a credential source scoped to the repository's own
// origin, where it used to install an authorizer called for every request the
// transport made. Asserting that New() yields a transport proves nothing about
// it — New() with no options at all does that — so drive the source.
//
// What the source does with an origin belongs to transport/http and is tested
// there; what belongs here is that WithHTTPAuth wires up a scoped one rather
// than an unconditional one.
func TestWithHTTPAuth(t *testing.T) {
	t.Parallel()

	var o options
	WithHTTPAuth(&xhttp.BasicAuth{Username: "user", Password: "pass"})(&o)
	require.NotNil(t, o.http.Credentials, "the option must install a source")

	repo := &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}

	cred, err := o.http.Credentials(context.Background(), &CredentialRequest{
		TargetOrigin:  &url.URL{Scheme: "https", Host: "example.com"},
		RepositoryURL: repo,
	})
	require.NoError(t, err)
	require.NotNil(t, cred, "the repository's own origin must be answered for")

	cred, err = o.http.Credentials(context.Background(), &CredentialRequest{
		TargetOrigin:  &url.URL{Scheme: "https", Host: "evil.example"},
		RepositoryURL: repo,
		Redirected:    true,
	})
	require.NoError(t, err)
	assert.Nil(t, cred, "the credential must not follow a redirect to another origin")
}

func TestWithHTTPClient(t *testing.T) {
	t.Parallel()

	custom := &http.Client{}
	c := New(WithHTTPClient(custom))
	defer c.Close()

	tr, err := c.Transport("https")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

func TestWithRedirectPolicy(t *testing.T) {
	t.Parallel()

	var o options
	WithRedirectPolicy(NoFollowRedirects)(&o)

	assert.Equal(t, xhttp.NoFollowRedirects, o.http.FollowRedirects)
}

func TestWithProxyURL(t *testing.T) {
	t.Parallel()

	proxyURL, err := url.Parse("socks5://proxy.example:1080")
	require.NoError(t, err)

	c := New(WithProxyURL(proxyURL))
	defer c.Close()

	for _, scheme := range []string{"ssh", "git", "http", "https"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q", scheme)
		assert.NotNil(t, tr, "scheme %q", scheme)
	}
}

func TestWithProxyEnvironment(t *testing.T) {
	t.Parallel()

	c := New(WithProxyEnvironment())
	defer c.Close()

	for _, scheme := range []string{"ssh", "git", "http", "https"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q", scheme)
		assert.NotNil(t, tr, "scheme %q", scheme)
	}
}

func TestWithDialer(t *testing.T) {
	t.Parallel()

	c := New(WithDialer((&net.Dialer{}).DialContext))
	defer c.Close()

	for _, scheme := range []string{"ssh", "git"} {
		tr, err := c.Transport(scheme)
		require.NoError(t, err, "scheme %q", scheme)
		assert.NotNil(t, tr, "scheme %q", scheme)
	}
}

func TestWithLoader(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{}
	c := New(WithLoader(loader))
	defer c.Close()

	tr, err := c.Transport("file")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

func TestMultipleOptions(t *testing.T) {
	t.Parallel()

	auth := &xhttp.BasicAuth{Username: "u", Password: "p"}
	custom := &mockTransport{}

	c := New(
		WithHTTPAuth(auth),
		WithTransport("custom", custom),
	)
	defer c.Close()

	tr, err := c.Transport("custom")
	require.NoError(t, err)
	assert.Equal(t, custom, tr)

	tr, err = c.Transport("http")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

func TestNilRequest(t *testing.T) {
	t.Parallel()

	c := New()
	defer c.Close()

	_, err := c.Handshake(context.Background(), nil)
	require.Error(t, err)

	_, err = c.Connect(context.Background(), nil)
	require.Error(t, err)
}

func TestWithInsecureSkipTLS(t *testing.T) {
	t.Parallel()

	c := New(WithInsecureSkipTLS())
	defer c.Close()

	tr, err := c.Transport("https")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

func TestWithCABundle(t *testing.T) {
	t.Parallel()

	c := New(WithCABundle(testCAPEM))
	defer c.Close()

	tr, err := c.Transport("https")
	require.NoError(t, err)
	assert.NotNil(t, tr)
}

func TestWithInsecureSkipTLS_And_WithCABundle_Merge(t *testing.T) {
	t.Parallel()

	var o options
	WithInsecureSkipTLS()(&o)
	WithCABundle(testCAPEM)(&o)

	require.NotNil(t, o.http.TLS)
	assert.True(t, o.http.TLS.InsecureSkipVerify)
	assert.NotNil(t, o.http.TLS.RootCAs)
}

func TestWithInsecureSkipTLS_And_WithCABundle_ReverseOrder(t *testing.T) {
	t.Parallel()

	var o options
	WithCABundle(testCAPEM)(&o)
	WithInsecureSkipTLS()(&o)

	require.NotNil(t, o.http.TLS)
	assert.True(t, o.http.TLS.InsecureSkipVerify)
	assert.NotNil(t, o.http.TLS.RootCAs)
}

// Self-signed CA certificate for testing.
var testCAPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJALRiMLAh4HMHMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBnRl
c3RjYTAeFw0yNDA0MDQwMDAwMDBaFw0zNDA0MDIwMDAwMDBaMBExDzANBgNVBAMM
BnRlc3RjYTBcMA0GCSqGSIb3DQEBAQUAA0sAMEgCQQC7o96+IG5sKBe0QKbsBigc
GsR8cKQuDfhCFqzWn7zr4aqHsLQiKEJsClMDGnNHEFGDFpXuIFxnGOTPYFOYIuDH
AgMBAAGjUzBRMB0GA1UdDgQWBBQgTxe0MCRKYB0ILQM0L7V/lMjxNjAfBgNVHSME
GDAWgBQgTxe0MCRKYB0ILQM0L7V/lMjxNjAPBgNVHRMBAf8EBTADAQH/MA0GCSqG
SIb3DQEBCwUAA0EAh/8fnFa6VW1cB8QJWIM4KpCmpY9R1YMaqGCbDjM0FZmE+dqA
NsaKMCSE1YOIMBN6mBUX3iTmy/sCTIYMBbFPgQ==
-----END CERTIFICATE-----
`)

// header returns what the credential supplied for target puts on a request, or
// "" when no credential was supplied.
func header(t *testing.T, o options, target string) string {
	t.Helper()

	require.NotNil(t, o.http.Credentials)
	cred, err := o.http.Credentials(context.Background(), &CredentialRequest{
		TargetOrigin:  &url.URL{Scheme: "https", Host: target},
		RepositoryURL: &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
	})
	require.NoError(t, err)
	if cred == nil || cred.Authorizer == nil {
		return ""
	}

	req, err := http.NewRequest(http.MethodGet, "https://"+target+"/repo.git/info/refs", nil)
	require.NoError(t, err)
	require.NoError(t, cred.Authorizer(req))
	return req.Header.Get("Authorization")
}

// The two options once wrote the same field, so applying both silently
// discarded the first — the caller got one credential and no indication that
// the other had been dropped. A source that declines is not a source that
// overrides either: an earlier one is still reached, so overriding is per
// origin rather than wholesale replacement.
func TestWithHTTPAuthAndWithHTTPCredentialsCompose(t *testing.T) {
	t.Parallel()

	auth := &xhttp.BasicAuth{Username: "user", Password: "pass"}
	gateway := &url.URL{Scheme: "https", Host: "gateway.example"}
	forGateway := xhttp.ForOrigin(gateway, func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer gateway-token")
		return nil
	})

	// Basic dXNlcjpwYXNz is user:pass, what BasicAuth's Authorizer sets.
	const wantRepo = "Basic dXNlcjpwYXNz"

	t.Run("auth first", func(t *testing.T) {
		t.Parallel()

		var o options
		WithHTTPAuth(auth)(&o)
		WithHTTPCredentials(forGateway)(&o)

		assert.Equal(t, wantRepo, header(t, o, "example.com"),
			"the repository credential must survive the second option")
		assert.Equal(t, "Bearer gateway-token", header(t, o, "gateway.example"),
			"the gateway credential must be consulted too")
	})

	t.Run("credentials first", func(t *testing.T) {
		t.Parallel()

		var o options
		WithHTTPCredentials(forGateway)(&o)
		WithHTTPAuth(auth)(&o)

		assert.Equal(t, wantRepo, header(t, o, "example.com"))
		assert.Equal(t, "Bearer gateway-token", header(t, o, "gateway.example"))
	})
}

// Appending an option to a slice of defaults is how a caller overrides one, and
// it must work across the two credential options as it does within either:
// whichever was applied last answers for an origin they both answer for.
// Application order is the only ordering the caller can see.
func TestHTTPCredentialOptionsFollowApplicationOrder(t *testing.T) {
	t.Parallel()

	origin := &url.URL{Scheme: "https", Host: "example.com"}
	bearer := func(tok string) Option {
		return WithHTTPCredentials(xhttp.ForOrigin(origin, func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+tok)
			return nil
		}))
	}
	basic := func(user string) Option {
		return WithHTTPAuth(&xhttp.BasicAuth{Username: user, Password: user})
	}
	// Basic dXNlcjp1c2Vy is user:user, what basic("user") sets.
	const wantBasicUser = "Basic dXNlcjp1c2Vy"

	for _, tc := range []struct {
		name          string
		first, second Option
		want          string
	}{
		{"credentials over credentials", bearer("first"), bearer("second"), "Bearer second"},
		{"credentials over credentials, reversed", bearer("second"), bearer("first"), "Bearer first"},
		{"auth over credentials", bearer("first"), basic("user"), wantBasicUser},
		{"credentials over auth", basic("user"), bearer("second"), "Bearer second"},
		{"auth over auth", basic("default"), basic("user"), wantBasicUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var o options
			tc.first(&o)
			tc.second(&o)
			assert.Equal(t, tc.want, header(t, o, "example.com"))
		})
	}
}

// A source that fails stops the chain there: a store that errored has not
// declined, so falling through to an earlier source would answer with the
// credential the caller meant to override.
func TestHTTPCredentialSourceErrorStopsEarlierSources(t *testing.T) {
	t.Parallel()

	boom := errors.New("credential store unavailable")

	var reached bool
	var o options
	WithHTTPCredentials(func(context.Context, *CredentialRequest) (*Credential, error) {
		reached = true
		return &Credential{Authorizer: func(*http.Request) error { return nil }}, nil
	})(&o)
	WithHTTPCredentials(func(context.Context, *CredentialRequest) (*Credential, error) {
		return nil, boom
	})(&o)

	_, err := o.http.Credentials(context.Background(), &CredentialRequest{
		TargetOrigin:  &url.URL{Scheme: "https", Host: "example.com"},
		RepositoryURL: &url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
	})
	require.ErrorIs(t, err, boom)
	assert.False(t, reached, "an earlier source must not be consulted past a failure")
}

type mockTransport struct{}

func (m *mockTransport) Handshake(_ context.Context, _ *transport.Request) (transport.Session, error) {
	return nil, nil
}

// A credential source answers for the origin a request is about to be made to,
// and for no other: a redirect target is chosen by the server that issued the
// redirect, not by the caller. ForOrigin compares origins the way the transport
// does, so a source built with it cannot answer for an origin the transport
// would not have sent to — and declines everywhere else, which leaves a request
// unauthenticated rather than leaking.
func ExampleWithHTTPCredentials() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("server received:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		// A minimal protocol v2 capability advertisement.
		_, _ = fmt.Fprint(w, "001e# service=git-upload-pack\n0000000eversion 2\n0000")
	}))
	defer srv.Close()

	origin, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}

	c := New(WithHTTPCredentials(xhttp.ForOrigin(origin, func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer origin-token")
		return nil
	})))
	defer c.Close()

	repositoryURL, err := url.Parse(srv.URL + "/repo.git")
	if err != nil {
		panic(err)
	}
	session, err := c.Handshake(context.Background(), &transport.Request{
		URL:     repositoryURL,
		Command: transport.UploadPackService,
	})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Output:
	// server received: Bearer origin-token
}

// The point of the aliases is that a caller can write a credential source —
// the literal's signature, the request it reads, the credential it returns —
// without naming plumbing/transport/http at all. This compiles only while that
// holds, and it is the store idiom the CredentialsFunc documentation shows:
// a source spanning several forges has no single origin to name up front, so
// it asks each request which of the origins it holds the request is for.
//
// Which origins IsOrigin accepts is the transport's relation, enumerated
// against it in that package. What is checked here is that the relation is
// reachable through the client-package names and that the option accepts the
// result.
func TestCredentialsFuncWritableWithClientNamesOnly(t *testing.T) {
	t.Parallel()

	store := map[string]string{"https://example.com": "example-token"}
	var fn CredentialsFunc = func(_ context.Context, req *CredentialRequest) (*Credential, error) {
		for raw, tok := range store {
			held, err := url.Parse(raw)
			if err != nil {
				return nil, err
			}
			if !req.IsOrigin(held) {
				continue
			}
			return &Credential{Authorizer: func(r *http.Request) error {
				r.Header.Set("Authorization", "Bearer "+tok)
				return nil
			}}, nil
		}
		return nil, nil
	}

	require.NotNil(t, WithHTTPCredentials(fn), "the option must accept a source written this way")

	for _, tc := range []struct {
		target string
		want   string
	}{
		{"https://example.com", "Bearer example-token"},
		{"https://evilexample.com", ""},
	} {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			target, err := url.Parse(tc.target)
			require.NoError(t, err)
			cred, err := fn(context.Background(), &CredentialRequest{TargetOrigin: target})
			require.NoError(t, err)

			if tc.want == "" {
				assert.Nil(t, cred, "a suffix match would hand the token to this")
				return
			}
			require.NotNil(t, cred)
			r, err := http.NewRequest(http.MethodGet, tc.target+"/repo.git", nil)
			require.NoError(t, err)
			require.NoError(t, cred.Authorizer(r))
			assert.Equal(t, tc.want, r.Header.Get("Authorization"))
		})
	}
}

// WithHTTPAuth(nil) is "no HTTP authentication", which is what a Client without
// the option already is, so it must leave the options as it found them. Its
// neighbours already read that way: WithTransport ignores a nil transport, and
// WithHTTPCredentials(nil) adds a source that answers for nothing. This used to
// dereference the nil interface while New was applying it.
func TestWithHTTPAuthNilIsANoOp(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		c := New(WithHTTPAuth(nil))
		defer c.Close()
	}, "a nil auth must not take the Client down with it")

	var o options
	WithHTTPAuth(nil)(&o)
	assert.Nil(t, o.http.Credentials, "a nil auth must not install a credential source")

	// And it must not displace a source already configured: nothing to add is
	// not the same as something that declines.
	var kept options
	WithHTTPAuth(&xhttp.BasicAuth{Username: "user", Password: "pass"})(&kept)
	WithHTTPAuth(nil)(&kept)
	assert.Equal(t, "Basic dXNlcjpwYXNz", header(t, kept, "example.com"))
}
