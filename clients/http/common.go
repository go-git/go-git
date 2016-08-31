// Package http implements a HTTP client for go-git.
package http

import (
	"fmt"
	"net/http"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
)

// HTTPAuthMethod concrete implementation of common.AuthMethod for HTTP services
type HTTPAuthMethod interface {
	common.AuthMethod
	setAuth(r *http.Request)
}

// BasicAuth represent a HTTP basic auth
type BasicAuth struct {
	username, password string
}

// NewBasicAuth returns a BasicAuth base on the given user and password
func NewBasicAuth(username, password string) *BasicAuth {
	return &BasicAuth{username, password}
}

func (a *BasicAuth) setAuth(r *http.Request) {
	r.SetBasicAuth(a.username, a.password)
}

// Name name of the auth
func (a *BasicAuth) Name() string {
	return "http-basic-auth"
}

func (a *BasicAuth) String() string {
	masked := "*******"
	if a.password == "" {
		masked = "<empty>"
	}

	return fmt.Sprintf("%s - %s:%s", a.Name(), a.username, masked)
}

// HTTPError a dedicated error to return errors bases on status codes
type HTTPError struct {
	Response *http.Response
}

// NewHTTPError returns a new HTTPError based on a http response
func NewHTTPError(r *http.Response) error {
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}

	switch r.StatusCode {
	case 401:
		return common.ErrAuthorizationRequired
	case 404:
		return common.ErrRepositoryNotFound
	}

	err := &HTTPError{r}
	return core.NewUnexpectedError(err)
}

// StatusCode returns the status code of the response
func (e *HTTPError) StatusCode() int {
	return e.Response.StatusCode
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected requesting %q status code: %d",
		e.Response.Request.URL, e.Response.StatusCode,
	)
}
