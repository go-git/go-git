package dotgit

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/storage"
)

type SuiteDotGit struct {
	suite.Suite
}

func TestSuiteDotGit(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteDotGit))
}

func (s *SuiteDotGit) EmptyFS() (fs billy.Filesystem) { return memfs.New() }

func (s *SuiteDotGit) TestInitialize() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.Initialize()
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("objects", "info"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("objects", "pack"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("refs", "heads"))
	s.Require().NoError(err)

	_, err = fs.Stat(fs.Join("refs", "tags"))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestSetRefs() {
	fs := s.EmptyFS()

	dir := New(fs)

	testSetRefs(s, dir)
}

func (s *SuiteDotGit) TestSetRefsNorwfs() {
	fs := s.EmptyFS()

	dir := New(&norwfs{fs})

	testSetRefs(s, dir)
}

func (s *SuiteDotGit) TestRefsHeadFirst() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)
	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.NotEqual(0, len(refs))
	s.Equal("HEAD", refs[0].Name().String())
}

func testSetRefs(s *SuiteDotGit, dir *DotGit) {
	firstFoo := plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	)
	err := dir.SetRef(firstFoo, nil)

	s.Require().NoError(err)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/symbolic",
		"ref: refs/heads/foo",
	), nil)

	s.Require().NoError(err)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"bar",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/feature/baz",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 3)

	ref := findReference(refs, "refs/heads/foo")
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	ref = findReference(refs, "refs/heads/symbolic")
	s.NotNil(ref)
	s.Equal("refs/heads/foo", ref.Target().String())

	ref = findReference(refs, "bar")
	s.Nil(ref)

	_, err = dir.readReferenceFile(".", "refs/heads/feature/baz")
	s.Require().NoError(err)

	_, err = dir.readReferenceFile(".", "refs/heads/feature")
	s.ErrorIs(err, ErrIsDir)

	ref, err = dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	ref, err = dir.Ref("refs/heads/symbolic")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("refs/heads/foo", ref.Target().String())

	ref, err = dir.Ref("bar")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	// Check that SetRef with a non-nil `old` works.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	s.Require().NoError(err)

	// `firstFoo` is no longer the right `old` reference, so this
	// should fail.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	s.NotNil(err)
}

func (s *SuiteDotGit) TestRefsFromPackedRefs() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "refs/remotes/origin/branch")
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
}

func (s *SuiteDotGit) TestRefsFromReferenceFile() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	s.NotNil(ref)
	s.Equal(plumbing.SymbolicReference, ref.Type())
	s.Equal("refs/remotes/origin/master", string(ref.Target()))
}

func BenchmarkRefMultipleTimes(b *testing.B) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	refname := plumbing.ReferenceName("refs/remotes/origin/branch")

	dir := New(fs)
	_, err := dir.Ref(refname)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	for b.Loop() {
		_, err := dir.Ref(refname)
		if err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFile() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/HEAD")
	err := dir.RemoveRef(name)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, string(name))
	s.Nil(ref)
}

func (s *SuiteDotGit) TestRemoveRefFromPackedRefs() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/master")
	err := dir.RemoveRef(name)
	s.Require().NoError(err)

	b, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch\n",
		string(b))
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFileAndPackedRefs() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	// Make a ref file for a ref that's already in `packed-refs`.
	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/branch",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	// Make sure it only appears once in the refs list.
	refs, err := dir.Refs()
	s.Require().NoError(err)
	found := false
	for _, ref := range refs {
		if ref.Name() == "refs/remotes/origin/branch" {
			s.False(found)
			found = true
		}
	}

	name := plumbing.ReferenceName("refs/remotes/origin/branch")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	b, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master\n",
		string(b))

	refs, err = dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, string(name))
	s.Nil(ref)
}

func (s *SuiteDotGit) TestRemoveRefNonExistent() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	before, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.Require().NoError(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), string(before))
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	brokenContent := "BROKEN STUFF REALLY BROKEN"

	err := util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0o755))
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.NotNil(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), brokenContent)
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs2() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	brokenContent := strings.Repeat("a", bufio.MaxScanTokenSize*2)

	err := util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0o755))
	s.Require().NoError(err)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	s.NotNil(err)

	after, err := util.ReadFile(fs, packedRefsPath)
	s.Require().NoError(err)

	s.Equal(string(after), brokenContent)
}

