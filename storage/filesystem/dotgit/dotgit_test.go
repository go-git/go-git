package dotgit

import (
	"bufio"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/grahambrooks/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteDotGit struct {
	fixtures.Suite
}

var _ = Suite(&SuiteDotGit{})

func (s *SuiteDotGit) TemporalFilesystem() (fs billy.Filesystem, clean func()) {
	fs = osfs.New(os.TempDir())
	path, err := util.TempDir(fs, "", "")
	if err != nil {
		panic(err)
	}

	fs, err = fs.Chroot(path)
	if err != nil {
		panic(err)
	}

	return fs, func() {
		util.RemoveAll(fs, path)
	}
}

func (s *SuiteDotGit) TestInitialize(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	err := dir.Initialize()
	c.Assert(err, IsNil)

	_, err = fs.Stat(fs.Join("objects", "info"))
	c.Assert(err, IsNil)

	_, err = fs.Stat(fs.Join("objects", "pack"))
	c.Assert(err, IsNil)

	_, err = fs.Stat(fs.Join("refs", "heads"))
	c.Assert(err, IsNil)

	_, err = fs.Stat(fs.Join("refs", "tags"))
	c.Assert(err, IsNil)
}

func (s *SuiteDotGit) TestSetRefs(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	testSetRefs(c, dir)
}

func (s *SuiteDotGit) TestSetRefsNorwfs(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(&norwfs{fs})

	testSetRefs(c, dir)
}

func (s *SuiteDotGit) TestRefsHeadFirst(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)
	refs, err := dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(len(refs), Not(Equals), 0)
	c.Assert(refs[0].Name().String(), Equals, "HEAD")
}

func testSetRefs(c *C, dir *DotGit) {
	firstFoo := plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	)
	err := dir.SetRef(firstFoo, nil)

	c.Assert(err, IsNil)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/symbolic",
		"ref: refs/heads/foo",
	), nil)

	c.Assert(err, IsNil)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"bar",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)

	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/feature/baz",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(refs, HasLen, 3)

	ref := findReference(refs, "refs/heads/foo")
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	ref = findReference(refs, "refs/heads/symbolic")
	c.Assert(ref, NotNil)
	c.Assert(ref.Target().String(), Equals, "refs/heads/foo")

	ref = findReference(refs, "bar")
	c.Assert(ref, IsNil)

	_, err = dir.readReferenceFile(".", "refs/heads/feature/baz")
	c.Assert(err, IsNil)

	_, err = dir.readReferenceFile(".", "refs/heads/feature")
	c.Assert(err, Equals, ErrIsDir)

	ref, err = dir.Ref("refs/heads/foo")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	ref, err = dir.Ref("refs/heads/symbolic")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Target().String(), Equals, "refs/heads/foo")

	ref, err = dir.Ref("bar")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

	// Check that SetRef with a non-nil `old` works.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	c.Assert(err, IsNil)

	// `firstFoo` is no longer the right `old` reference, so this
	// should fail.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	), firstFoo)
	c.Assert(err, NotNil)
}

