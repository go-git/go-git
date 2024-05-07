package transport

import (
	"context"
	"errors"
	"fmt"

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError(c *C) {
	var (
		stderr  = "something"
		wantErr = fmt.Errorf("something")
	)

	client := NewTransport(mockCommander{stderr: stderr})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), false)
	c.Assert(err, NotNil)
	if wantErr != nil {
		if wantErr != err {
			if wantErr.Error() != err.Error() {
				c.Fatalf("expected a different error: got '%s', expected '%s'", err, wantErr)
			}
		}
	} else if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteNotFoundError(c *C) {
	var (
		stderr = `remote:
remote: ========================================================================
remote: 
remote: ERROR: The project you were looking for could not be found or you don't have permission to view it.

remote: 
remote: ========================================================================
remote:`
		wantErr *RemoteError
	)

	client := NewTransport(mockCommander{stderr: stderr})
	sess, err := client.NewSession(nil, nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.Handshake(context.TODO(), false)
	c.Assert(err, NotNil)
	if !errors.As(err, &wantErr) {
		c.Fatalf("expected a different error: got '%s', expected '%s'", err, wantErr)
	}
}
