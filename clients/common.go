package clients

import (
	"io"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/clients/http"
)

type GitUploadPackService interface {
	Connect(url common.Endpoint) error
	Info() (*common.GitUploadPackInfo, error)
	Fetch(r *common.GitUploadPackRequest) (io.ReadCloser, error)
}

func NewGitUploadPackService() GitUploadPackService {
	return http.NewGitUploadPackService()
}
