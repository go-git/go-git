package serverinfo

import (
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"
	. "gopkg.in/check.v1"
)

type ServerInfoSuite struct{}

var _ = Suite(&ServerInfoSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *ServerInfoSuite) TestUpdateServerInfoInit(c *C) {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Init(st, fs)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	err = UpdateServerInfo(st, fs)
	c.Assert(err, IsNil)
}

func assertInfoRefs(c *C, st storage.Storer, fs billy.Filesystem) {
	refsFile, err := fs.Open("info/refs")
	c.Assert(err, IsNil)

	defer refsFile.Close()
	bts, err := io.ReadAll(refsFile)
	c.Assert(err, IsNil)

	localRefs := make(map[plumbing.ReferenceName]plumbing.Hash)
	for _, line := range strings.Split(string(bts), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		c.Assert(parts, HasLen, 2)
		hash := plumbing.NewHash(parts[0])
		name := plumbing.ReferenceName(parts[1])
		localRefs[name] = hash
	}

	refs, err := st.IterReferences()
	c.Assert(err, IsNil)

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name()
		hash := ref.Hash()
		switch ref.Type() {
		case plumbing.SymbolicReference:
			if name == plumbing.HEAD {
				return nil
			}
			ref, err := st.Reference(ref.Target())
			c.Assert(err, IsNil)
			hash = ref.Hash()
			fallthrough
		case plumbing.HashReference:
			h, ok := localRefs[name]
			c.Assert(ok, Equals, true)
			c.Assert(h, Equals, hash)
			if name.IsTag() {
				tag, err := object.GetTag(st, hash)
				if err == nil {
					t, ok := localRefs[name+"^{}"]
					c.Assert(ok, Equals, true)
					c.Assert(t, Equals, tag.Target)
				}
			}
		}
		return nil
	})

	c.Assert(err, IsNil)
}

func assertObjectPacks(c *C, st storage.Storer, fs billy.Filesystem) {
	infoPacks, err := fs.Open("objects/info/packs")
	c.Assert(err, IsNil)

	defer infoPacks.Close()
	bts, err := io.ReadAll(infoPacks)
	c.Assert(err, IsNil)

	pos, ok := st.(storer.PackedObjectStorer)
	c.Assert(ok, Equals, true)
	localPacks := make(map[string]struct{})
	packs, err := pos.ObjectPacks()
	c.Assert(err, IsNil)

	for _, line := range strings.Split(string(bts), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		c.Assert(parts, HasLen, 2)
		pack := strings.TrimPrefix(parts[1], "pack-")
		pack = strings.TrimSuffix(pack, ".pack")
		localPacks[pack] = struct{}{}
	}

	for _, p := range packs {
		_, ok := localPacks[p.String()]
		c.Assert(ok, Equals, true)
	}
}

func (s *ServerInfoSuite) TestUpdateServerInfoTags(c *C) {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().URL,
	})
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	err = UpdateServerInfo(st, fs)
	c.Assert(err, IsNil)

	assertInfoRefs(c, st, fs)
	assertObjectPacks(c, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasic(c *C) {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.Basic().One().URL,
	})
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	err = UpdateServerInfo(st, fs)
	c.Assert(err, IsNil)

	assertInfoRefs(c, st, fs)
	assertObjectPacks(c, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasicChange(c *C) {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.Basic().One().URL,
	})
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)

	err = UpdateServerInfo(st, fs)
	c.Assert(err, IsNil)

	assertInfoRefs(c, st, fs)
	assertObjectPacks(c, st, fs)

	head, err := r.Head()
	c.Assert(err, IsNil)

	ref := plumbing.NewHashReference("refs/heads/my-branch", head.Hash())
	err = r.Storer.SetReference(ref)
	c.Assert(err, IsNil)

	_, err = r.CreateTag("test-tag", head.Hash(), &git.CreateTagOptions{
		Message: "test-tag",
	})
	c.Assert(err, IsNil)

	err = UpdateServerInfo(st, fs)

	assertInfoRefs(c, st, fs)
	assertObjectPacks(c, st, fs)
}
