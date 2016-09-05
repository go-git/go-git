package filesystem

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type StorageSuite struct{}

var _ = Suite(&StorageSuite{})
