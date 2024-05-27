package transport

import (
	"context"
	"errors"
	"io"

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError(c *C) {
	stderr := "something"
	client := NewPackTransport(mockCommander{stderr: stderr})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), false)
	c.Assert(err, NotNil)
	if !errors.Is(err, io.EOF) {
		c.Fatalf("unexpected error: %s", err)
	}
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteNotFoundError(c *C) {
	stderr := `remote:
remote: ========================================================================
remote: 
remote: ERROR: The project you were looking for could not be found or you don't have permission to view it.

remote: 
remote: ========================================================================
remote:`

	client := NewPackTransport(mockCommander{stderr: stderr})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), false)
	c.Assert(err, NotNil)
	if !errors.Is(err, io.EOF) {
		c.Fatalf("expected a different error: got '%s', expected '%s'", err, io.EOF)
	}
}