func (s *SuiteDotGit) TestRefsFromPackedRefs(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/branch")
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func (s *SuiteDotGit) TestRefsFromReferenceFile(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/remotes/origin/master")
}

func BenchmarkRefMultipleTimes(b *testing.B) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	refname := plumbing.ReferenceName("refs/remotes/origin/branch")

	dir := New(fs)
	_, err := dir.Ref(refname)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	for i := 0; i < b.N; i++ {
		_, err := dir.Ref(refname)
		if err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFile(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/HEAD")
	err := dir.RemoveRef(name)
	c.Assert(err, IsNil)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, string(name))
	c.Assert(ref, IsNil)
}

func (s *SuiteDotGit) TestRemoveRefFromPackedRefs(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	name := plumbing.ReferenceName("refs/remotes/origin/master")
	err := dir.RemoveRef(name)
	c.Assert(err, IsNil)

	b, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	c.Assert(string(b), Equals, ""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"e8d3ffab552895c19b9fcf7aa264d277cde33881 refs/remotes/origin/branch\n")
}

func (s *SuiteDotGit) TestRemoveRefFromReferenceFileAndPackedRefs(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	// Make a ref file for a ref that's already in `packed-refs`.
	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/branch",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)

	// Make sure it only appears once in the refs list.
	refs, err := dir.Refs()
	c.Assert(err, IsNil)
	found := false
	for _, ref := range refs {
		if ref.Name() == "refs/remotes/origin/branch" {
			c.Assert(found, Equals, false)
			found = true
		}
	}

	name := plumbing.ReferenceName("refs/remotes/origin/branch")
	err = dir.RemoveRef(name)
	c.Assert(err, IsNil)

	b, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	c.Assert(string(b), Equals, ""+
		"# pack-refs with: peeled fully-peeled \n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/master\n"+
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/remotes/origin/master\n")

	refs, err = dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, string(name))
	c.Assert(ref, IsNil)
}

func (s *SuiteDotGit) TestRemoveRefNonExistent(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	before, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	c.Assert(err, IsNil)

	after, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	c.Assert(string(before), Equals, string(after))
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	brokenContent := "BROKEN STUFF REALLY BROKEN"

	err := util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0755))
	c.Assert(err, IsNil)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	c.Assert(err, NotNil)

	after, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	c.Assert(brokenContent, Equals, string(after))
}

func (s *SuiteDotGit) TestRemoveRefInvalidPackedRefs2(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	brokenContent := strings.Repeat("a", bufio.MaxScanTokenSize*2)

	err := util.WriteFile(fs, packedRefsPath, []byte(brokenContent), os.FileMode(0755))
	c.Assert(err, IsNil)

	name := plumbing.ReferenceName("refs/heads/nonexistent")
	err = dir.RemoveRef(name)
	c.Assert(err, NotNil)

	after, err := util.ReadFile(fs, packedRefsPath)
	c.Assert(err, IsNil)

	c.Assert(brokenContent, Equals, string(after))
}

func (s *SuiteDotGit) TestRefsFromHEADFile(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, plumbing.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/heads/master")
}

func (s *SuiteDotGit) TestConfig(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	file, err := dir.Config()
	c.Assert(err, IsNil)
	c.Assert(filepath.Base(file.Name()), Equals, "config")
}

func (s *SuiteDotGit) TestConfigWriteAndConfig(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	f, err := dir.ConfigWriter()
	c.Assert(err, IsNil)

	_, err = f.Write([]byte("foo"))
	c.Assert(err, IsNil)

	f, err = dir.Config()
	c.Assert(err, IsNil)

	cnt, err := io.ReadAll(f)
	c.Assert(err, IsNil)

	c.Assert(string(cnt), Equals, "foo")
}

func (s *SuiteDotGit) TestIndex(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	idx, err := dir.Index()
	c.Assert(err, IsNil)
	c.Assert(idx, NotNil)
}

func (s *SuiteDotGit) TestIndexWriteAndIndex(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	f, err := dir.IndexWriter()
	c.Assert(err, IsNil)

	_, err = f.Write([]byte("foo"))
	c.Assert(err, IsNil)

	f, err = dir.Index()
	c.Assert(err, IsNil)

	cnt, err := io.ReadAll(f)
	c.Assert(err, IsNil)

	c.Assert(string(cnt), Equals, "foo")
}

func (s *SuiteDotGit) TestShallow(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	file, err := dir.Shallow()
	c.Assert(err, IsNil)
	c.Assert(file, IsNil)
}

