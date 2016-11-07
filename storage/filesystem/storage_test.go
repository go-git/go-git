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
	storage, err := NewStorage(os.New(c.MkDir()))
	c.Assert(err, IsNil)

	s.BaseStorageSuite = test.NewBaseStorageSuite(storage)
}