func (s *SuiteDotGit) TestRefsFromHEADFile() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	s.Require().NoError(err)

	ref := findReference(refs, "HEAD")
	s.NotNil(ref)
	s.Equal(plumbing.SymbolicReference, ref.Type())
	s.Equal("refs/heads/master", string(ref.Target()))
}

func (s *SuiteDotGit) TestConfig() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	file, err := dir.Config()
	s.Require().NoError(err)
	s.Equal("config", filepath.Base(file.Name()))
}

func (s *SuiteDotGit) TestConfigWriteAndConfig() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.ConfigWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Config()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func (s *SuiteDotGit) TestIndex() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	idx, err := dir.Index()
	s.Require().NoError(err)
	s.NotNil(idx)
}

func (s *SuiteDotGit) TestIndexWriteAndIndex() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.IndexWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Index()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func (s *SuiteDotGit) TestShallow() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	file, err := dir.Shallow()
	s.Require().NoError(err)
	s.Nil(file)
}

func (s *SuiteDotGit) TestShallowWriteAndShallow() {
	fs := s.EmptyFS()

	dir := New(fs)

	f, err := dir.ShallowWriter()
	s.Require().NoError(err)

	_, err = f.Write([]byte("foo"))
	s.Require().NoError(err)

	f, err = dir.Shallow()
	s.Require().NoError(err)

	cnt, err := io.ReadAll(f)
	s.Require().NoError(err)

	s.Equal("foo", string(cnt))
}

func findReference(refs []*plumbing.Reference, name string) *plumbing.Reference {
	n := plumbing.ReferenceName(name)
	for _, ref := range refs {
		if ref.Name() == n {
			return ref
		}
	}

	return nil
}

func (s *SuiteDotGit) TestObjectPacks() {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	testObjectPacks(s, fs, dir, f)
}

func (s *SuiteDotGit) TestObjectPacksExclusive() {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjectPacks(s, fs, dir, f)
}

func testObjectPacks(s *SuiteDotGit, fs billy.Filesystem, dir *DotGit, f *fixtures.Fixture) {
	hashes, err := dir.ObjectPacks()
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal(plumbing.NewHash(f.PackfileHash), hashes[0])

	// Make sure that a random file in the pack directory doesn't
	// break everything.
	badFile, err := fs.Create("objects/pack/OOPS_THIS_IS_NOT_RIGHT.pack")
	s.Require().NoError(err)
	err = badFile.Close()
	s.Require().NoError(err)
	// temporary file generated by git gc
	tmpFile, err := fs.Create("objects/pack/.tmp-11111-pack-58rf8y4wm1b1k52bpe0kdlx6lpreg6ahso8n3ylc.pack")
	s.Require().NoError(err)
	err = tmpFile.Close()
	s.Require().NoError(err)

	hashes2, err := dir.ObjectPacks()
	s.Require().NoError(err)
	s.Len(hashes2, 1)
	s.Equal(hashes2[0], hashes[0])
}

func (s *SuiteDotGit) TestObjectPack() {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)
	s.Equal(".pack", filepath.Ext(pack.Name()))
}

func (s *SuiteDotGit) TestObjectPackWithKeepDescriptors() {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := NewWithOptions(fs, Options{KeepDescriptors: true})

	pack, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)
	s.Equal(".pack", filepath.Ext(pack.Name()))

	// Move to an specific offset
	pack.Seek(42, io.SeekStart)

	pack2, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)

	// If the file is the same the offset should be the same
	offset, err := pack2.Seek(0, io.SeekCurrent)
	s.Require().NoError(err)
	s.Equal(int64(42), offset)

	err = dir.Close()
	s.Require().NoError(err)

	pack2, err = dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)

	// If the file is opened again its offset should be 0
	offset, err = pack2.Seek(0, io.SeekCurrent)
	s.Require().NoError(err)
	s.Equal(int64(0), offset)

	err = pack2.Close()
	s.Require().NoError(err)

	err = dir.Close()
	s.NotNil(err)
}

func (s *SuiteDotGit) TestObjectPackIdx() {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	idx, err := dir.ObjectPackIdx(plumbing.NewHash(f.PackfileHash))
	s.Require().NoError(err)
	s.Equal(".idx", filepath.Ext(idx.Name()))
	s.Nil(idx.Close())
}

func TestOpenPackRevReadFromDisk(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)

	require.NoError(t, rev.Close())
}

