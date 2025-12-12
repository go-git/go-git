package transport

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

func TestCommonSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(CommonSuite))
}

type CommonSuite struct {
	suite.Suite
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError() {
	stderr := "something"

	client := NewPackTransport(mockCommander{stderr: stderr})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), UploadPackService)
	s.Error(err)
}