func (s *SuiteDotGit) TestShallowWriteAndShallow(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	f, err := dir.ShallowWriter()
	c.Assert(err, IsNil)

	_, err = f.Write([]byte("foo"))
	c.Assert(err, IsNil)

	f, err = dir.Shallow()
	c.Assert(err, IsNil)

	cnt, err := io.ReadAll(f)
	c.Assert(err, IsNil)

	c.Assert(string(cnt), Equals, "foo")
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

func (s *SuiteDotGit) TestObjectPacks(c *C) {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	testObjectPacks(c, fs, dir, f)
}

func (s *SuiteDotGit) TestObjectPacksExclusive(c *C) {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjectPacks(c, fs, dir, f)
}

func testObjectPacks(c *C, fs billy.Filesystem, dir *DotGit, f *fixtures.Fixture) {
	hashes, err := dir.ObjectPacks()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 1)
	c.Assert(hashes[0], Equals, plumbing.NewHash(f.PackfileHash))

	// Make sure that a random file in the pack directory doesn't
	// break everything.
	badFile, err := fs.Create("objects/pack/OOPS_THIS_IS_NOT_RIGHT.pack")
	c.Assert(err, IsNil)
	err = badFile.Close()
	c.Assert(err, IsNil)
	// temporary file generated by git gc
	tmpFile, err := fs.Create("objects/pack/.tmp-11111-pack-58rf8y4wm1b1k52bpe0kdlx6lpreg6ahso8n3ylc.pack")
	c.Assert(err, IsNil)
	err = tmpFile.Close()
	c.Assert(err, IsNil)

	hashes2, err := dir.ObjectPacks()
	c.Assert(err, IsNil)
	c.Assert(hashes2, HasLen, 1)
	c.Assert(hashes[0], Equals, hashes2[0])
}

func (s *SuiteDotGit) TestObjectPack(c *C) {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(pack.Name()), Equals, ".pack")
}

func (s *SuiteDotGit) TestObjectPackWithKeepDescriptors(c *C) {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := NewWithOptions(fs, Options{KeepDescriptors: true})

	pack, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(pack.Name()), Equals, ".pack")

	// Move to an specific offset
	pack.Seek(42, io.SeekStart)

	pack2, err := dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	c.Assert(err, IsNil)

	// If the file is the same the offset should be the same
	offset, err := pack2.Seek(0, io.SeekCurrent)
	c.Assert(err, IsNil)
	c.Assert(offset, Equals, int64(42))

	err = dir.Close()
	c.Assert(err, IsNil)

	pack2, err = dir.ObjectPack(plumbing.NewHash(f.PackfileHash))
	c.Assert(err, IsNil)

	// If the file is opened again its offset should be 0
	offset, err = pack2.Seek(0, io.SeekCurrent)
	c.Assert(err, IsNil)
	c.Assert(offset, Equals, int64(0))

	err = pack2.Close()
	c.Assert(err, IsNil)

	err = dir.Close()
	c.Assert(err, NotNil)
}

func (s *SuiteDotGit) TestObjectPackIdx(c *C) {
	f := fixtures.Basic().ByTag(".git").One()
	fs := f.DotGit()
	dir := New(fs)

	idx, err := dir.ObjectPackIdx(plumbing.NewHash(f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(idx.Name()), Equals, ".idx")
	c.Assert(idx.Close(), IsNil)
}

func (s *SuiteDotGit) TestObjectPackNotFound(c *C) {
	fs := fixtures.Basic().ByTag(".git").One().DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(plumbing.ZeroHash)
	c.Assert(err, Equals, ErrPackfileNotFound)
	c.Assert(pack, IsNil)

	idx, err := dir.ObjectPackIdx(plumbing.ZeroHash)
	c.Assert(err, Equals, ErrPackfileNotFound)
	c.Assert(idx, IsNil)
}

func (s *SuiteDotGit) TestNewObject(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)
	w, err := dir.NewObject()
	c.Assert(err, IsNil)

	err = w.WriteHeader(plumbing.BlobObject, 14)
	c.Assert(err, IsNil)
	n, err := w.Write([]byte("this is a test"))
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 14)

	c.Assert(w.Hash().String(), Equals, "a8a940627d132695a9769df883f85992f0ff4a43")

	err = w.Close()
	c.Assert(err, IsNil)

	i, err := fs.Stat("objects/a8/a940627d132695a9769df883f85992f0ff4a43")
	c.Assert(err, IsNil)
	c.Assert(i.Size(), Equals, int64(34))
}

