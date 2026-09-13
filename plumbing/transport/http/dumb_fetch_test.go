package http

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	transport "github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// fetchDumb runs one fetch over the dumb HTTP protocol into st, the way a
// clone of a repository the client already holds, or an incremental fetch of
// one it shares history with, does.
func fetchDumb(t *testing.T, addr *net.TCPAddr, name string, st *filesystem.Storage, req *transport.FetchRequest) error {
	t.Helper()

	sess, err := NewTransport(Options{ForceDumb: true}).Handshake(
		context.Background(),
		&transport.Request{
			URL:     httpEndpoint(addr, name),
			Command: transport.UploadPackService,
		},
	)
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	if err := sess.Fetch(context.Background(), st, req); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// clientStorage returns a storage whose objects are the basic fixture's, held
// in memory: a client that has already fetched the repository.
func clientStorage(t *testing.T) *filesystem.Storage {
	t.Helper()

	fs, err := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
	require.NoError(t, err)

	st := filesystem.NewStorage(fs, nil)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// TestDumbFetchSkipsObjectsAlreadyPresent covers the failure this fixes. The
// walk reached an object the client already held, fetchObject reported success
// for it without ever populating the object, and obj.Type() was
// plumbing.InvalidObject, so the whole fetch aborted with ErrInvalidType.
//
// Reference git aborts the request for such an object and carries on
// (http-walker.c, fetch_object: odb_has_object). The server here is a
// repository the client already has in full, so every object the walk reaches
// is already present locally.
func TestDumbFetchSkipsObjectsAlreadyPresent(t *testing.T) {
	t.Parallel()

	base, addr := setupDumbServer(t)
	serverFS := prepareRepo(t, fixtures.Basic().One(), base, "basic.git")
	serverSt := filesystem.NewStorage(serverFS, nil)
	t.Cleanup(func() { _ = serverSt.Close() })
	require.NoError(t, transport.UpdateServerInfo(serverSt, serverFS))

	err := fetchDumb(t, addr, "basic.git", clientStorage(t), &transport.FetchRequest{})
	require.NoError(t, err, "a fetch whose objects are all already present must not abort")
}

// TestDumbFetchSkipsAncestorsItAlreadyHas is the incremental shape an ordinary
// second fetch takes: the server grew one commit, and the walk reaches the
// parent and the tree the client already holds - neither of which is in a pack
// index the walk has loaded yet.
func TestDumbFetchSkipsAncestorsItAlreadyHas(t *testing.T) {
	t.Parallel()

	base, addr := setupDumbServer(t)
	serverFS := prepareRepo(t, fixtures.Basic().One(), base, "basic.git")
	serverSt := filesystem.NewStorage(serverFS, nil)
	t.Cleanup(func() { _ = serverSt.Close() })

	master := plumbing.NewBranchReferenceName("master")
	head, err := serverSt.Reference(master)
	require.NoError(t, err)
	parent, err := object.GetCommit(serverSt, head.Hash())
	require.NoError(t, err)

	child := &object.Commit{
		Author:       parent.Author,
		Committer:    parent.Committer,
		Message:      "second\n",
		TreeHash:     parent.TreeHash,
		ParentHashes: []plumbing.Hash{parent.Hash},
	}
	obj := serverSt.NewEncodedObject()
	require.NoError(t, child.Encode(obj))
	childHash, err := serverSt.SetEncodedObject(obj)
	require.NoError(t, err)

	require.NoError(t, serverSt.SetReference(plumbing.NewHashReference(master, childHash)))
	require.NoError(t, transport.UpdateServerInfo(serverSt, serverFS))

	clientSt := clientStorage(t)
	require.NoError(t, clientSt.HasEncodedObject(parent.Hash),
		"the client must already hold the parent commit")

	require.NoError(t, fetchDumb(t, addr, "basic.git", clientSt, &transport.FetchRequest{}))
	require.NoError(t, clientSt.HasEncodedObject(childHash),
		"the new commit must have been fetched")
}

// TestDumbFetchKeepsPackIndexesItAlreadyHas covers the inverted existence
// check: Stat returns nil for a file that is present and never fs.ErrExist, so
// the "already have it" branch was dead and every walk re-downloaded every
// pack index. Here the served copy no longer carries the indexes at all, so a
// walk that re-downloads one fails on its 404 instead of using the index the
// client holds.
func TestDumbFetchKeepsPackIndexesItAlreadyHas(t *testing.T) {
	t.Parallel()

	base, addr := setupDumbServer(t)
	serverFS := prepareRepo(t, fixtures.Basic().One(), base, "basic.git")
	serverSt := filesystem.NewStorage(serverFS, nil)
	t.Cleanup(func() { _ = serverSt.Close() })
	require.NoError(t, transport.UpdateServerInfo(serverSt, serverFS))

	clientSt := clientStorage(t)

	entries, err := clientSt.Filesystem().ReadDir(filepath.Join("objects", "pack"))
	require.NoError(t, err)

	var indexes []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".idx") {
			indexes = append(indexes, e.Name())
		}
	}
	require.NotEmpty(t, indexes, "the client copy must carry a pack index for this test")

	for _, name := range indexes {
		require.NoError(t, serverFS.Remove(filepath.Join("objects", "pack", name)))
	}

	err = fetchDumb(t, addr, "basic.git", clientSt, &transport.FetchRequest{})
	require.NoError(t, err, "a pack index already on disk must not be fetched again")
}

