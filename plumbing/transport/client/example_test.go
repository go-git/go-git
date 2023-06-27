package client_test

import (
	"crypto/tls"
	"net/http"

	"github.com/sgnl-ai/go-git/plumbing/transport/client"
	githttp "github.com/sgnl-ai/go-git/plumbing/transport/http"
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