func TestOpenPackRevIgnoreReverseIndex(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	// Overwrite the .rev with garbage.
	revPath := fmt.Sprintf("objects/pack/pack-%s.rev", h)
	corruptRevFile(t, fs, revPath, []byte("corrupt rev data"))

	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.NotEqual(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)
	require.NoError(t, rev.Close())

	// Re-open DotGit with ReadReverseIndex=false so the corrupt disk
	// file is ignored and a fresh rev is generated instead.
	dot = NewWithOptions(fs, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: true,
	})

	rev, err = dot.OpenPackRev(h)
	require.NoError(t, err)

	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr, "generated rev should have valid RIDX header")
	require.NoError(t, rev.Close())
}

func TestOpenPackRevNoRevFileOnDisk(t *testing.T) {
	t.Parallel()

	_, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	revPath := fmt.Sprintf("objects/pack/pack-%s.rev", h)
	_, err := fs.Stat(revPath)
	require.True(t, os.IsNotExist(err), ".rev file should not exist on disk")

	dot := NewWithOptions(fs, Options{ReadReverseIndex: false})
	rev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	var hdr [4]byte
	_, err = rev.ReadAt(hdr[:], 0)
	require.NoError(t, err)
	assert.Equal(t, [4]byte{'R', 'I', 'D', 'X'}, hdr)
	require.NoError(t, rev.Close())
}

func TestOpenRevFileDoesNotExist(t *testing.T) {
	t.Parallel()

	_, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	// ReadReverseIndex=true tries to open the .rev from disk — which
	// does not exist. Generate one on demand.
	dot := NewWithOptions(fs, Options{ReadReverseIndex: true})
	_, err := dot.OpenPackRev(h)
	require.NoError(t, err)
}

func TestOpenPackRevGeneratedMatchesDisk(t *testing.T) {
	t.Parallel()

	dot, h, fs := createPackWithRev(t, Options{
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})

	diskRev, err := dot.OpenPackRev(h)
	require.NoError(t, err)

	diskData, err := io.ReadAll(diskRev)
	require.NoError(t, err)
	require.NoError(t, diskRev.Close())

	genDot := NewWithOptions(fs, Options{ReadReverseIndex: false})
	genRev, err := genDot.OpenPackRev(h)
	require.NoError(t, err)

	genData, err := io.ReadAll(genRev)
	require.NoError(t, err)
	require.NoError(t, genRev.Close())

	assert.Equal(t, diskData, genData, "generated rev should be byte-identical to on-disk rev")
}

func TestOpenPackRevUsableByLazyIndex(t *testing.T) {
	t.Parallel()

	dot, h, _ := createPackWithRev(t, Options{
		ReadReverseIndex:  false,
		WriteReverseIndex: false,
	})

	openIdx := func() (idxfile.ReadAtCloser, error) {
		return dot.ObjectPackIdx(h)
	}
	openRev := func() (idxfile.ReadAtCloser, error) {
		return dot.OpenPackRev(h)
	}

	idx, err := idxfile.NewLazyIndex(openIdx, openRev, h)
	require.NoError(t, err)
	defer idx.Close()

	// Verify we can look up a known object by offset → hash (uses the rev).
	// First get an entry via the forward index to obtain a known offset.
	entries, err := idx.Entries()
	require.NoError(t, err)

	entry, err := entries.Next()
	require.NoError(t, err)
	require.NoError(t, entries.Close())

	// Use FindHash (reverse lookup) which relies on the rev data.
	got, err := idx.FindHash(int64(entry.Offset))
	require.NoError(t, err)
	assert.Equal(t, entry.Hash, got, "FindHash via generated rev should return the correct hash")
}

// createPackWithRev creates a packfile from the basic fixture and returns
// the DotGit, the pack hash, and the filesystem. The DotGit is created
// with the given options.
func createPackWithRev(t *testing.T, opts Options) (*DotGit, plumbing.Hash, billy.Filesystem) {
	t.Helper()

	f := fixtures.Basic().One()
	fs := osfs.New(t.TempDir())
	dot := NewWithOptions(fs, opts)
	require.NoError(t, dot.Initialize())

	w, err := dot.NewObjectPack()
	require.NoError(t, err)

	_, err = io.Copy(w, f.Packfile())
	require.NoError(t, err)
	require.NoError(t, w.Close())

	h := plumbing.NewHash(f.PackfileHash)
	return dot, h, fs
}

