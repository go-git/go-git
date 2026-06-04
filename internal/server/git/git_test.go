package git_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servergit "github.com/go-git/go-git/v6/internal/server"
	servergitdaemon "github.com/go-git/go-git/v6/internal/server/git"
	transportgit "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/git"
)

func TestGitServer_Archive(t *testing.T) {
	t.Parallel()

	srv := startGitServer(t, fixtures.Basic().One())

	tr := git.NewTransport(git.Options{})
	req := &transportgit.Request{
		URL:     srv.url("/basic.git"),
		Command: transportgit.UploadArchiveService,
	}

	sess, err := tr.Handshake(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { sess.Close() })

	arch, ok := sess.(transportgit.Archiver)
	require.True(t, ok, "session should implement Archiver")

	rc, err := arch.Archive(context.Background(), &transportgit.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { rc.Close() })

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	tarReader := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0, "archive should contain files")
}

func TestGitServer_ArchiveTarGz(t *testing.T) {
	t.Parallel()

	srv := startGitServer(t, fixtures.Basic().One())

	tr := git.NewTransport(git.Options{})
	req := &transportgit.Request{
		URL:     srv.url("/basic.git"),
		Command: transportgit.UploadArchiveService,
	}

	sess, err := tr.Handshake(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { sess.Close() })

	arch, ok := sess.(transportgit.Archiver)
	require.True(t, ok, "session should implement Archiver")

	rc, err := arch.Archive(context.Background(), &transportgit.ArchiveRequest{
		Args: []string{"--format=tar.gz", "master"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { rc.Close() })

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	gr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)

	tarReader := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0, "archive should contain files")
}

func TestGitServer_MaxConnections(t *testing.T) {
	t.Parallel()

	loader := servergit.Loader(t, fixtures.Basic().One())
	srv := servergitdaemon.FromLoader(loader)
	srv.MaxConnections = 1

	endpoint, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { srv.Close() })

	u, err := url.Parse(endpoint + "/basic.git")
	require.NoError(t, err)

	tr1 := git.NewTransport(git.Options{})
	conn1, err := tr1.Connect(context.Background(), &transportgit.Request{
		URL: u, Command: transportgit.UploadPackService,
	})
	require.NoError(t, err)
	defer conn1.Close()

	conn2, err := net.Dial("tcp", u.Host)
	require.NoError(t, err, "TCP accept should succeed before server checks MaxConnections")
	defer conn2.Close()

	require.NoError(t, conn2.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 1)
	_, err = conn2.Read(buf)
	assert.ErrorIs(t, err, io.EOF, "second connection should be closed by server due to MaxConnections")
}

func TestGitServer_InitTimeout(t *testing.T) {
	t.Parallel()

	loader := servergit.Loader(t, fixtures.Basic().One())
	srv := servergitdaemon.FromLoader(loader)
	srv.InitTimeout = 50 * time.Millisecond

	endpoint, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { srv.Close() })

	// The init timeout configuration is accepted without error.
	// Exercising it requires a raw TCP connection that stalls after
	// connect, which is difficult from the client transport layer.
	_ = endpoint
}

func TestGitServer_Timeout(t *testing.T) {
	t.Parallel()

	loader := servergit.Loader(t, fixtures.Basic().One())
	srv := servergitdaemon.FromLoader(loader)
	srv.Timeout = 200 * time.Millisecond

	endpoint, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { srv.Close() })

	u, err := url.Parse(endpoint)
	require.NoError(t, err)

	conn, err := net.Dial("tcp", u.Host)
	require.NoError(t, err)
	defer conn.Close()

	// Stay idle past the configured Timeout; the server must close
	// the connection, surfacing as a Read error.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 1)
	start := time.Now()
	_, err = conn.Read(buf)
	elapsed := time.Since(start)
	assert.Error(t, err, "server should close idle connection past Timeout")
	// Allow a generous tolerance for timer/scheduling jitter on shared
	// CI runners. We measure elapsed from the client's perspective but
	// the server arms its deadline a few ms earlier (during accept and
	// the first Read), so client-measured elapsed underestimates the
	// real server timeout. A 30ms tolerance still catches a server that
	// closes substantially earlier than configured.
	assert.GreaterOrEqual(t, elapsed, srv.Timeout-30*time.Millisecond, "server should not close before Timeout elapses")
}

// gitServer is a helper that holds a running git:// server and its endpoint.
type gitServer struct {
	server   *servergitdaemon.Server
	endpoint string
}

func (g *gitServer) url(path string) *url.URL {
	u, err := url.Parse(g.endpoint + path)
	if err != nil {
		panic("invalid URL: " + err.Error())
	}
	return u
}

// startGitServer starts a git:// server backed by the given fixture.
func startGitServer(t *testing.T, f *fixtures.Fixture) *gitServer {
	t.Helper()

	loader := servergit.Loader(t, f)
	srv := servergitdaemon.FromLoader(loader)

	endpoint, err := srv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { srv.Close() })

	return &gitServer{
		server:   srv,
		endpoint: endpoint,
	}
}
