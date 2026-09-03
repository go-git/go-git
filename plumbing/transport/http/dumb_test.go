package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/transport/test"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

func setupDumbServer(t testing.TB) (base string, addr *net.TCPAddr) {
	return setupDumbServerWithMiddleware(t, func(h http.Handler) http.Handler { return h })
}

func setupDumbServerWithMiddleware(
	t testing.TB,
	middleware func(http.Handler) http.Handler,
) (base string, addr *net.TCPAddr) {
	t.Helper()

	l := test.ListenTCP(t)
	addr = l.Addr().(*net.TCPAddr)
	base = filepath.Join(t.TempDir(), fmt.Sprintf("go-git-http-dumb-%d", addr.Port))
	require.NoError(t, os.MkdirAll(base, 0o755))

	fileServer := http.FileServer(http.Dir(base))
	server := &http.Server{
		Handler: middleware(noSendFileHandler(fileServer)),
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

func noSendFileHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(&noSendFileResponseWriter{ResponseWriter: w}, r)
	})
}

type noSendFileResponseWriter struct {
	http.ResponseWriter
}

type requestCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *requestCounter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		if c.counts == nil {
			c.counts = make(map[string]int)
		}
		c.counts[r.URL.Path]++
		c.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (c *requestCounter) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[path]
}

func (w *noSendFileResponseWriter) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(p)
}

type dumbFilesystemStorer struct {
	storage.Storer
	fs billy.Filesystem
}

func (s *dumbFilesystemStorer) Filesystem() billy.Filesystem {
	return s.fs
}

func TestParseInfoPack(t *testing.T) {
	t.Parallel()

	sha1 := strings.Repeat("1", formatcfg.SHA1HexSize)
	sha256 := strings.Repeat("2", formatcfg.SHA256HexSize)

	tests := []struct {
		name     string
		line     string
		wantName string
		wantHash string
		wantErr  bool
	}{
		{
			name:     "loose SHA-1",
			line:     "P loose-" + sha1 + ".pack",
			wantName: "loose-" + sha1 + ".pack",
			wantHash: sha1,
		},
		{
			name:     "pack SHA-256",
			line:     "P pack-" + sha256 + ".pack",
			wantName: "pack-" + sha256 + ".pack",
			wantHash: sha256,
		},
		{name: "missing record type", line: "loose-" + sha1 + ".pack", wantErr: true},
		{name: "unsupported prefix", line: "P temp-" + sha1 + ".pack", wantErr: true},
		{name: "invalid hash", line: "P pack-not-a-hash.pack", wantErr: true},
		{name: "zero hash", line: "P pack-" + strings.Repeat("0", formatcfg.SHA1HexSize) + ".pack", wantErr: true},
		{name: "path traversal", line: "P ../pack-" + sha1 + ".pack", wantErr: true},
		{name: "backslash path", line: `P ..\pack-` + sha1 + ".pack", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseInfoPack(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, got.name)
			require.Equal(t, tt.wantHash, got.hash.String())
			require.Equal(t, path.Join("objects", "pack", tt.wantName), got.packPath())
			require.Equal(t,
				path.Join("objects", "pack", strings.TrimSuffix(tt.wantName, ".pack")+".idx"),
				got.idxPath(),
			)
		})
	}
}