// corruptRevFile overwrites the file at path with the given data,
// handling the read-only permissions set by fixPermissions.
func corruptRevFile(t *testing.T, fs billy.Filesystem, path string, data []byte) {
	t.Helper()
	if chmodFS, ok := fs.(billy.Chmod); ok {
		require.NoError(t, chmodFS.Chmod(path, 0o644))
	}
	f, err := fs.Create(path)
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func (s *SuiteDotGit) TestObjectPackNotFound() {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.ZeroHash)
	s.ErrorIs(err, ErrPackfileNotFound)
	s.Nil(pack)

	idx, err := dir.ObjectPackIdx(plumbing.ZeroHash)
	s.ErrorIs(err, ErrPackfileNotFound)
	s.Nil(idx)
}

func (s *SuiteDotGit) TestNewObject() {
	fs := s.EmptyFS()

	dir := New(fs)
	w, err := dir.NewObject()
	s.Require().NoError(err)

	err = w.WriteHeader(plumbing.BlobObject, 14)
	s.Require().NoError(err)
	n, err := w.Write([]byte("this is a test"))
	s.Require().NoError(err)
	s.Equal(14, n)

	s.Equal("a8a940627d132695a9769df883f85992f0ff4a43", w.Hash().String())

	err = w.Close()
	s.Require().NoError(err)

	i, err := fs.Stat("objects/a8/a940627d132695a9769df883f85992f0ff4a43")
	s.Require().NoError(err)
	s.Equal(int64(34), i.Size())
}

func (s *SuiteDotGit) TestObjects() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	testObjects(s, fs, dir)
	testObjectsWithPrefix(s, fs, dir)
}

func (s *SuiteDotGit) TestObjectsExclusive() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjects(s, fs, dir)
	testObjectsWithPrefix(s, fs, dir)
}

func testObjects(s *SuiteDotGit, _ billy.Filesystem, dir *DotGit) {
	hashes, err := dir.Objects()
	s.Require().NoError(err)
	s.Len(hashes, 187)
	s.Equal("0097821d427a3c3385898eb13b50dcbc8702b8a3", hashes[0].String())
	s.Equal("01d5fa556c33743006de7e76e67a2dfcd994ca04", hashes[1].String())
	s.Equal("03db8e1fbe133a480f2867aac478fd866686d69e", hashes[2].String())
}

func testObjectsWithPrefix(s *SuiteDotGit, _ billy.Filesystem, dir *DotGit) {
	prefix, _ := hex.DecodeString("01d5")
	hashes, err := dir.ObjectsWithPrefix(prefix)
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal("01d5fa556c33743006de7e76e67a2dfcd994ca04", hashes[0].String())

	// Empty prefix should yield all objects.
	// (subset of testObjects)
	hashes, err = dir.ObjectsWithPrefix(nil)
	s.Require().NoError(err)
	s.Len(hashes, 187)
}

func (s *SuiteDotGit) TestObjectsNoFolder() {
	fs := s.EmptyFS()

	dir := New(fs)
	hash, err := dir.Objects()
	s.Require().NoError(err)
	s.Len(hash, 0)
}

func (s *SuiteDotGit) TestObject() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	s.Require().NoError(err)
	s.True(strings.HasSuffix(
		file.Name(), fs.Join("objects", "03", "db8e1fbe133a480f2867aac478fd866686d69e")),
	)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0o755))
	fs.Create(incomingFilePath)

	_, err = dir.Object(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestPreGit235Object() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	s.Require().NoError(err)
	s.True(strings.HasSuffix(
		file.Name(), fs.Join("objects", "03", "db8e1fbe133a480f2867aac478fd866686d69e")),
	)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0o755))
	fs.Create(incomingFilePath)

	_, err = dir.Object(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObjectStat() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	_, err := dir.ObjectStat(hash)
	s.Require().NoError(err)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0o755))
	fs.Create(incomingFilePath)

	_, err = dir.ObjectStat(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObjectDelete() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	err := dir.ObjectDelete(hash)
	s.Require().NoError(err)

	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingSubDirPath := fs.Join(incomingDirPath, incomingHash[0:2])
	incomingFilePath := fs.Join(incomingSubDirPath, incomingHash[2:40])

	err = fs.MkdirAll(incomingSubDirPath, os.FileMode(0o755))
	s.Require().NoError(err)

	f, err := fs.Create(incomingFilePath)
	s.Require().NoError(err)

	err = f.Close()
	s.Require().NoError(err)

	err = dir.ObjectDelete(plumbing.NewHash(incomingHash))
	s.Require().NoError(err)
}

