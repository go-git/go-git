package filesystem

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/test"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
	dir string
	fs  billy.Filesystem
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	tmp, err := util.TempDir(osfs.Default, "", "go-git-filestystem-config")
	c.Assert(err, IsNil)

	s.dir = tmp
	s.fs = osfs.New(s.dir)
	storage := NewStorage(s.fs, cache.NewObjectLRUDefault())

	setUpTest(s, c, storage)
}

func setUpTest(s *StorageSuite, c *C, storage *Storage) {
	// ensure that right interfaces are implemented
	var _ storer.EncodedObjectStorer = storage
	var _ storer.IndexStorer = storage
	var _ storer.ReferenceStorer = storage
	var _ storer.ShallowStorer = storage
	var _ storer.DeltaObjectStorer = storage
	var _ storer.PackfileWriter = storage

	s.BaseStorageSuite = test.NewBaseStorageSuite(storage)
}

func (s *StorageSuite) TestFilesystem(c *C) {
	fs := memfs.New()
	storage := NewStorage(fs, cache.NewObjectLRUDefault())

	c.Assert(storage.Filesystem(), Equals, fs)
}

func (s *StorageSuite) TestNewStorageShouldNotAddAnyContentsToDir(c *C) {
	fis, err := s.fs.ReadDir("/")
	c.Assert(err, IsNil)
	c.Assert(fis, HasLen, 0)
}

type StorageExclusiveSuite struct {
	StorageSuite
}

var _ = Suite(&StorageExclusiveSuite{})

func (s *StorageExclusiveSuite) SetUpTest(c *C) {
	tmp, err := util.TempDir(osfs.Default, "", "go-git-filestystem-config")
	c.Assert(err, IsNil)

	s.dir = tmp
	s.fs = osfs.New(s.dir)

	storage := NewStorageWithOptions(
		s.fs,
		cache.NewObjectLRUDefault(),
		Options{ExclusiveAccess: true})

	setUpTest(&s.StorageSuite, c, storage)
}
