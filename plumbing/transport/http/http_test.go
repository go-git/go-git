package http

import (
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
)

func setupSmartServer(t testing.TB) (base string, addr *net.TCPAddr) {
	t.Helper()

	l := test.ListenTCP(t)
	addr = l.Addr().(*net.TCPAddr)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-%d", addr.Port))
	require.NoError(t, os.MkdirAll(base, 0o755))

	out, err := exec.Command("git", "--exec-path").CombinedOutput()
	require.NoError(t, err)

	server := &http.Server{
		Handler: &cgi.Handler{
			Path: filepath.Join(strings.TrimSpace(string(out)), "git-http-backend"),
			Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", base)},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		require.ErrorIs(t, server.Serve(l), http.ErrServerClosed)
	}()
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		<-done
	})

	return base, addr
}

func prepareRepo(t testing.TB, f *fixtures.Fixture, base, name string) billy.Filesystem {
	t.Helper()
	return test.PrepareRepository(t, f, base, name)
}

func httpEndpoint(addr *net.TCPAddr, name string) *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", addr.Port),
		Path:   "/" + name,
	}
}