func (s *SuiteDotGit) TestObjects(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	testObjects(c, fs, dir)
	testObjectsWithPrefix(c, fs, dir)
}

func (s *SuiteDotGit) TestObjectsExclusive(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := NewWithOptions(fs, Options{ExclusiveAccess: true})

	testObjects(c, fs, dir)
	testObjectsWithPrefix(c, fs, dir)
}

func testObjects(c *C, fs billy.Filesystem, dir *DotGit) {
	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 187)
	c.Assert(hashes[0].String(), Equals, "0097821d427a3c3385898eb13b50dcbc8702b8a3")
	c.Assert(hashes[1].String(), Equals, "01d5fa556c33743006de7e76e67a2dfcd994ca04")
	c.Assert(hashes[2].String(), Equals, "03db8e1fbe133a480f2867aac478fd866686d69e")
}

func testObjectsWithPrefix(c *C, fs billy.Filesystem, dir *DotGit) {
	prefix, _ := hex.DecodeString("01d5")
	hashes, err := dir.ObjectsWithPrefix(prefix)
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 1)
	c.Assert(hashes[0].String(), Equals, "01d5fa556c33743006de7e76e67a2dfcd994ca04")

	// Empty prefix should yield all objects.
	// (subset of testObjects)
	hashes, err = dir.ObjectsWithPrefix(nil)
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 187)
}

func (s *SuiteDotGit) TestObjectsNoFolder(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)
	hash, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hash, HasLen, 0)
}

func (s *SuiteDotGit) TestObject(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	c.Assert(err, IsNil)
	c.Assert(strings.HasSuffix(
		file.Name(), fs.Join("objects", "03", "db8e1fbe133a480f2867aac478fd866686d69e")),
		Equals, true,
	)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0755))
	fs.Create(incomingFilePath)

	_, err = dir.Object(plumbing.NewHash(incomingHash))
	c.Assert(err, IsNil)
}

func (s *SuiteDotGit) TestPreGit235Object(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	c.Assert(err, IsNil)
	c.Assert(strings.HasSuffix(
		file.Name(), fs.Join("objects", "03", "db8e1fbe133a480f2867aac478fd866686d69e")),
		Equals, true,
	)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0755))
	fs.Create(incomingFilePath)

	_, err = dir.Object(plumbing.NewHash(incomingHash))
	c.Assert(err, IsNil)
}

func (s *SuiteDotGit) TestObjectStat(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	_, err := dir.ObjectStat(hash)
	c.Assert(err, IsNil)
	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingFilePath := fs.Join(incomingDirPath, incomingHash[0:2], incomingHash[2:40])
	fs.MkdirAll(incomingDirPath, os.FileMode(0755))
	fs.Create(incomingFilePath)

	_, err = dir.ObjectStat(plumbing.NewHash(incomingHash))
	c.Assert(err, IsNil)
}

func (s *SuiteDotGit) TestObjectDelete(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	err := dir.ObjectDelete(hash)
	c.Assert(err, IsNil)

	incomingHash := "9d25e0f9bde9f82882b49fe29117b9411cb157b7" // made up hash
	incomingDirPath := fs.Join("objects", "tmp_objdir-incoming-123456")
	incomingSubDirPath := fs.Join(incomingDirPath, incomingHash[0:2])
	incomingFilePath := fs.Join(incomingSubDirPath, incomingHash[2:40])

	err = fs.MkdirAll(incomingSubDirPath, os.FileMode(0755))
	c.Assert(err, IsNil)

	f, err := fs.Create(incomingFilePath)
	c.Assert(err, IsNil)

	err = f.Close()
	c.Assert(err, IsNil)

	err = dir.ObjectDelete(plumbing.NewHash(incomingHash))
	c.Assert(err, IsNil)
}

