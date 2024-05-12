package client_test

import (
	"crypto/tls"
	"net/http"

	"github.com/grahambrooks/go-git/v5/plumbing/transport/client"
	githttp "github.com/grahambrooks/go-git/v5/plumbing/transport/http"
)

func ExampleInstallProtocol() {
	// Create custom net/http client that.
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Install it as default client for https URLs.
	client.InstallProtocol("https", githttp.NewClient(httpClient))
}
