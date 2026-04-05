package http

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// Err represents an HTTP error response.
type Err struct {
	URL    *url.URL
	Status int
	Reason string
}

// StatusCode returns the HTTP status code of the error.
func (e *Err) StatusCode() int { return e.Status }

func (e *Err) Error() string {
	format := "unexpected requesting %q status code: %d"
	if e.Reason != "" {
		return fmt.Sprintf(format+": %s", e.URL, e.Status, e.Reason)
	}
	return fmt.Sprintf(format, e.URL, e.Status)
}

// checkError maps HTTP response status codes to typed transport errors.
func checkError(r *http.Response) error {
	if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var reason string
	var messageBuffer bytes.Buffer
	if r.Body != nil {
		messageLength, _ := messageBuffer.ReadFrom(r.Body)
		if messageLength > 0 {
			reason = messageBuffer.String()
		}
	}

	err := &Err{
		URL:    r.Request.URL,
		Status: r.StatusCode,
		Reason: reason,
	}

	switch r.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %w", transport.ErrAuthenticationRequired, err)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %w", transport.ErrAuthorizationFailed, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", transport.ErrRepositoryNotFound, err)
	}

	return err
}

const infoRefsPath = "/info/refs"

// applyRedirect updates the base URL if the server redirected.
func applyRedirect(resp *http.Response, baseURL *url.URL) *url.URL {
	if resp.Request == nil {
		return baseURL
	}
	r := resp.Request
	if !strings.HasSuffix(r.URL.Path, infoRefsPath) {
		return baseURL
	}
	redirected := *baseURL
	redirected.Host = r.URL.Host
	redirected.Scheme = r.URL.Scheme
	redirected.Path = r.URL.Path[:len(r.URL.Path)-len(infoRefsPath)]
	return &redirected
}

// doRequest performs an HTTP request and returns a typed error on failure.
func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return res, nil
	}

	return res, checkError(res)
}

// applyAuth sets basic auth from URL userinfo and/or the authorizer function.
func applyAuth(httpReq *http.Request, baseURL *url.URL, authorizer func(*http.Request) error) error {
	if baseURL.User != nil {
		password, _ := baseURL.User.Password()
		httpReq.SetBasicAuth(baseURL.User.Username(), password)
	}
	if authorizer != nil {
		return authorizer(httpReq)
	}
	return nil
}
