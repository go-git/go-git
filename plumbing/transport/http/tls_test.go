package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveClient_Default(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{})
	client := tr.resolveClient()

	assert.NotNil(t, client)
	httpTr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.False(t, httpTr.TLSClientConfig.InsecureSkipVerify)
	assert.Nil(t, httpTr.TLSClientConfig.RootCAs)
}

func TestResolveClient_InsecureSkipVerify(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{
		TLS: &tls.Config{InsecureSkipVerify: true},
	})
	client := tr.resolveClient()

	httpTr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, httpTr.TLSClientConfig)
	assert.True(t, httpTr.TLSClientConfig.InsecureSkipVerify)
}

func TestResolveClient_CABundle(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(testCAPEM)

	tr := NewTransport(Options{
		TLS: &tls.Config{RootCAs: pool},
	})
	client := tr.resolveClient()

	httpTr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, httpTr.TLSClientConfig)
	assert.NotNil(t, httpTr.TLSClientConfig.RootCAs)
}

func TestResolveClient_InsecureAndCABundle(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(testCAPEM)

	tr := NewTransport(Options{
		TLS: &tls.Config{
			InsecureSkipVerify: true,
			RootCAs:            pool,
		},
	})
	client := tr.resolveClient()

	httpTr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, httpTr.TLSClientConfig)
	assert.True(t, httpTr.TLSClientConfig.InsecureSkipVerify)
	assert.NotNil(t, httpTr.TLSClientConfig.RootCAs)
}

func TestResolveClient_CustomClient_IgnoresTLS(t *testing.T) {
	t.Parallel()

	customTransport := &http.Transport{}
	custom := &http.Client{Transport: customTransport}
	tr := NewTransport(Options{
		Client: custom,
		TLS:    &tls.Config{InsecureSkipVerify: true},
	})
	client := tr.resolveClient()

	assert.NotSame(t, custom, client)
	assert.Same(t, customTransport, client.Transport)
	require.NotNil(t, client.CheckRedirect)
	assert.Nil(t, custom.CheckRedirect)
}

func TestResolveClient_CustomClientWrapsRedirectPolicy(t *testing.T) {
	t.Parallel()

	called := false
	custom := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			called = true
			return nil
		},
	}
	tr := NewTransport(Options{Client: custom})
	client := tr.resolveClient()

	target, _ := url.Parse("http://example.com/repo.git")
	req := &http.Request{URL: target, Header: http.Header{}}
	req = req.WithContext(withInitialRequest(context.Background()))

	err := client.CheckRedirect(req, []*http.Request{{}})
	require.NoError(t, err)
	assert.True(t, called)

	req = req.WithContext(context.Background())
	err = client.CheckRedirect(req, []*http.Request{{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-initial request")
}

func TestResolveClient_LowSpeedPreservesOptions(t *testing.T) {
	t.Parallel()

	proxy, err := url.Parse("http://proxy.example:8080")
	require.NoError(t, err)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	guard := &LowSpeedGuard{Limit: 100, Time: time.Second}
	opts := Options{
		TLS:             tlsConfig,
		HTTPProxy:       http.ProxyURL(proxy),
		FollowRedirects: NoFollowRedirects,
		LowSpeed:        guard,
	}
	client := NewTransport(opts).resolveClient()
	defer client.CloseIdleConnections()
	wrapped, ok := client.Transport.(*lowSpeedTransport)
	require.True(t, ok)
	base, ok := wrapped.RoundTripper.(*http.Transport)
	require.True(t, ok)
	assert.Same(t, tlsConfig, base.TLSClientConfig)
	req, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	require.NoError(t, err)
	gotProxy, err := base.Proxy(req)
	require.NoError(t, err)
	assert.Equal(t, proxy, gotProxy)
	assert.ErrorContains(t, client.CheckRedirect(req, nil), "redirects disabled")
	assert.Equal(t, *guard, wrapped.guard)
	assert.Same(t, tlsConfig, opts.TLS)
	assert.Same(t, guard, opts.LowSpeed)
	assert.Nil(t, opts.Client)

	customTransport := &http.Transport{}
	custom := &http.Client{Transport: customTransport, Timeout: time.Minute}
	opts.Client = custom
	client = NewTransport(opts).resolveClient()
	defer client.CloseIdleConnections()
	wrapped, ok = client.Transport.(*lowSpeedTransport)
	require.True(t, ok)
	assert.Same(t, customTransport, wrapped.RoundTripper)
	assert.Equal(t, custom.Timeout, client.Timeout)
	assert.Same(t, customTransport, custom.Transport)
	assert.Nil(t, custom.CheckRedirect)
	assert.Nil(t, customTransport.TLSClientConfig)
	assert.Nil(t, customTransport.Proxy)
	assert.Equal(t, LowSpeedGuard{Limit: 100, Time: time.Second}, *guard)
}

func TestResolveClient_LowSpeedDisabled(t *testing.T) {
	t.Parallel()
	for _, guard := range []*LowSpeedGuard{nil, {}, {Limit: 1}, {Time: time.Second}, {Limit: -1, Time: time.Second}, {Limit: 1, Time: -1}} {
		t.Run(fmt.Sprint(guard), func(t *testing.T) {
			t.Parallel()
			custom := &http.Transport{}
			client := NewTransport(Options{
				Client: &http.Client{Transport: custom}, LowSpeed: guard,
			}).resolveClient()
			assert.Same(t, custom, client.Transport)
		})
	}
}

func TestResolveClient_NilTLS(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{TLS: nil})
	client := tr.resolveClient()

	httpTr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.False(t, httpTr.TLSClientConfig.InsecureSkipVerify)
	assert.Nil(t, httpTr.TLSClientConfig.RootCAs)
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
