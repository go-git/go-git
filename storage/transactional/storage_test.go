package transactional

import (
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/sgnl-ai/go-git/plumbing"
	"github.com/sgnl-ai/go-git/plumbing/cache"
	"github.com/sgnl-ai/go-git/plumbing/storer"
	"github.com/sgnl-ai/go-git/storage"
	"github.com/sgnl-ai/go-git/storage/filesystem"
	"github.com/sgnl-ai/go-git/storage/memory"
	"github.com/sgnl-ai/go-git/storage/test"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
	temporal func() storage.Storer
}

var _ = Suite(&StorageSuite{
	temporal: func() storage.Storer {
		return memory.NewStorage()
	},
})

var _ = Suite(&StorageSuite{
	temporal: func() storage.Storer {
		fs := memfs.New()
		return filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	},
})

func (s *StorageSuite) SetUpTest(c *C) {
	base := memory.NewStorage()
	temporal := s.temporal()

	s.BaseStorageSuite = test.NewBaseStorageSuite(NewStorage(base, temporal))
}

func (s *StorageSuite) TestCommit(c *C) {
	base := memory.NewStorage()
	temporal := s.temporal()
	st := NewStorage(base, temporal)

	commit := base.NewEncodedObject()
	commit.SetType(plumbing.CommitObject)

	_, err := st.SetEncodedObject(commit)
	c.Assert(err, IsNil)

	ref := plumbing.NewHashReference("refs/a", commit.Hash())
	c.Assert(st.SetReference(ref), IsNil)

	err = st.Commit()
	c.Assert(err, IsNil)

	ref, err = base.Reference(ref.Name())
	c.Assert(err, IsNil)
	c.Assert(ref.Hash(), Equals, commit.Hash())

	obj, err := base.EncodedObject(plumbing.AnyObject, commit.Hash())
	c.Assert(err, IsNil)
	c.Assert(obj.Hash(), Equals, commit.Hash())
}

func (s *StorageSuite) TestTransactionalPackfileWriter(c *C) {
	base := memory.NewStorage()
	temporal := s.temporal()
	st := NewStorage(base, temporal)

	_, tmpOK := temporal.(storer.PackfileWriter)
	_, ok := st.(storer.PackfileWriter)
	c.Assert(ok, Equals, tmpOK)
}
