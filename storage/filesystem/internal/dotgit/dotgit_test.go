package dotgit

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/utils/fs"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type SuiteDotGit struct {
	fixtures map[string]fs.Filesystem
}

var _ = Suite(&SuiteDotGit{})

func (s *SuiteDotGit) SetUpSuite(c *C) {
	fixtures.RootFolder = "../../../../fixtures"
}

func (s *SuiteDotGit) TearDownSuite(c *C) {
	for _, f := range s.fixtures {
		err := os.RemoveAll(f.Base())
		c.Assert(err, IsNil)
	}
}

func (s *SuiteDotGit) TestRefsFromPackedRefs(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/branch")
	c.Assert(ref, NotNil)
	c.Assert(ref.Hash().String(), Equals, "e8d3ffab552895c19b9fcf7aa264d277cde33881")

}
func (s *SuiteDotGit) TestRefsFromReferenceFile(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "refs/remotes/origin/HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/remotes/origin/master")

}

func (s *SuiteDotGit) TestRefsFromHEADFile(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	dir := New(fs)

	refs, err := dir.Refs()
	c.Assert(err, IsNil)

	ref := findReference(refs, "HEAD")
	c.Assert(ref, NotNil)
	c.Assert(ref.Type(), Equals, core.SymbolicReference)
	c.Assert(string(ref.Target()), Equals, "refs/heads/master")
}

func (s *SuiteDotGit) TestConfig(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	dir := New(fs)

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
	f := fixtures.Basic().ByTag(".git")
	fs := f.DotGit()
	dir := New(fs)

	hashes, err := dir.ObjectPacks()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 1)
	c.Assert(hashes[0], Equals, f.PackfileHash)
}

func (s *SuiteDotGit) TestObjectPack(c *C) {
	f := fixtures.Basic().ByTag(".git")
	fs := f.DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(f.PackfileHash)
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(pack.Filename()), Equals, ".pack")
}

func (s *SuiteDotGit) TestObjectPackIdx(c *C) {
	f := fixtures.Basic().ByTag(".git")
	fs := f.DotGit()
	dir := New(fs)

	idx, err := dir.ObjectPackIdx(f.PackfileHash)
	c.Assert(err, IsNil)
	c.Assert(filepath.Ext(idx.Filename()), Equals, ".idx")
}

func (s *SuiteDotGit) TestObjectPackNotFound(c *C) {
	fs := fixtures.Basic().ByTag(".git").DotGit()
	dir := New(fs)

	pack, err := dir.ObjectPack(core.ZeroHash)
	c.Assert(err, Equals, ErrPackfileNotFound)
	c.Assert(pack, IsNil)

	idx, err := dir.ObjectPackIdx(core.ZeroHash)
	c.Assert(idx, IsNil)
}

func (s *SuiteDotGit) TestObjects(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").DotGit()
	dir := New(fs)

	hashes, err := dir.Objects()
	c.Assert(err, IsNil)
	c.Assert(hashes, HasLen, 187)
	c.Assert(hashes[0].String(), Equals, "0097821d427a3c3385898eb13b50dcbc8702b8a3")
	c.Assert(hashes[1].String(), Equals, "01d5fa556c33743006de7e76e67a2dfcd994ca04")
	c.Assert(hashes[2].String(), Equals, "03db8e1fbe133a480f2867aac478fd866686d69e")
}

func (s *SuiteDotGit) TestObject(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").DotGit()
	dir := New(fs)

	hash := core.NewHash("03db8e1fbe133a480f2867aac478fd866686d69e")
	file, err := dir.Object(hash)
	c.Assert(err, IsNil)
	c.Assert(strings.HasSuffix(
		file.Filename(), "objects/03/db8e1fbe133a480f2867aac478fd866686d69e"),
		Equals, true,
	)
}

func (s *SuiteDotGit) TestObjectNotFound(c *C) {
	fs := fixtures.ByTag(".git").ByTag("unpacked").DotGit()
	dir := New(fs)

	hash := core.NewHash("not-found-object")
	file, err := dir.Object(hash)
	c.Assert(err, NotNil)
	c.Assert(file, IsNil)
}

func (s *SuiteDotGit) TestNewObjectPack(c *C) {
	f := fixtures.Basic().One()

	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir)

	fs := fs.NewOS(dir)
	dot := New(fs)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	_, err = io.Copy(w, f.Packfile())
	c.Assert(err, IsNil)

	c.Assert(w.Close(), IsNil)

	stat, err := fs.Stat(fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(84794))

	stat, err = fs.Stat(fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(1940))
}
