package transport

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

func TestCommonSuite(t *testing.T) {
	suite.Run(t, new(CommonSuite))
}

type CommonSuite struct {
	suite.Suite
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForUnknownSource() {
	msg := "unknown system is complaining of something very sad :("

	isRepoNotFound := isRepoNotFoundError(msg)

	s.False(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundError() {
	msg := "no such repository : some error stuf"

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGithub() {
	msg := fmt.Sprintf("%s : some error stuf", githubRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForBitBucket() {
	msg := fmt.Sprintf("%s : some error stuf", bitbucketRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForLocal() {
	msg := fmt.Sprintf("some error stuf : %s", localRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolNotFound() {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolNoSuch() {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolNoSuchErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitProtocolAccessDenied() {
	msg := fmt.Sprintf("%s : some error stuf", gitProtocolAccessDeniedErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGogsAccessDenied() {
	msg := fmt.Sprintf("%s : some error stuf", gogsAccessDeniedErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestIsRepoNotFoundErrorForGitlab() {
	msg := fmt.Sprintf("%s : some error stuf", gitlabRepoNotFoundErr)

	isRepoNotFound := isRepoNotFoundError(msg)

	s.True(isRepoNotFound)
}

func (s *CommonSuite) TestCheckNotFoundError() {
	firstErrLine := make(chan string, 1)

	session := session{
		firstErrLine: firstErrLine,
	}

	firstErrLine <- ""

	err := session.checkNotFoundError()

	s.Nil(err)
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError() {
	var (
		stderr  = "something"
		wantErr = fmt.Errorf("unknown error: something")
	)

	client := NewClient(mockCommander{stderr: stderr})
	sess, err := client.NewUploadPackSession(nil, nil)
	if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.AdvertisedReferences()

	if wantErr != nil {
		if wantErr != err {
			if wantErr.Error() != err.Error() {
				s.T().Fatalf("expected a different error: got '%s', expected '%s'", err, wantErr)
			}
		}
	} else if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteNotFoundError() {
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
		s.T().Fatalf("unexpected error: %s", err)
	}

	_, err = sess.AdvertisedReferences()

	if wantErr != nil {
		if wantErr != err {
			if wantErr.Error() != err.Error() {
				s.T().Fatalf("expected a different error: got '%s', expected '%s'", err, wantErr)
			}
		}
	} else if err != nil {
		s.T().Fatalf("unexpected error: %s", err)
	}
}
