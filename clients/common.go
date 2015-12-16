package clients

import (
	"fmt"
	"net/url"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/clients/file"
	"gopkg.in/src-d/go-git.v2/clients/http"
	"gopkg.in/src-d/go-git.v2/clients/ssh"
)

// NewGitUploadPackService returns the appropiate upload pack service
// among of the set of supported protocols: HTTP, SSH or file.
// TODO: should this get a scheme as an argument instead of an URL?
func NewGitUploadPackService(repoURL string) (common.GitUploadPackService, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url %q", repoURL)
	}
	switch u.Scheme {
	case "http", "https":
		return http.NewGitUploadPackService(), nil
	case "ssh":
		return ssh.NewGitUploadPackService(), nil
	case "file":
		return file.NewGitUploadPackService(), nil
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
}
