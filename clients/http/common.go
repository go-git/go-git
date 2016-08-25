// Package http implements a HTTP client for go-git.
package http

import (
	"fmt"
	"net/http"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
)

type HTTPAuthMethod interface {
	common.AuthMethod
	setAuth(r *http.Request)
}

type BasicAuth struct {
	username, password string
}

func NewBasicAuth(username, password string) *BasicAuth {
	return &BasicAuth{username, password}
}

func (a *BasicAuth) setAuth(r *http.Request) {
	r.SetBasicAuth(a.username, a.password)
}

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

type HTTPError struct {
	Response *http.Response
}

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

func (e *HTTPError) StatusCode() int {
	return e.Response.StatusCode
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected requesting %q status code: %d",
		e.Response.Request.URL, e.Response.StatusCode,
	)
}
