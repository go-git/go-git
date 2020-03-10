package filesystem

import (
	"io/ioutil"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/test"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
	dir string
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	storage := NewStorage(osfs.New(s.dir), cache.NewObjectLRUDefault())

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
	fis, err := ioutil.ReadDir(s.dir)
	c.Assert(err, IsNil)
	c.Assert(fis, HasLen, 0)
}

type StorageExclusiveSuite struct {
	StorageSuite
}

var _ = Suite(&StorageExclusiveSuite{})

func (s *StorageExclusiveSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	storage := NewStorageWithOptions(
		osfs.New(s.dir),
		cache.NewObjectLRUDefault(),
		Options{ExclusiveAccess: true})

	setUpTest(&s.StorageSuite, c, storage)
}
