package http

import (
	"io"
	"net/http"
	"strings"

	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/pktline"
)

type GitUploadPackService struct {
	Client *http.Client

	endpoint common.Endpoint
}

func NewGitUploadPackService() *GitUploadPackService {
	return &GitUploadPackService{
		Client: http.DefaultClient,
	}
}

func (s *GitUploadPackService) Connect(url common.Endpoint) error {
	s.endpoint = url

	return nil
}

func (s *GitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	res, err := s.doRequest("GET", common.GitUploadPackServiceName, nil)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	dec := pktline.NewDecoder(res.Body)
	return common.NewGitUploadPackInfo(dec)
}

func (s *GitUploadPackService) doRequest(method, service string, content *strings.Reader) (*http.Response, error) {
	var body io.Reader
	if content != nil {
		body = content
	}

	req, err := http.NewRequest(method, s.endpoint.Service(service), body)
	if err != nil {
		return nil, err
	}

	s.applyHeadersToRequest(req, content)

	res, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if err := NewHTTPError(res); err != nil {
		return nil, err
	}

	return res, nil
}

func (s *GitUploadPackService) applyHeadersToRequest(req *http.Request, content *strings.Reader) {
	req.Header.Add("User-Agent", "git/1.0")
	req.Header.Add("Host", "github.com")

	if content == nil {
		req.Header.Add("Accept", "*/*")
	} else {
		req.Header.Add("Accept", "application/x-git-upload-pack-result")
		req.Header.Add("Content-Type", "application/x-git-upload-pack-request")
		req.Header.Add("Content-Length", string(content.Len()))
	}
}
