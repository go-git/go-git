package memory

import (
	"testing"

	"github.com/go-git/go-git/v5/storage/test"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct {
	test.BaseStorageSuite
}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) SetUpTest(c *C) {
	s.BaseStorageSuite = test.NewBaseStorageSuite(NewStorage())
}
