package filesystem

import (
	"testing"

	"gopkg.in/src-d/go-git.v4/storage/test"

	. "gopkg.in/check.v1"
	"srcd.works/go-billy.v1/osfs"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	storage, err := NewStorage(osfs.New(c.MkDir()))
	c.Assert(err, IsNil)

	s.BaseStorageSuite = test.NewBaseStorageSuite(storage)
}
