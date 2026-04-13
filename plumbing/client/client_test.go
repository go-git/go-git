package client

import (
	"context"
	"net"
	"net/http"
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

func TestWithHTTPAuth(t *testing.T) {
	t.Parallel()

	auth := &xhttp.BasicAuth{Username: "user", Password: "pass"}

	c := New(WithHTTPAuth(auth))
	defer c.Close()

	tr, err := c.Transport("http")
	require.NoError(t, err)
	assert.NotNil(t, tr)
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

type mockTransport struct{}

func (m *mockTransport) Handshake(_ context.Context, _ *transport.Request) (transport.Session, error) {
	return nil, nil
}
