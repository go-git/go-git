package transport

import (
	"io"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
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
	s.NotNil(fs)
	s.NotNil(st)

	err := UpdateServerInfo(st, fs)
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
	fixture := fixtures.Basic().One()
	st := filesystem.NewStorage(fixture.DotGit(), nil)
	fs := memfs.New()

	err := UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasic() {
	fixture := fixtures.Basic().One()
	st := filesystem.NewStorage(fixture.DotGit(), nil)
	fs := memfs.New()

	err := UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasicChange() {
	fixture := fixtures.Basic().One()
	st := filesystem.NewStorage(fixture.DotGit(), nil)
	fs := memfs.New()

	err := UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)

	head, err := st.Reference(plumbing.HEAD)
	s.NoError(err)

	ref := plumbing.NewHashReference("refs/heads/my-branch", head.Hash())
	err = st.SetReference(ref)
	s.NoError(err)

	tag := plumbing.NewHashReference("refs/tags/test-tag", head.Hash())
	err = st.SetReference(tag)
	s.NoError(err)

	err = UpdateServerInfo(st, fs)
	s.NoError(err)

	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}