func (s *SuiteDotGit) TestObjectNotFound(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	dir := New(fs)

	hash := plumbing.NewHash("not-found-object")
	file, err := dir.Object(hash)
	c.Assert(err, NotNil)
	c.Assert(file, IsNil)
}

func (s *SuiteDotGit) TestSubmodules(c *C) {
	fs := fixtures.ByTag("submodule").One().DotGit()
	dir := New(fs)

	m, err := dir.Module("basic")
	c.Assert(err, IsNil)
	c.Assert(strings.HasSuffix(m.Root(), m.Join(".git", "modules", "basic")), Equals, true)
}

func (s *SuiteDotGit) TestPackRefs(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dir := New(fs)

	err := dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"e8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(refs, HasLen, 2)
	looseCount, err := dir.CountLooseRefs()
	c.Assert(err, IsNil)
	c.Assert(looseCount, Equals, 2)

	err = dir.PackRefs()
	c.Assert(err, IsNil)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(refs, HasLen, 2)
	looseCount, err = dir.CountLooseRefs()
	c.Assert(err, IsNil)
	c.Assert(looseCount, Equals, 0)

	ref, err := dir.Ref("refs/heads/foo")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")
	ref, err = dir.Ref("refs/heads/bar")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "a8d3ffab552895c19b9fcf7aa264d277cde33881")

	// Now update one of them, re-pack, and check again.
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/foo",
		"b8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)
	looseCount, err = dir.CountLooseRefs()
	c.Assert(err, IsNil)
	c.Assert(looseCount, Equals, 1)
	err = dir.PackRefs()
	c.Assert(err, IsNil)

	// Make sure the refs are still there, but no longer loose.
	refs, err = dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(refs, HasLen, 2)
	looseCount, err = dir.CountLooseRefs()
	c.Assert(err, IsNil)
	c.Assert(looseCount, Equals, 0)

	ref, err = dir.Ref("refs/heads/foo")
	c.Assert(err, IsNil)
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "b8d3ffab552895c19b9fcf7aa264d277cde33881")
}

func TestAlternatesDefault(t *testing.T) {
	// Create a new dotgit object.
	dotFS := osfs.New(t.TempDir())

	testAlternates(t, dotFS, dotFS)
}

func TestAlternatesWithFS(t *testing.T) {
	// Create a new dotgit object with a specific FS for alternates.
	altFS := osfs.New(t.TempDir())
	dotFS, _ := altFS.Chroot("repo2")

	testAlternates(t, dotFS, altFS)
}

func TestAlternatesWithBoundOS(t *testing.T) {
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

func (s *SuiteDotGit) TestIncBytes(c *C) {
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
		c.Assert(out, DeepEquals, test.out)
		c.Assert(overflow, Equals, test.overflow)
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
	for _, n := range f.paths {
		if p == n {
			return true
		}
	}
	return false
}

func (f *notExistsFS) Open(filename string) (billy.File, error) {
	if f.matches(filename) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.Open(filename)
}

func (f *notExistsFS) ReadDir(path string) ([]os.FileInfo, error) {
	if f.matches(path) {
		return nil, os.ErrNotExist
	}

	return f.Filesystem.ReadDir(path)
}

func (s *SuiteDotGit) TestDeletedRefs(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

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
	c.Assert(err, IsNil)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/bar",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)
	err = dir.SetRef(plumbing.NewReferenceFromStrings(
		"refs/heads/baz/baz",
		"a8d3ffab552895c19b9fcf7aa264d277cde33881",
	), nil)
	c.Assert(err, IsNil)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)
	c.Assert(refs, HasLen, 1)
	c.Assert(refs[0].Name(), Equals, plumbing.ReferenceName("refs/heads/foo"))
}
