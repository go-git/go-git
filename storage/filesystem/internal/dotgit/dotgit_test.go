package dotgit

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/fs"

	"github.com/alcortesm/tgz"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

var initFixtures = [...]struct {
	name string
	tgz  string
}{
	{
		name: "spinnaker",
		tgz:  "fixtures/spinnaker-gc.tgz",
	}, {
		name: "no-packfile-no-idx",
		tgz:  "fixtures/no-packfile-no-idx.tgz",
	}, {
		name: "empty",
		tgz:  "fixtures/empty-gitdir.tgz",
	}, {
		name: "unpacked",
		tgz:  "fixtures/unpacked-objects-no-packfile-no-idx.tgz",
	}, {
		name: "unpacked-dummy",
		tgz:  "fixtures/unpacked-objects-exist-one-dummy-object-no-packfile-no-idx.tgz",
	},
}

type SuiteDotGit struct {
	fixtures map[string]fs.Filesystem
}

var _ = Suite(&SuiteDotGit{})

func (s *SuiteDotGit) SetUpSuite(c *C) {
	s.fixtures = make(map[string]fs.Filesystem, len(initFixtures))

	for _, init := range initFixtures {
		com := Commentf("fixture name = %s\n", init.name)

		path, err := tgz.Extract(init.tgz)
		c.Assert(err, IsNil, com)

		s.fixtures[init.name] = fs.NewOSClient(filepath.Join(path, ".git"))
	}
}

func (s *SuiteDotGit) TearDownSuite(c *C) {
	for _, f := range s.fixtures {
		err := os.RemoveAll(f.Base())
		c.Assert(err, IsNil)
	}
}

func (s *SuiteDotGit) TestRefsFromPackedRefs(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/tags/v0.37.0")
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "85ec60477681933961c9b64c18ada93220650ac5")

}
func (s *SuiteDotGit) TestRefsFromReferenceFile(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/remotes/origin/master")

}

func (s *SuiteDotGit) TestRefsFromHEADFile(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/heads/master")
}

func (s *SuiteDotGit) TestConfig(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	file, err := dir.Config()
	c.Assert(err, IsNil)
	c.Assert(filepath.Base(file.Filename()), Equals, "config")
}

func findReference(refs []*core.Reference, name string) *core.Reference {
	n := core.ReferenceName(name)
	for _, ref := range refs {
		if ref.Name() == n {
			return ref
		}
	}

	return nil
}

func (s *SuiteDotGit) newFixtureDir(c *C, fixName string) *DotGit {
	f, ok := s.fixtures[fixName]
	c.Assert(ok, Equals, true)

	return New(f)
}

func (s *SuiteDotGit) TestObjectsPack(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	files, err := dir.ObjectsPacks()
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
}

func (s *SuiteDotGit) TestObjectsNoPackile(c *C) {
	dir := s.newFixtureDir(c, "no-packfile-no-idx")

	files, err := dir.ObjectsPacks()
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)
}

func (s *SuiteDotGit) TestObjectsPackFolderNotExists(c *C) {
	dir := s.newFixtureDir(c, "empty")

	files, err := dir.ObjectsPacks()
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)
}

func (s *SuiteDotGit) TestObjectPack(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	filename := "pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack"
	pack, idx, err := dir.ObjectPack(filename)
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(pack.Filename()), Equals, ".pack")
	c.Assert(filepath.Ext(idx.Filename()), Equals, ".idx")
}

func (s *SuiteDotGit) TestObjectPackNotFound(c *C) {
	dir := s.newFixtureDir(c, "spinnaker")

	filename := "pack-not-exists.pack"
	pack, idx, err := dir.ObjectPack(filename)
	c.Assert(err, Equals, ErrPackfileNotFound)
	c.Assert(pack, IsNil)
	c.Assert(idx, IsNil)
}

func (s *SuiteDotGit) TestObjects(c *C) {
	dir := s.newFixtureDir(c, "unpacked")

	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 3)
	c.Assert(hashes[0].String(), Equals, "1e0304e3cb54d0ad612ad70f1f15a285a65a4b8e")
	c.Assert(hashes[1].String(), Equals, "5efb9bc29c482e023e40e0a2b3b7e49cec842034")
	c.Assert(hashes[2].String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
}

func (s *SuiteDotGit) TestObjectsWithGarbage(c *C) {
	dir := s.newFixtureDir(c, "unpacked-dummy")

	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 3)
	c.Assert(hashes[0].String(), Equals, "1e0304e3cb54d0ad612ad70f1f15a285a65a4b8e")
	c.Assert(hashes[1].String(), Equals, "5efb9bc29c482e023e40e0a2b3b7e49cec842034")
	c.Assert(hashes[2].String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
}

func (s *SuiteDotGit) TestObjectsNoPackage(c *C) {
	dir := s.newFixtureDir(c, "empty")

	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 0)
}

func (s *SuiteDotGit) TestObjectsNoObjects(c *C) {
	dir := s.newFixtureDir(c, "no-packfile-no-idx")

	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 0)
}

func (s *SuiteDotGit) TestObject(c *C) {
	dir := s.newFixtureDir(c, "unpacked")

	hash := core.NewHash("1e0304e3cb54d0ad612ad70f1f15a285a65a4b8e")
	file, err := dir.Object(hash)
	c.Assert(err, IsNil)
	c.Assert(file.Filename(), Not(Equals), "")
}

func (s *SuiteDotGit) TestObjectNotFound(c *C) {
	dir := s.newFixtureDir(c, "unpacked")

	hash := core.NewHash("not-found-object")
	file, err := dir.Object(hash)
	c.Assert(err, NotNil)
	c.Assert(file, IsNil)
}

func (s *SuiteDotGit) TestNewObjectPack(c *C) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		log.Fatal(err)
	}

	dot := New(fs.NewOSClient(dir))

	r, err := os.Open("../../../../formats/packfile/fixtures/git-fixture.ofs-delta")
	c.Assert(err, IsNil)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	n, err := io.Copy(w, r)
	c.Assert(err, IsNil)
	c.Check(n, Equals, int64(85300))

	c.Assert(w.Close(), IsNil)
}
