package clients

import (
	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/clients/http"
)

type GitUploadPackService interface {
	Connect(url common.Endpoint) error
	Info() (*common.GitUploadPackInfo, error)
}

func NewGitUploadPackService() GitUploadPackService {
	return http.NewGitUploadPackService()
}
