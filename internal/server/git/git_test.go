package git_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servergit "github.com/go-git/go-git/v6/internal/server"
	servergitdaemon "github.com/go-git/go-git/v6/internal/server/git"
	transportgit "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/git"
)

func TestGitServer_UploadPack(t *testing.T) {
	t.Parallel()

	srv := startGitServer(t, fixtures.Basic().One())

	tr := git.NewTransport(git.Options{})
	req := &transportgit.Request{
		URL:     srv.url("/basic.git"),
		Command: transportgit.UploadPackService,
	}

	sess, err := tr.Handshake(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { sess.Close() })

	refs, err := sess.GetRemoteRefs(context.Background())
	require.NoError(t, err)
	assert.Greater(t, len(refs), 0, "server should advertise refs")
}

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

func TestGitServer_UnsupportedService(t *testing.T) {
	t.Parallel()

	srv := startGitServer(t, fixtures.Basic().One())

	tr := git.NewTransport(git.Options{})
	req := &transportgit.Request{
		URL:     srv.url("/basic.git"),
		Command: "git-unsupported-service",
	}

	// The server silently drops connections for unsupported services.
	// Connect may succeed (it's just TCP + request write), but
	// Handshake will fail because the server closes the connection.
	conn, err := tr.Connect(context.Background(), req)
	if err != nil {
		// Connection failed — acceptable outcome.
		return
	}
	defer conn.Close()

	// If Connect succeeded, reading from the connection should fail
	// because the server closes it immediately.
	buf := make([]byte, 1)
	_, err = conn.Reader().Read(buf)
	assert.Error(t, err, "reading from unsupported service should fail")
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
