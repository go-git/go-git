package transport

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/internal/repository"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type ServerInfoSuite struct {
	suite.Suite
}

func TestServerInfoSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServerInfoSuite))
}

func (s *ServerInfoSuite) TestUpdateServerInfoInit() {
	fs := memfs.New()
	st := memory.NewStorage()
	err := UpdateServerInfo(st, fs)
	s.NoError(err)
}

func (s *ServerInfoSuite) TestUpdateServerInfoTags() {
	fixture := fixtures.Basic().One()
	dotgit, err := fixture.DotGit()
	s.Require().NoError(err)
	st := filesystem.NewStorage(dotgit, nil)
	defer func() { _ = st.Close() }()
	fs := memfs.New()

	err = UpdateServerInfo(st, fs)
	s.NoError(err)
	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasic() {
	fixture := fixtures.Basic().One()
	dotgit, err := fixture.DotGit()
	s.Require().NoError(err)
	st := filesystem.NewStorage(dotgit, nil)
	defer func() { _ = st.Close() }()
	fs := memfs.New()

	err = UpdateServerInfo(st, fs)
	s.NoError(err)
	assertInfoRefs(s, st, fs)
	assertObjectPacks(s, st, fs)
}

func (s *ServerInfoSuite) TestUpdateServerInfoBasicChange() {
	fixture := fixtures.Basic().One()
	dotgit, err := fixture.DotGit()
	s.Require().NoError(err)
	st := filesystem.NewStorage(dotgit, nil)
	defer func() { _ = st.Close() }()
	fs := memfs.New()

	err = UpdateServerInfo(st, fs)
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

func assertInfoRefs(s *ServerInfoSuite, st storage.Storer, fs billy.Filesystem) {
	f, err := fs.Open("info/refs")
	s.NoError(err)
	defer f.Close()

	bts, err := io.ReadAll(f)
	s.NoError(err)

	localRefs := make(map[plumbing.ReferenceName]plumbing.Hash)
	for line := range strings.SplitSeq(string(bts), "\n") {
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
	f, err := fs.Open("objects/info/packs")
	s.NoError(err)
	defer f.Close()

	bts, err := io.ReadAll(f)
	s.NoError(err)

	pos, ok := st.(storer.PackedObjectStorer)
	s.True(ok)
	localPacks := make(map[string]struct{})
	packs, err := pos.ObjectPacks()
	s.NoError(err)

	for line := range strings.SplitSeq(string(bts), "\n") {
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

func (s *ServerInfoSuite) TestUpdateServerInfoFiltersReferenceNames() {
	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	valid := []plumbing.ReferenceName{"refs/heads/main", "refs/heads/@", "refs/heads/-foo", "refs/heads/\u200c./main"}
	invalid := []plumbing.ReferenceName{"refs/heads/main.lock", "refs/heads/bad\ninjected", "refs/heads/sp ace", "CONFIG", plumbing.HEAD}
	for _, name := range append(valid, invalid...) {
		s.Require().NoError(st.SetReference(plumbing.NewHashReference(name, hash)))
	}
	fs := memfs.New()
	s.Require().NoError(UpdateServerInfo(st, fs))
	f, err := fs.Open("info/refs")
	s.Require().NoError(err)
	defer f.Close()
	out, err := io.ReadAll(f)
	s.Require().NoError(err)
	for _, name := range valid {
		s.Contains(string(out), hash.String()+"\t"+name.String()+"\n")
	}
	for _, name := range invalid {
		s.NotContains(string(out), name.String())
	}
	iter, err := st.IterReferences()
	s.Require().NoError(err)
	count := 0
	s.Require().NoError(iter.ForEach(func(*plumbing.Reference) error { count++; return nil }))
	s.Equal(len(valid)+len(invalid), count)
}

func (s *ServerInfoSuite) TestUpdateServerInfoSkipsUnresolvableSymrefs() {
	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	s.Require().NoError(st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	s.Require().NoError(st.SetReference(plumbing.NewSymbolicReference("refs/heads/loop", "refs/heads/loop")))
	s.Require().NoError(st.SetReference(plumbing.NewSymbolicReference("refs/heads/missing", "refs/heads/absent")))
	s.Require().NoError(st.SetReference(plumbing.NewSymbolicReference("refs/heads/alias", "refs/heads/main")))
	fs := memfs.New()
	s.Require().NoError(UpdateServerInfo(st, fs))
	f, err := fs.Open("info/refs")
	s.Require().NoError(err)
	defer f.Close()
	out, err := io.ReadAll(f)
	s.Require().NoError(err)
	s.Contains(string(out), hash.String()+"\trefs/heads/main\n")
	s.Contains(string(out), hash.String()+"\trefs/heads/alias\n")
	s.NotContains(string(out), "refs/heads/loop")
	s.NotContains(string(out), "refs/heads/missing")
}

func (s *ServerInfoSuite) TestWriteInfoRefsPropagatesSymrefReadError() {
	st := memory.NewStorage()
	s.Require().NoError(st.SetReference(plumbing.NewSymbolicReference("refs/heads/alias", "refs/heads/main")))
	want := errors.New("reference storage unavailable")
	var out bytes.Buffer
	err := repository.WriteInfoRefs(&out, referenceReadErrorStorage{st, want})
	s.ErrorIs(err, want)
}

func (s *ServerInfoSuite) TestWriteInfoRefsSkipsFilesystemSymlinkLoop() {
	st := memory.NewStorage()
	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	s.Require().NoError(st.SetReference(plumbing.NewHashReference("refs/heads/main", hash)))
	s.Require().NoError(st.SetReference(plumbing.NewSymbolicReference("refs/heads/alias", "ORIG_HEAD")))
	broken := referenceReadErrorStorage{st, &os.PathError{Op: "stat", Path: "ORIG_HEAD", Err: syscall.ELOOP}}
	var out bytes.Buffer
	s.Require().NoError(repository.WriteInfoRefs(&out, broken))
	s.Equal(hash.String()+"\trefs/heads/main\n", out.String())
}
