package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"

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
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
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
