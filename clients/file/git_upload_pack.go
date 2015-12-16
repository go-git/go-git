package file

import (
	"io"

	"gopkg.in/src-d/go-git.v2/clients/common"
)

type GitUploadPackService struct{}

func NewGitUploadPackService() *GitUploadPackService {
	return &GitUploadPackService{}
}

func (s *GitUploadPackService) Connect(url common.Endpoint) error {
	return nil
}

func (s *GitUploadPackService) ConnectWithAuth(url common.Endpoint, auth common.AuthMethod) error {
	return nil
}

func (s *GitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	return nil, nil
}

func (s *GitUploadPackService) Fetch(r *common.GitUploadPackRequest) (io.ReadCloser, error) {
	return nil, nil
}
