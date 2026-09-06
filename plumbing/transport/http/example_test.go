package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

// A credential held for the repository is withheld the moment a redirect leaves
// the repository's origin. To authenticate where the redirect landed, supply a
// credential for that origin too: one adapter per origin, combined with Chain.
// Each answers only for the origin it stands for, so a redirect cannot draw one
// origin's credential to another.
func ExampleForOrigin() {
	// Stands in for the origin a redirect moves the repository to. It
	// challenges, because the repository's own credential does not reach it.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Println("gateway received:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		// A minimal protocol v2 capability advertisement.
		_, _ = fmt.Fprint(w, "001e# service=git-upload-pack\n0000000eversion 2\n0000")
	}))
	defer gateway.Close()

	repository := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, gateway.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	defer repository.Close()

	gatewayOrigin, err := url.Parse(gateway.URL)
	if err != nil {
		panic(err)
	}

	opts := Options{Credentials: Chain(
		ForRepositoryOrigin(func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer repository-token")
			return nil
		}),
		ForOrigin(gatewayOrigin, func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer gateway-token")
			return nil
		}),
	)}

	repositoryURL, err := url.Parse(repository.URL + "/repo.git")
	if err != nil {
		panic(err)
	}
	// The transport asks the source about each origin it is about to reach,
	// and declining is legal: no adapter here answers for an origin it was not
	// given, and the request simply goes out unauthenticated.
	session, err := NewTransport(opts).Handshake(context.Background(), &transport.Request{
		URL:     repositoryURL,
		Command: transport.UploadPackService,
	})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Output:
	// gateway received: Bearer gateway-token
}

// The common case: one credential, for the repository's own origin, and
// nowhere else. ForRepositoryOrigin reads the origin from each request rather
// than being told it up front, so the same source serves any repository the
// caller clones and still answers for none but its own origin.
func ExampleForRepositoryOrigin() {
	repository := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("repository received:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = fmt.Fprint(w, advert(transport.UploadPackService))
	}))
	defer repository.Close()

	opts := Options{Credentials: ForRepositoryOrigin(func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer repository-token")
		return nil
	})}

	repositoryURL, err := url.Parse(repository.URL + "/repo.git")
	if err != nil {
		panic(err)
	}
	session, err := NewTransport(opts).Handshake(context.Background(), &transport.Request{
		URL:     repositoryURL,
		Command: transport.UploadPackService,
	})
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Output:
	// repository received: Bearer repository-token
}
