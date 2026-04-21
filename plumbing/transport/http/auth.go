package http

import "net/http"

// BasicAuth implements HTTP basic authentication.
type BasicAuth struct {
	Username, Password string
}

// Authorizer sets basic auth on the HTTP request.
func (a *BasicAuth) Authorizer(r *http.Request) error {
	r.SetBasicAuth(a.Username, a.Password)
	return nil
}

// TokenAuth implements HTTP bearer token authentication.
type TokenAuth struct {
	Token string
}

// Authorizer sets the bearer token on the HTTP request.
func (a *TokenAuth) Authorizer(r *http.Request) error {
	r.Header.Set("Authorization", "Bearer "+a.Token)
	return nil
}
