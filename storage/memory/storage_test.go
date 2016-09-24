package memory

import (
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/storage/test"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	storage := NewStorage()
	s.BaseStorageSuite = test.NewBaseStorageSuite(
		storage.ObjectStorage(),
		storage.ReferenceStorage(),
		storage.ConfigStorage(),
	)
}

func (s *StorageSuite) TestStorageObjectStorage(c *C) {
	storage := NewStorage()
	o := storage.ObjectStorage()
	e := storage.ObjectStorage()

	c.Assert(o == e, Equals, true)
}

func (s *StorageSuite) TestStorageReferenceStorage(c *C) {
	storage := NewStorage()
	o := storage.ReferenceStorage()
	e := storage.ReferenceStorage()

	c.Assert(o == e, Equals, true)
}

func (s *StorageSuite) TestStorageConfigStorage(c *C) {
	storage := NewStorage()
	o := storage.ConfigStorage()
	e := storage.ConfigStorage()

	c.Assert(o == e, Equals, true)
}