func TestDumbFetchUsesAdvertisedLoosePackName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		objectFormat formatcfg.ObjectFormat
		fixture      *fixtures.Fixture
	}{
		{
			name:         "SHA-1",
			objectFormat: formatcfg.SHA1,
			fixture:      fixtures.Basic().ByTag("packfile").ByTag(".git").ByObjectFormat("sha1").One(),
		},
		{
			name:         "SHA-256",
			objectFormat: formatcfg.SHA256,
			fixture:      fixtures.Basic().ByTag("packfile").ByTag(".git").ByObjectFormat("sha256").One(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, addr := setupDumbServer(t)
			serverFS := prepareRepo(t, tt.fixture, base, "loose.git")
			serverStorage := filesystem.NewStorageWithOptions(
				serverFS,
				nil,
				filesystem.Options{ObjectFormat: tt.objectFormat},
			)
			t.Cleanup(func() { require.NoError(t, serverStorage.Close()) })
			if tt.objectFormat == formatcfg.SHA256 {
				require.NoError(t, serverStorage.SetObjectFormat(tt.objectFormat))
			}

			hash := tt.fixture.PackfileHash
			canonicalBase := filepath.Join("objects", "pack", "pack-"+hash)
			looseBase := filepath.Join("objects", "pack", "loose-"+hash)
			for _, name := range []string{canonicalBase + ".pack", canonicalBase + ".idx"} {
				err := serverFS.Remove(name)
				require.True(t, err == nil || os.IsNotExist(err), "remove %s: %v", name, err)
			}
			copyFixtureFile(t, serverFS, looseBase+".pack", tt.fixture.Packfile)
			copyFixtureFile(t, serverFS, looseBase+".idx", tt.fixture.Idx)
			packNames, err := serverStorage.ObjectPackNames()
			require.NoError(t, err)
			require.Equal(t, []string{filepath.Base(looseBase) + ".pack"}, packNames)
			require.NoError(t, transport.UpdateServerInfo(serverStorage, serverFS))

			clientFS := memfs.New()
			require.NoError(t, clientFS.MkdirAll(filepath.Join("objects", "pack"), 0o755))
			memoryStorage := memory.NewStorage(memory.WithObjectFormat(tt.objectFormat))
			clientStorage := &dumbFilesystemStorer{
				Storer: memoryStorage,
				fs:     clientFS,
			}

			clientTransport := NewTransport(Options{ForceDumb: true})
			endpoint := httpEndpoint(addr, "loose.git")
			session, err := clientTransport.Handshake(context.Background(), &transport.Request{
				URL:     endpoint,
				Command: transport.UploadPackService,
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, session.Close()) })

			err = session.Fetch(context.Background(), clientStorage, &transport.FetchRequest{
				Wants: []plumbing.Hash{plumbing.NewHash(tt.fixture.Head)},
			})
			require.NoError(t, err)

			_, err = clientFS.Stat(looseBase + ".pack")
			require.NoError(t, err)
			_, err = clientFS.Stat(looseBase + ".idx")
			require.NoError(t, err)
		})
	}
}

func TestDumbFetchReusesDownloadedPackFiles(t *testing.T) {
	t.Parallel()

	counter := new(requestCounter)
	base, addr := setupDumbServerWithMiddleware(t, counter.middleware)
	fixture := fixtures.Basic().ByTag("packfile").ByTag(".git").ByObjectFormat("sha1").One()
	serverFS := prepareRepo(t, fixture, base, "reuse.git")
	serverStorage := filesystem.NewStorage(serverFS, nil)
	t.Cleanup(func() { require.NoError(t, serverStorage.Close()) })
	require.NoError(t, transport.UpdateServerInfo(serverStorage, serverFS))

	packNames, err := serverStorage.ObjectPackNames()
	require.NoError(t, err)
	require.Len(t, packNames, 1)
	packPath := path.Join("objects", "pack", packNames[0])
	idxPath := strings.TrimSuffix(packPath, ".pack") + ".idx"

	clientFS := memfs.New()
	require.NoError(t, clientFS.MkdirAll(path.Join("objects", "pack"), 0o755))
	clientTransport := NewTransport(Options{ForceDumb: true})
	session, err := clientTransport.Handshake(context.Background(), &transport.Request{
		URL:     httpEndpoint(addr, "reuse.git"),
		Command: transport.UploadPackService,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })

	fetch := func() {
		clientStorage := &dumbFilesystemStorer{
			Storer: memory.NewStorage(),
			fs:     clientFS,
		}
		err := session.Fetch(context.Background(), clientStorage, &transport.FetchRequest{
			Wants: []plumbing.Hash{plumbing.NewHash(fixture.Head)},
		})
		require.NoError(t, err)
	}

	fetch()
	serverPackPath := "/reuse.git/" + packPath
	serverIdxPath := "/reuse.git/" + idxPath
	require.Equal(t, 1, counter.count(serverPackPath))
	require.Equal(t, 1, counter.count(serverIdxPath))

	fetch()
	require.Equal(t, 1, counter.count(serverPackPath), "cached pack must not be downloaded again")
	require.Equal(t, 1, counter.count(serverIdxPath), "cached index must not be downloaded again")
}

