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
	"github.com/stretchr/testify/suite"
)

type ServerInfoSuite struct {
	suite.Suite
}

func TestServerInfoSuite(t *testing.T) {
	suite.Run(t, new(ServerInfoSuite))
}

func (s *ServerInfoSuite) TestUpdateServerInfoInit() {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Init(st, fs)
	s.NoError(err)
	s.NotNil(r)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)
}

func assertInfoRefs(s *ServerInfoSuite, st storage.Storer, fs billy.Filesystem) {
	refsFile, err := fs.Open("info/refs")
	s.NoError(err)

	defer refsFile.Close()
	bts, err := io.ReadAll(refsFile)
	s.NoError(err)

	localRefs := make(map[plumbing.ReferenceName]plumbing.Hash)
	for _, line := range strings.Split(string(bts), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		s.Len(parts, 2)
		hash := plumbing.NewHash(parts[0])
		name := plumbing.ReferenceName(parts[1])
		localRefs[name] = hash
	}

	refs, err := st.IterReferences()
	s.NoError(err)

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name()
		hash := ref.Hash()
		switch ref.Type() {
		case plumbing.SymbolicReference:
			if name == plumbing.HEAD {
				return nil
			}
			ref, err := st.Reference(ref.Target())
			s.NoError(err)
			hash = ref.Hash()
			fallthrough
		case plumbing.HashReference:
			h, ok := localRefs[name]
			s.True(ok)
			s.Equal(hash, h)
			if name.IsTag() {
				tag, err := object.GetTag(st, hash)
				if err == nil {
					t, ok := localRefs[name+"^{}"]
					s.True(ok)
					s.Equal(tag.Target, t)
				}
			}
		}
		return nil
	})

	s.NoError(err)
}

func assertObjectPacks(s *ServerInfoSuite, st storage.Storer, fs billy.Filesystem) {
	infoPacks, err := fs.Open("objects/info/packs")
	s.NoError(err)

	defer infoPacks.Close()
	bts, err := io.ReadAll(infoPacks)
	s.NoError(err)

	pos, ok := st.(storer.PackedObjectStorer)
	s.True(ok)
	localPacks := make(map[string]struct{})
	packs, err := pos.ObjectPacks()
	s.NoError(err)

	for _, line := range strings.Split(string(bts), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		s.Len(parts, 2)
		pack := strings.TrimPrefix(parts[1], "pack-")
		pack = strings.TrimSuffix(pack, ".pack")
		localPacks[pack] = struct{}{}
	}

	for _, p := range packs {
		_, ok := localPacks[p.String()]
		s.True(ok)
	}
}

func (s *ServerInfoSuite) TestUpdateServerInfoTags() {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().URL,
	})
	s.NoError(err)
	s.NotNil(r)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasic() {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.Basic().One().URL,
	})
	s.NoError(err)
	s.NotNil(r)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasicChange() {
	fs := memfs.New()
	st := memory.NewStorage()
	r, err := git.Clone(st, fs, &git.CloneOptions{
		URL: fixtures.Basic().One().URL,
	})
	s.NoError(err)
	s.NotNil(r)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)

	head, err := r.Head()
	s.NoError(err)

	ref := plumbing.NewHashReference("refs/heads/my-branch", head.Hash())
	err = r.Storer.SetReference(ref)
	s.NoError(err)

	_, err = r.CreateTag("test-tag", head.Hash(), &git.CreateTagOptions{
		Message: "test-tag",
	})
	s.NoError(err)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}
