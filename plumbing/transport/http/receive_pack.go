package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

// maxReceivePackContentLength matches Git v2.54.0's default HTTP push framing.
// http_post_buffer holds 16 packets of LARGE_PACKET_MAX (65520 bytes each).
// rpc_read_from_out requires one packet of free space before every read, even
// the final flush, leaving (16 - 1) * 65520 bytes for a fixed-length body.
// post_rpc uses unknown-length framing for larger requests. See:
// https://github.com/git/git/blob/v2.54.0/http.c
// https://github.com/git/git/blob/v2.54.0/pkt-line.h
// https://github.com/git/git/blob/v2.54.0/remote-curl.c
const maxReceivePackContentLength = 15 * 65520

type rpSession struct {
	*session
}

func newReceivePackSession(c *client, ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	s, err := newSession(c, ep, auth)
	return &rpSession{s}, err
}

func (s *rpSession) AdvertisedReferences() (*packp.AdvRefs, error) {
	return advertisedReferences(context.TODO(), s.session, transport.ReceivePackServiceName)
}

func (s *rpSession) AdvertisedReferencesContext(ctx context.Context) (*packp.AdvRefs, error) {
	return advertisedReferences(ctx, s.session, transport.ReceivePackServiceName)
}

func (s *rpSession) ReceivePack(ctx context.Context, req *packp.ReferenceUpdateRequest) (
	*packp.ReportStatus, error) {
	url := fmt.Sprintf(
		"%s/%s",
		s.endpoint.String(), transport.ReceivePackServiceName,
	)

	buf := bytes.NewBuffer(nil)
	if err := req.Encode(buf); err != nil {
		return nil, err
	}

	res, err := s.doRequest(ctx, http.MethodPost, url, buf)
	if err != nil {
		return nil, err
	}

	r, err := ioutil.NonEmptyReader(res.Body)
	if err == ioutil.ErrEmptyReader {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var d *sideband.Demuxer
	if req.Capabilities.Supports(capability.Sideband64k) {
		d = sideband.NewDemuxer(sideband.Sideband64k, r)
	} else if req.Capabilities.Supports(capability.Sideband) {
		d = sideband.NewDemuxer(sideband.Sideband, r)
	}
	if d != nil {
		d.Progress = req.Progress
		r = d
	}

	rc := ioutil.NewReadCloser(r, res.Body)

	report := packp.NewReportStatus()
	if err := report.Decode(rc); err != nil {
		return nil, err
	}

	return report, report.Error()
}

func (s *rpSession) doRequest(
	ctx context.Context, method, url string, content *bytes.Buffer,
) (*http.Response, error) {

	var body io.Reader
	if content != nil {
		body = content
	}

	req, err := newRequest(method, url, body)
	if err != nil {
		return nil, plumbing.NewPermanentError(err)
	}

	applyHeadersToRequest(req, content, s.endpoint.Host, transport.ReceivePackServiceName)
	if req.ContentLength > maxReceivePackContentLength {
		req.ContentLength = -1
		req.Header.Del("Content-Length")
	}
	s.ApplyAuthToRequest(req)

	res, err := s.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, plumbing.NewUnexpectedError(err)
	}

	if err := NewErr(res); err != nil {
		return nil, err
	}

	return res, nil
}
