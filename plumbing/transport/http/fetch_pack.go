package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
)

type fetchPackSession struct {
	*session
	advRefsRun bool
}

func newFetchPackSession(c *http.Client,
	ep transport.Endpoint) transport.FetchPackSession {

	return &fetchPackSession{
		session: &session{
			auth:     basicAuthFromEndpoint(ep),
			client:   c,
			endpoint: ep,
		},
	}
}

func (s *fetchPackSession) AdvertisedReferences() (*transport.UploadPackInfo,
	error) {
	if s.advRefsRun {
		return nil, transport.ErrAdvertistedReferencesAlreadyCalled
	}

	defer func() { s.advRefsRun = true }()

	url := fmt.Sprintf(
		"%s/info/refs?service=%s",
		s.endpoint.String(), transport.UploadPackServiceName,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	s.applyAuthToRequest(req)
	s.applyHeadersToRequest(req, nil)
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized {
		return nil, transport.ErrAuthorizationRequired
	}

	i := transport.NewUploadPackInfo()
	if err := i.Decode(res.Body); err != nil {
		if err == packp.ErrEmpty {
			err = transport.ErrEmptyRemoteRepository
		}

		return nil, err
	}

	return i, nil
}

func (s *fetchPackSession) FetchPack(r *transport.UploadPackRequest) (io.ReadCloser, error) {
	if r.IsEmpty() {
		return nil, transport.ErrEmptyUploadPackRequest
	}

	url := fmt.Sprintf(
		"%s/%s",
		s.endpoint.String(), transport.UploadPackServiceName,
	)

	res, err := s.doRequest("POST", url, r.Reader())
	if err != nil {
		return nil, err
	}

	reader, err := ioutil.NonEmptyReader(res.Body)
	if err == ioutil.ErrEmptyReader || err == io.ErrUnexpectedEOF {
		return nil, transport.ErrEmptyUploadPackRequest
	}

	if err != nil {
		return nil, err
	}

	rc := ioutil.NewReadCloser(reader, res.Body)
	if err := discardResponseInfo(rc); err != nil {
		return nil, err
	}

	return rc, nil
}

// Close does nothing.
func (s *fetchPackSession) Close() error {
	return nil
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

func (s *fetchPackSession) doRequest(method, url string, content *strings.Reader) (*http.Response, error) {
	var body io.Reader
	if content != nil {
		body = content
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, plumbing.NewPermanentError(err)
	}

	s.applyHeadersToRequest(req, content)
	s.applyAuthToRequest(req)

	res, err := s.client.Do(req)
	if err != nil {
		return nil, plumbing.NewUnexpectedError(err)
	}

	if err := NewErr(res); err != nil {
		_ = res.Body.Close()
		return nil, err
	}

	return res, nil
}

func (s *fetchPackSession) applyHeadersToRequest(req *http.Request, content *strings.Reader) {
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
