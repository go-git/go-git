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

func (s *CommonSuite) TestIsRepoNotFoundError() {
	tests := []struct {
		name     string
		msg      string
		expected bool
	}{
		{
			name:     "unknown source",
			msg:      "unknown system is complaining of something very sad :(",
			expected: false,
		},
		{
			name:     "github",
			msg:      fmt.Sprintf("%s : some error stuf", githubRepoNotFoundErr),
			expected: true,
		},
		{
			name:     "bitbucket",
			msg:      fmt.Sprintf("%s : some error stuf", bitbucketRepoNotFoundErr),
			expected: true,
		},
		{
			name:     "local",
			msg:      fmt.Sprintf("some error stuf : %s", localRepoNotFoundErr),
			expected: true,
		},
		{
			name:     "git protocol not found",
			msg:      fmt.Sprintf("%s : some error stuf", gitProtocolNotFoundErr),
			expected: true,
		},
		{
			name:     "git protocol no such",
			msg:      fmt.Sprintf("%s : some error stuf", gitProtocolNoSuchErr),
			expected: true,
		},
		{
			name:     "git protocol access denied",
			msg:      fmt.Sprintf("%s : some error stuf", gitProtocolAccessDeniedErr),
			expected: true,
		},
		{
			name:     "gogs access denied",
			msg:      fmt.Sprintf("%s : some error stuf", gogsAccessDeniedErr),
			expected: true,
		},
		{
			name:     "gitlab",
			msg:      fmt.Sprintf("%s : some error stuf", gitlabRepoNotFoundErr),
			expected: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			isRepoNotFound := isRepoNotFoundError(tt.msg)
			s.Equal(tt.expected, isRepoNotFound)
		})
	}
}

func (s *CommonSuite) TestCheckNotFoundError() {
	firstErrLine := make(chan string, 1)

	session := session{
		firstErrLine: firstErrLine,
	}

	firstErrLine <- ""

	s.NoError(session.checkNotFoundError())
}

func (s *CommonSuite) TestAdvertisedReferencesWithRemoteUnknownError() {
	var (
		stderr  = "something"
		wantErr = fmt.Errorf("unknown error: something")
	)

	client := NewClient(mockCommander{stderr: stderr})
	sess, err := client.NewUploadPackSession(nil, nil)
	s.Require().NoError(err)

	_, err = sess.AdvertisedReferences()
	s.Equal(wantErr, err)
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
	s.Require().NoError(err)

	_, err = sess.AdvertisedReferences()
	s.Equal(wantErr, err)
}