// TestDumbFetchTreatsMissingInfoPacksAsNoPacks covers the 404 on
// objects/info/packs. A repository whose objects are all loose has no packs to
// list, and a host may simply not serve the file; checkError maps the 404 to
// transport.ErrRepositoryNotFound, so the repository was refused as missing
// even though every object it holds is fetchable.
//
// Reference git reads the same 404 as a successful listing of no packs
// (http-walker.c, fetch_indices: HTTP_MISSING_TARGET is handled with HTTP_OK).
func TestDumbFetchTreatsMissingInfoPacksAsNoPacks(t *testing.T) {
	t.Parallel()

	base, addr := setupDumbServer(t)

	serverPath := filepath.Join(base, "loose.git")
	require.NoError(t, os.MkdirAll(serverPath, 0o755))
	objects := writeLooseRepository(t, serverPath)
	require.NoError(t, os.Remove(filepath.Join(serverPath, "objects", "info", "packs")))

	clientSt := filesystem.NewStorage(memfs.New(), nil)
	t.Cleanup(func() { _ = clientSt.Close() })

	require.NoError(t, fetchDumb(t, addr, "loose.git", clientSt, &transport.FetchRequest{}))

	for _, o := range objects {
		require.NoError(t, clientSt.HasEncodedObject(o),
			"object %s must have been fetched despite the missing objects/info/packs", o)
	}
}

// TestDumbFetchReportsAWantTheServerCannotServe covers what the shared
// conformance case TestFetchError asserts: fetching an object the server does
// not have is an error. The walk used to satisfy that case only as a side
// effect of the already-present abort, because it never looked at the caller's
// wants at all.
func TestDumbFetchReportsAWantTheServerCannotServe(t *testing.T) {
	t.Parallel()

	base, addr := setupDumbServer(t)

	serverPath := filepath.Join(base, "loose.git")
	require.NoError(t, os.MkdirAll(serverPath, 0o755))
	writeLooseRepository(t, serverPath)

	clientSt := filesystem.NewStorage(memfs.New(), nil)
	t.Cleanup(func() { _ = clientSt.Close() })

	missing := plumbing.NewHash("1111111111111111111111111111111111111111")
	err := fetchDumb(t, addr, "loose.git", clientSt,
		&transport.FetchRequest{Wants: []plumbing.Hash{missing}})
	require.Error(t, err, "a want the server cannot serve must not pass as a successful fetch")
}

// writeLooseRepository writes a small bare repository whose objects are all
// loose, so a walk into it has no pack to fall back on, and leaves it with the
// objects/info/packs that transport.UpdateServerInfo generates. It returns the
// hashes it wrote, so a caller can assert what a fetch into it stored.
func writeLooseRepository(t *testing.T, dir string) []plumbing.Hash {
	t.Helper()

	fs := osfs.New(dir)
	st := filesystem.NewStorage(fs, nil)
	t.Cleanup(func() { _ = st.Close() })

	blobObj := st.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	w, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("hello dumb\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	blob, err := st.SetEncodedObject(blobObj)
	require.NoError(t, err)

	tree := &object.Tree{Entries: []object.TreeEntry{{
		Name: "hello.txt",
		Mode: filemode.Regular,
		Hash: blob,
	}}}
	treeObj := st.NewEncodedObject()
	require.NoError(t, tree.Encode(treeObj))
	treeHash, err := st.SetEncodedObject(treeObj)
	require.NoError(t, err)

	sig := object.Signature{
		Name:  "go-git",
		Email: "go-git@example.com",
		When:  time.Unix(0, 0).UTC(),
	}
	commit := &object.Commit{
		Author:    sig,
		Committer: sig,
		Message:   "initial\n",
		TreeHash:  treeHash,
	}
	commitObj := st.NewEncodedObject()
	require.NoError(t, commit.Encode(commitObj))
	commitHash, err := st.SetEncodedObject(commitObj)
	require.NoError(t, err)

	require.NoError(t, st.SetReference(plumbing.NewHashReference(
		plumbing.NewBranchReferenceName("master"), commitHash)))
	require.NoError(t, st.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName("master"))))

	require.NoError(t, transport.UpdateServerInfo(st, fs))

	return []plumbing.Hash{blob, treeHash, commitHash}
}
