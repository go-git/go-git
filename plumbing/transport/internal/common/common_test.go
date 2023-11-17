package common

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestIsRepoNotFoundErrorForUnknownSource(c *C) {
	msg := "unknown system is complaining of something very sad :("

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, false)
}

func (s *CommonSuite) TestIsRepoNotFoundError(c *C) {
	msg := "no such repository : some error stuf"

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestCheckNotFoundError(c *C) {
	firstErrLine := make(chan string, 1)

	session := session{
		firstErrLine: firstErrLine,
	}

	firstErrLine <- ""

	err := session.checkNotFoundError()

	c.Assert(err, IsNil)
}

func TestAdvertisedReferencesWithRemoteError(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		wantErr error
	}{
		{
			name:    "unknown error",
			stderr:  "something",
			wantErr: fmt.Errorf("unknown error: something"),
		},
		{
			name: "GitLab: repository not found",
			stderr: `remote:
remote: ========================================================================
remote: 
remote: ERROR: The project you were looking for could not be found or you don't have permission to view it.

remote: 
remote: ========================================================================
remote:`,
			wantErr: transport.ErrRepositoryNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(MockCommander{stderr: tt.stderr})
			sess, err := client.NewUploadPackSession(nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			_, err = sess.AdvertisedReferences()

			if tt.wantErr != nil {
				if tt.wantErr != err {
					if tt.wantErr.Error() != err.Error() {
						t.Fatalf("expected a different error: got '%s', expected '%s'", err, tt.wantErr)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
		})
	}
}
