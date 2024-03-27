package transport

import (
	"fmt"

	. "gopkg.in/check.v1"
)

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

func (s *CommonSuite) TestIsRepoNotFoundErrorForGithub(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", githubRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForBitBucket(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", bitbucketRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForLocal(c *C) {
	msg := fmt.Sprintf("some error stuf : %s", localRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolNotFound(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolNoSuch(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolNoSuchErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolAccessDenied(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolAccessDeniedErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGogsAccessDenied(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", gogsAccessDeniedErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	c.Assert(isRepoNotFound, Equals, true)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitlab(c *C) {
	msg := fmt.Sprintf("%s : some error stuf", gitlabRepoNotFoundErr)

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

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError(c *C) {
	var (
		stderr  = "something"
		wantErr = fmt.Errorf("unknown error: something")
	)

	client := NewClient(mockCommander{stderr: stderr})
	sess, err := client.NewUploadPackSession(nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.AdvertisedReferences()

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
		wantErr = ErrRepositoryNotFound
	)

	client := NewClient(mockCommander{stderr: stderr})
	sess, err := client.NewUploadPackSession(nil, nil)
	if err != nil {
		c.Fatalf("unexpected error: %s", err)
	}

	_, err = sess.AdvertisedReferences()

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
