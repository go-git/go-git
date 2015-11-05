package clients

import (
	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/clients/http"
)

func NewGitUploadPackService() common.GitUploadPackService {
	return http.NewGitUploadPackService()
}
