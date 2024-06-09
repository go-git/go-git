package file

import (
	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type CommonSuite struct {
	fixtures.Suite
	ReceivePackBin string
	UploadPackBin  string
	tmpDir         string // to be removed at teardown
}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) SetUpSuite(c *C) {
}

func (s *CommonSuite) TearDownSuite(c *C) {
	s.Suite.TearDownSuite(c)
}