func (s *SuiteDotGit) TestObjectNotFound() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("not-found-object")
	file, err := dir.Object(hash)
	s.NotNil(err)
	s.Nil(file)
}

func (s *SuiteDotGit) TestSubmodules() {
	fs := fixtures.ByTag("submodule").One().DotGit()
	dir := New(fs)

	m, err := dir.Module("basic")
	s.Require().NoError(err)
	s.True(strings.HasSuffix(m.Root(), m.Join(".git", "modules", "basic")))
}

func (s *SuiteDotGit) TestPackRefs() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err := dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(2, looseCount)

	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err := dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
	ref, err = dir.Ref("refs/heads/bar")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("a8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	// Now update one of them, re-pack, and check again.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)
	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 2)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err = dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("b8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())
}

func TestAlternatesDefault(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object.
	dotFS := osfs.New(t.TempDir())

	testAlternates(t, dotFS, dotFS)
}

func TestAlternatesWithFS(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object with a specific FS for alternates.
	altFS := osfs.New(t.TempDir())
	dotFS, _ := altFS.Chroot("repo2")

	testAlternates(t, dotFS, altFS)
}

func TestAlternatesWithBoundOS(t *testing.T) {
	t.Parallel()
	// Create a new dotgit object with a specific FS for alternates.
	altFS := osfs.New(t.TempDir(), osfs.WithBoundOS())
	dotFS, _ := altFS.Chroot("repo2")

	testAlternates(t, dotFS, altFS)
}

func testAlternates(t *testing.T, dotFS, altFS billy.Filesystem) {
	tests := []struct {
		name      string
		in        []string
		inWindows []string
		setup     func()
		wantErr   bool
		wantRoots []string
	}{
		{
			name: "no alternates",
		},
		{
			name:      "abs path",
			in:        []string{filepath.Join(altFS.Root(), "./repo1/.git/objects")},
			inWindows: []string{filepath.Join(altFS.Root(), ".\\repo1\\.git\\objects")},
			setup: func() {
				err := altFS.MkdirAll(filepath.Join("repo1", ".git", "objects"), 0o700)
				assert.NoError(t, err)
			},
			wantRoots: []string{filepath.Join("repo1", ".git")},
		},
		{
			name:      "rel path",
			in:        []string{"../../../repo3//.git/objects"},
			inWindows: []string{"..\\..\\..\\repo3\\.git\\objects"},
			setup: func() {
				err := altFS.MkdirAll(filepath.Join("repo3", ".git", "objects"), 0o700)
				assert.NoError(t, err)
			},
			wantRoots: []string{filepath.Join("repo3", ".git")},
		},
		{
			name:      "invalid abs path",
			in:        []string{"/alt/target2"},
			inWindows: []string{"\\alt\\target2"},
			wantErr:   true,
		},
		{
			name:      "invalid rel path",
			in:        []string{"../../../alt/target3"},
			inWindows: []string{"..\\..\\..\\alt\\target3"},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := NewWithOptions(dotFS, Options{AlternatesFS: altFS})
			err := dir.Initialize()
			assert.NoError(t, err)

			content := strings.Join(tc.in, "\n")
			if runtime.GOOS == "windows" {
				content = strings.Join(tc.inWindows, "\r\n")
			}

			// Create alternates file.
			altpath := dotFS.Join("objects", "info", "alternates")
			f, err := dotFS.Create(altpath)
			assert.NoError(t, err)
			f.Write([]byte(content))
			f.Close()

			if tc.setup != nil {
				tc.setup()
			}

			dotgits, err := dir.Alternates()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			for i, d := range dotgits {
				assert.Regexp(t, "^"+regexp.QuoteMeta(altFS.Root()), d.fs.Root())
				assert.Regexp(t, regexp.QuoteMeta(tc.wantRoots[i])+"$", d.fs.Root())
			}
		})
	}
}

func TestAlternatesDupes(t *testing.T) {
	t.Parallel()
	dotFS := osfs.New(t.TempDir())
	dir := New(dotFS)
	err := dir.Initialize()
	assert.NoError(t, err)

	path := filepath.Join(dotFS.Root(), "target3")
	dupes := []string{path, path, path, path, path}

	content := strings.Join(dupes, "\n")
	if runtime.GOOS == "windows" {
		content = strings.Join(dupes, "\r\n")
	}

	err = dotFS.MkdirAll("target3", 0o700)
	assert.NoError(t, err)

	// Create alternates file.
	altpath := dotFS.Join("objects", "info", "alternates")
	f, err := dotFS.Create(altpath)
	assert.NoError(t, err)
	f.Write([]byte(content))
	f.Close()

	dotgits, err := dir.Alternates()
	assert.NoError(t, err)
	assert.Len(t, dotgits, 1)
}

