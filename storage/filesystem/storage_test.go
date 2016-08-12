package filesystem

import (
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/dotgit"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct{}

var _ = Suite(&StorageSuite{})

func (s *StorageSuite) TestNewErrorNotFound(c *C) {
	fs := fs.NewOS()
	_, err := NewStorage(fs, "not_found/.git")
	c.Assert(err, Equals, dotgit.ErrNotFound)
}