type statErrorFS struct {
	billy.Filesystem
	path string
	err  error
}

func (f *statErrorFS) Stat(name string) (os.FileInfo, error) {
	if name == f.path {
		return nil, f.err
	}
	return f.Filesystem.Stat(name)
}

func TestDownloadFileIfMissingPropagatesStatError(t *testing.T) {
	t.Parallel()

	for _, file := range []string{
		"objects/pack/pack-deadbeef.idx",
		"objects/pack/pack-deadbeef.pack",
	} {
		t.Run(filepath.Ext(file), func(t *testing.T) {
			t.Parallel()
			wantErr := errors.New("simulated stat failure")
			fs := &statErrorFS{Filesystem: memfs.New(), path: file, err: wantErr}
			walker := &fetchWalker{fs: fs}
			require.ErrorIs(t, walker.downloadFileIfMissing(file), wantErr)
		})
	}
}

func copyFixtureFile(
	t *testing.T,
	fs billy.Filesystem,
	name string,
	open func() (billy.File, error),
) {
	t.Helper()

	source, err := open()
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()

	destination, err := fs.Create(name)
	require.NoError(t, err)
	_, err = io.Copy(destination, source)
	require.NoError(t, err)
	require.NoError(t, destination.Close())
}

type dumbUploadPackSuite struct {
	test.UploadPackSuite
}

func TestDumbUploadPackSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(dumbUploadPackSuite))
}

func (s *dumbUploadPackSuite) SetupTest() {
	base, addr := setupDumbServer(s.T())

	basicFS := prepareRepo(s.T(), fixtures.Basic().One(), base, "basic.git")
	emptyFS := prepareRepo(s.T(), fixtures.ByTag("empty").One(), base, "empty.git")

	s.Endpoint = httpEndpoint(addr, "basic.git")
	s.EmptyEndpoint = httpEndpoint(addr, "empty.git")
	s.NonExistentEndpoint = httpEndpoint(addr, "non-existent.git")

	s.Storer = filesystem.NewStorage(basicFS, nil)
	s.EmptyStorer = filesystem.NewStorage(emptyFS, nil)
	s.NonExistentStorer = memory.NewStorage()

	s.Transport = NewTransport(Options{ForceDumb: true})

	require.NoError(s.T(), transport.UpdateServerInfo(s.Storer, basicFS))
	require.NoError(s.T(), transport.UpdateServerInfo(s.EmptyStorer, emptyFS))
}

func (*dumbUploadPackSuite) TestDefaultBranch()                         {}
func (*dumbUploadPackSuite) TestAdvertisedReferencesEmpty()             {}
func (*dumbUploadPackSuite) TestAdvertisedReferencesFilterUnsupported() {}
func (*dumbUploadPackSuite) TestCapabilities()                          {}
func (*dumbUploadPackSuite) TestUploadPack()                            {}
func (*dumbUploadPackSuite) TestUploadPackFull()                        {}
func (*dumbUploadPackSuite) TestUploadPackInvalidReq()                  {}
func (*dumbUploadPackSuite) TestUploadPackMulti()                       {}
func (*dumbUploadPackSuite) TestUploadPackNoChanges()                   {}
func (*dumbUploadPackSuite) TestUploadPackPartial()                     {}
