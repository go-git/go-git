package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// httpRunner is the protocol v2 transport.CommandRunner for smart HTTP. Each
// command request is sent as a single stateless POST.
type httpRunner struct {
	client     *http.Client
	baseURL    *url.URL
	service    string
	authorizer func(*http.Request) error
}

var _ transport.CommandRunner = (*httpRunner)(nil)

func (r *httpRunner) Run(ctx context.Context, requestBody []byte) (io.ReadCloser, error) {
	serviceURL, err := url.JoinPath(r.baseURL.String(), r.service)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", fmt.Sprintf("application/x-%s-request", r.service))
	httpReq.Header.Set("Accept", fmt.Sprintf("application/x-%s-result", r.service))
	httpReq.Header.Set("User-Agent", capability.DefaultAgent())
	httpReq.Header.Set("Git-Protocol", "version=2")
	if r.baseURL.User != nil {
		password, _ := r.baseURL.User.Password()
		httpReq.SetBasicAuth(r.baseURL.User.Username(), password)
	}
	if r.authorizer != nil {
		if err := r.authorizer(httpReq); err != nil {
			return nil, err
		}
	}

	resp, err := doRequest(r.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("http transport: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http transport: POST %s unexpected status %d", serviceURL, resp.StatusCode)
	}

	return resp.Body, nil
}

// Close is a no-op: smart HTTP holds no persistent connection between commands.
func (r *httpRunner) Close() error { return nil }
