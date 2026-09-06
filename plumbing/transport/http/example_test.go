package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	transport "github.com/go-git/go-git/v6/plumbing/transport"
)

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