type norwfs struct {
	billy.Filesystem
}

func (f *norwfs) Capabilities() billy.Capability {
	return billy.Capabilities(f.Filesystem) &^ billy.ReadAndWriteCapability
}

func (s *SuiteDotGit) TestIncBytes() {
	tests := []struct {
		in       []byte
		out      []byte
		overflow bool
	}{
		{[]byte{0}, []byte{1}, false},
		{[]byte{0xff}, []byte{0}, true},
		{[]byte{7, 0xff}, []byte{8, 0}, false},
		{[]byte{0xff, 0xff}, []byte{0, 0}, true},
	}
	for _, test := range tests {
		out, overflow := incBytes(test.in)
		s.Equal(test.out, out)
		s.Equal(test.overflow, overflow)
	}
}

// this filesystem wrapper returns os.ErrNotExist if the file matches
// the provided paths list
type notExistsFS struct {
	billy.Filesystem

	paths []string
}

func (f *notExistsFS) matches(path string) bool {
	p := filepath.ToSlash(path)
	return slices.Contains(f.paths, p)
}

func (f *notExistsFS) Open(filename string) (billy.File, error) {
	if f.matches(filename) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.Open(filename)
}

func (f *notExistsFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if f.matches(path) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.ReadDir(path)
}

func (s *SuiteDotGit) TestDeletedRefs() {
	fs := s.EmptyFS()

	dir := New(&notExistsFS{
		Filesystem: fs,
		paths: []string{
			"refs/heads/bar",
			"refs/heads/baz",
		},
	})

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/baz/baz",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	s.Equal(plumbing.ReferenceName("refs/heads/foo"), refs[0].Name())
}

// Checks that setting a reference that has been packed and checking its old value is successful
func (s *SuiteDotGit) TestSetPackedRef() {
	fs := s.EmptyFS()

	dir := New(fs)

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	s.Require().NoError(err)

	refs, err := dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	looseCount, err := dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)

	err = dir.PackRefs()
	s.Require().NoError(err)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	s.Require().NoError(err)
	s.Len(refs, 1)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(0, looseCount)

	ref, err := dir.Ref("refs/heads/foo")
	s.Require().NoError(err)
	s.NotNil(ref)
	s.Equal("e8d3ffab552895c19b9fcf7aa264d277cde33881", ref.Hash().String())

	// Attempt to update the reference using an invalid old reference value
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33882",
	))
	s.ErrorIs(err, storage.ErrReferenceHasChanged)

	// Now update the reference and it should pass
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	))
	s.Require().NoError(err)
	looseCount, err = dir.CountLooseRefs()
	s.Require().NoError(err)
	s.Equal(1, looseCount)
}

func TestIssue55(t *testing.T) {
	t.Parallel()

	writeObject := func(fs billy.Filesystem) {
		t.Helper()

		dir := New(fs)
		err := dir.Initialize()
		require.NoError(t, err)

		w, err := dir.NewObject()
		require.NoError(t, err)

		err = w.WriteHeader(plumbing.BlobObject, 14)
		require.NoError(t, err)
		n, err := w.Write([]byte("this is a test"))
		require.NoError(t, err)
		assert.Equal(t, 14, n)

		assert.Equal(t, "a8a940627d132695a9769df883f85992f0ff4a43", w.Hash().String())

		err = w.Close()
		require.NoError(t, err)
	}

	for _, tc := range []struct {
		name string
		fs   billy.Filesystem
	}{
		{"BoundOS", osfs.New(t.TempDir(), osfs.WithBoundOS())},
		{"ChrootOS", osfs.New(t.TempDir(), osfs.WithChrootOS())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("objects", "a8", "a940627d132695a9769df883f85992f0ff4a43")

			writeObject(tc.fs)
			i, err := tc.fs.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, int64(34), i.Size())

			ro, err := isReadOnly(tc.fs, path)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", path)

			// Recreate the same object.
			writeObject(tc.fs)
			i, err = tc.fs.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, int64(34), i.Size())

			ro, err = isReadOnly(tc.fs, path)
			require.NoError(t, err)
			assert.True(t, ro, "file %q is not read-only", path)
		})
	}
}
