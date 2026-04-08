package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func TestRedirectPath(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	prepareRepo(t, fixtures.Basic().One(), base, "basic.git")

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := backendURL + "/basic.git" + r.URL.Path[len("/redirected-repo"):]
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/redirected-repo",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
}

func TestRedirectSchema(t *testing.T) {
	t.Parallel()

	base, backend := setupSmartServer(t)
	basicFS := prepareRepo(t, fixtures.Basic().One(), base, "basic.git")
	_ = filesystem.NewStorage(basicFS, nil)

	rl := test.ListenTCP(t)
	raddr := rl.Addr().(*net.TCPAddr)

	backendURL := fmt.Sprintf("http://localhost:%d", backend.Port)
	redirectServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, backendURL+r.URL.Path+"?"+r.URL.RawQuery, http.StatusMovedPermanently)
		}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, redirectServer.Serve(rl), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, redirectServer.Close())
		<-done
	})

	tr := NewTransport(Options{})
	endpoint := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", raddr.Port),
		Path:   "/basic.git",
	}

	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     endpoint,
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	info, err := session.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
}
