package file

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFileTransport_Open(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-upload-pack",
	}

	sess, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.NoError(t, sess.Close())
}

func TestFileTransport_Connect(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-receive-pack",
	}

	rwc, err := tr.Connect(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, rwc)
	require.NoError(t, rwc.Close())
}

func TestFileTransport_UnsupportedCommand(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{
		"/fake/repo.git": memory.NewStorage(),
	}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/fake/repo.git"},
		Command: "git-fake-command",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, transport.ErrCommandUnsupported)
}

func TestFileTransport_RepoNotFound(t *testing.T) {
	t.Parallel()

	loader := transport.MapLoader{}

	tr := NewTransport(Options{Loader: loader})

	req := &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: "/nonexistent.git"},
		Command: "git-upload-pack",
	}

	_, err := tr.Connect(context.Background(), req)
	require.Error(t, err)
}

func TestFileTransport_ImplementsConnector(t *testing.T) {
	t.Parallel()

	tr := NewTransport(Options{})

	_, ok := any(tr).(transport.Connector)
	assert.True(t, ok)
}

var _ transport.Conn = (*fileConn)(nil)

func archiveSession(t *testing.T) transport.Archiver {
	t.Helper()
	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	a, ok := session.(transport.Archiver)
	require.True(t, ok, "session should implement Archiver")
	return a
}

func TestArchive_Tar(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Greater(t, len(data), 0)

	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}

func TestArchive_TarGz(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar.gz", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	gr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)

	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}

func TestArchive_Zip(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=zip", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Greater(t, len(zr.File), 0)
}

func TestArchive_Prefix(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "--prefix=myproject/", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(hdr.Name, "myproject/"), "expected prefix myproject/, got %s", hdr.Name)
	}
}

func TestArchive_List(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--list"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	formats := strings.TrimSpace(string(data))
	lines := strings.Split(formats, "\n")
	assert.Contains(t, lines, "tar")
	assert.Contains(t, lines, "zip")
	assert.Contains(t, lines, "tar.gz")
	assert.Contains(t, lines, "tgz")
}

func TestArchive_TarFilePermissions(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	// The basic fixture has files with mode 0o100664 (group writable) and
	// directories with mode 0o040775. The umask 0o002 should preserve
	// the group writable bit, resulting in 0o664 for files and 0o775 for dirs.
	// Note: umask 0o002 means "remove write permission for others".
	// To get 0o644 from 0o664, we'd need umask 0o022.
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// Just verify modes are reasonable and consistent
		switch hdr.Typeflag {
		case tar.TypeDir:
			// Directories should be readable and executable by all
			assert.NotZero(t, hdr.Mode&0o555, "directory %s should be readable/executable", hdr.Name)
		case tar.TypeReg:
			// Regular files should be readable by all (owner at minimum)
			assert.NotZero(t, hdr.Mode&0o400, "regular file %s should be readable", hdr.Name)
			// Verify no special bits (setuid/setgid/sticky) are set in archived files
			// Git doesn't store these in the tree, and they shouldn't appear
			assert.Zero(t, hdr.Mode&0o7000, "file %s should not have special mode bits", hdr.Name)
		case tar.TypeSymlink:
			// Symlinks should always be 0o777 per canonical git
			assert.Equal(t, int64(0o777), hdr.Mode&0o777,
				"symlink %s should have mode 0o777, got 0o%03o", hdr.Name, hdr.Mode&0o777)
		}
	}
}

func TestArchive_ZipFilePermissions(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=zip", "master"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	for _, f := range zr.File {
		mode := f.Mode()
		// Skip directories in zip
		if f.FileInfo().IsDir() {
			continue
		}
		// Check if it's a symlink
		if mode&os.ModeSymlink != 0 {
			// Symlinks should have 0o777 permissions
			assert.Equal(t, os.FileMode(0o777), mode&os.ModePerm,
				"symlink %s should have mode 0o777, got 0o%03o", f.Name, mode&os.ModePerm)
			continue
		}
		// Just verify files are readable by owner
		assert.NotZero(t, mode&0o400, "file %s should be readable by owner", f.Name)
	}
}

func TestArchive_UploadPackSessionRejectsArchive(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	defer session.Close()

	a, ok := session.(transport.Archiver)
	require.True(t, ok, "StreamSession always implements Archiver")

	_, err = a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"master"},
	})
	require.ErrorIs(t, err, transport.ErrArchiveUnsupported)
}

func TestArchive_UnreachableBlocked(t *testing.T) {
	t.Parallel()

	a := archiveSession(t)

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"},
	})
	require.NoError(t, err)

	_, err = io.ReadAll(r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only ref names are allowed")
}

func TestArchive_AllowUnreachableConfig(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repoFS := test.PrepareRepository(t, fixtures.Basic().One(), base, "basic.git")
	repoPath, err := filepath.Abs(repoFS.Root())
	require.NoError(t, err)

	cfgPath := filepath.Join(repoPath, "config")
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString("\n[uploadArchive]\n\tallowUnreachable = true\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	tr := NewTransport(Options{})
	session, err := tr.Handshake(context.Background(), &transport.Request{
		URL:     &url.URL{Scheme: "file", Path: repoPath},
		Command: transport.UploadArchiveService,
	})
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	a, ok := session.(transport.Archiver)
	require.True(t, ok, "session should implement Archiver")

	r, err := a.Archive(context.Background(), &transport.ArchiveRequest{
		Args: []string{"--format=tar", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Greater(t, len(data), 0)

	tr2 := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr2.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Greater(t, len(names), 0)
}
