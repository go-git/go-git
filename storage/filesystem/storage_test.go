package filesystem

import (
	"testing"

	"gopkg.in/src-d/go-git.v4/storage/test"
	"gopkg.in/src-d/go-git.v4/utils/fs/os"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	path := c.MkDir()
	storage, err := NewStorage(os.New(path))
	c.Assert(err, IsNil)
	s.BaseStorageSuite = test.NewBaseStorageSuite(
		storage.ObjectStorage(),
		storage.ReferenceStorage(),
		storage.ConfigStorage(),
	)
}

func (s *StorageSuite) TestTxObjectStorageSetAndCommit(c *C) {
	c.Skip("tx not supported")
}

func (s *StorageSuite) TestTxObjectStorageSetAndRollback(c *C) {
	c.Skip("tx not supported")
}
