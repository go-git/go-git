package transport

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ReceivePackSuite struct {
	suite.Suite
}

func TestReceivePackSuite(t *testing.T) {
	suite.Run(t, new(ReceivePackSuite))
}

func (s *ReceivePackSuite) TestReceivePackAdvertiseV0() {
	testAdvertise(s.T(), ReceivePack, "", false)
}

func (s *ReceivePackSuite) TestReceivePackAdvertiseV2() {
	// TODO: support version 2
	testAdvertise(s.T(), UploadPack, "version=2", false)
}

func (s *ReceivePackSuite) TestReceivePackAdvertiseV1() {
	buf := testAdvertise(s.T(), ReceivePack, "version=1", false)
	s.Containsf(buf.String(), "version 1", "advertisement should contain version 1")
}
