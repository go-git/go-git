package transport

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type UploadPackSuite struct {
	suite.Suite
}

func TestUploadPackSuite(t *testing.T) {
	suite.Run(t, new(UploadPackSuite))
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV0() {
	testAdvertise(s.T(), UploadPack, "", false)
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV2() {
	// TODO: support version 2
	testAdvertise(s.T(), UploadPack, "version=2", false)
}

func (s *UploadPackSuite) TestUploadPackAdvertiseV1() {
	buf := testAdvertise(s.T(), UploadPack, "version=1", false)
	s.Containsf(buf.String(), "version 1", "advertisement should contain version 1")
}
