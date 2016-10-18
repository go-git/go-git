package http

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/src-d/go-git.v4/clients/common"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp/pktline"
)

// GitUploadPackService git-upoad-pack service over HTTP
type GitUploadPackService struct {
	client   *http.Client
	endpoint common.Endpoint
	auth     HTTPAuthMethod
}

// NewGitUploadPackService connects to a git-upload-pack service over HTTP, the
// auth is extracted from the URL, or can be provided using the SetAuth method
func NewGitUploadPackService(endpoint common.Endpoint) common.GitUploadPackService {
	s := &GitUploadPackService{
		client:   http.DefaultClient,
		endpoint: endpoint,
	}

	s.setBasicAuthFromEndpoint()
	return s
}

// Connect has not any effect, is here just for meet the interface
func (s *GitUploadPackService) Connect() error {
	return nil
}

func (s *GitUploadPackService) setBasicAuthFromEndpoint() {
	info := s.endpoint.User
	if info == nil {
		return
	}

	p, ok := info.Password()
	if !ok {
		return
	}

	u := info.Username()
	s.auth = NewBasicAuth(u, p)
}

// SetAuth sets the AuthMethod
func (s *GitUploadPackService) SetAuth(auth common.AuthMethod) error {
	httpAuth, ok := auth.(HTTPAuthMethod)
	if !ok {
		return common.ErrInvalidAuthMethod
	}

	s.auth = httpAuth
	return nil
}

// Info returns the references info and capabilities from the service
func (s *GitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	url := fmt.Sprintf(
		"%s/info/refs?service=%s",
		s.endpoint.String(), common.GitUploadPackServiceName,
	)

	res, err := s.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	i := common.NewGitUploadPackInfo()
	return i, i.Decode(pktline.NewScanner(res.Body))
}

// Fetch request and returns a reader to a packfile
func (s *GitUploadPackService) Fetch(r *common.GitUploadPackRequest) (io.ReadCloser, error) {
	url := fmt.Sprintf(
		"%s/%s",
		s.endpoint.String(), common.GitUploadPackServiceName,
	)

	res, err := s.doRequest("POST", url, r.Reader())
	if err != nil {
		return nil, err
	}

	reader := newBufferedReadCloser(res.Body)
	if _, err := reader.Peek(1); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, common.ErrEmptyGitUploadPack
		}

		return nil, err
	}

	if err := discardResponseInfo(reader); err != nil {
		return nil, err
	}

	return reader, nil
}

func discardResponseInfo(r io.Reader) error {
	s := pktline.NewScanner(r)
	for s.Scan() {
		if bytes.Equal(s.Bytes(), []byte{'N', 'A', 'K', '\n'}) {
			break
		}
	}

	return s.Err()
}

func (s *GitUploadPackService) doRequest(method, url string, content *strings.Reader) (*http.Response, error) {
	var body io.Reader
	if content != nil {
		body = content
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, core.NewPermanentError(err)
	}

	s.applyHeadersToRequest(req, content)
	s.applyAuthToRequest(req)

	res, err := s.client.Do(req)
	if err != nil {
		return nil, core.NewUnexpectedError(err)
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

func (s *GitUploadPackService) applyAuthToRequest(req *http.Request) {
	if s.auth == nil {
		return
	}

	s.auth.setAuth(req)
}

// Disconnect do nothing
func (s *GitUploadPackService) Disconnect() (err error) {
	return nil
}

type bufferedReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

func newBufferedReadCloser(r io.ReadCloser) *bufferedReadCloser {
	return &bufferedReadCloser{bufio.NewReader(r), r}
}

func (r *bufferedReadCloser) Close() error {
	return r.closer.Close()
}
