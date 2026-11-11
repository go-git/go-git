package git

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func freePort(t *testing.T) int {
	t.Helper()
	port, err := test.FreePort()
	require.NoError(t, err)
	return port
}

func startDaemon(t *testing.T, base string, port int) {
	t.Helper()
	test.StartGitDaemon(t, base, port,
		"--enable=receive-pack", "--enable=upload-archive", "--max-connections=1")
}

const windowsSkipMsg = `git for windows has issues with write operations through git:// protocol.
See https://github.com/git-for-windows/git/issues/907`

func TestGitTransport_Connect(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	for _, tc := range []struct {
		name    string
		command string
	}{
		{"UploadPack", "git-upload-pack"},
		{"ReceivePack", "git-receive-pack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			port := freePort(t)
			base := filepath.Join(t.TempDir(), fmt.Sprintf("git-proto-%d", port))
			_ = test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
			startDaemon(t, base, port)

			tr := NewTransport(Options{})

			req := &transport.Request{
				URL: &url.URL{
					Scheme: "git",
					Host:   fmt.Sprintf("localhost:%d", port),
					Path:   "/basic.git",
				},
				Command:  tc.command,
				Protocol: protocol.V0,
			}

			sess, err := tr.Connect(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, sess)

			// A daemon that refuses the command still answers, with an
			// ERR pkt-line. Reading the first line as a pkt-line is what
			// tells acceptance from refusal: ReadLine reports an ERR
			// payload as *pktline.ErrorLine, where counting bytes off the
			// reader cannot distinguish the two.
			_, line, err := pktline.ReadLine(sess.Reader())
			require.NoError(t, err)
			assert.NotEmpty(t, line, "server should advertise refs")

			require.NoError(t, sess.Close())
		})
	}
}

func TestGitTransport_ConnectFail(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	tr := NewTransport(Options{})

	req := &transport.Request{
		URL: &url.URL{
			Scheme: "git",
			Host:   "localhost:1",
			Path:   "/nonexistent.git",
		},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}

func TestGitTransport_Archive(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(windowsSkipMsg)
	}

	port := freePort(t)
	base := filepath.Join(t.TempDir(), fmt.Sprintf("git-proto-%d", port))
	_ = test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	startDaemon(t, base, port)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL: &url.URL{
			Scheme: "git",
			Host:   fmt.Sprintf("localhost:%d", port),
			Path:   "/basic.git",
		},
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	defer session.Close()

	a, ok := session.(transport.Archiver)
	require.True(t, ok, "session should implement Archiver")

	rc, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Greater(t, len(data), 0)

	tarR := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tarR.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}
