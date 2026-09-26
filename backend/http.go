package backend

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

type httpService struct {
	pattern *regexp.Regexp
	method  string
	handler func(b *Backend, w http.ResponseWriter, r *http.Request, repo, svc string)
	svc     string
}

var httpServices = []httpService{
	{regexp.MustCompile("(.*?)/HEAD$"), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile("(.*?)/info/refs$"), http.MethodGet, (*Backend).handleInfoRefs, ""},
	{regexp.MustCompile("(.*?)/objects/info/alternates$"), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile("(.*?)/objects/info/http-alternates$"), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile("(.*?)/objects/info/packs$"), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile("(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{38,62}$"), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile(`(.*?)/objects/pack/pack-[0-9a-f]{40,64}\.pack$`), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile(`(.*?)/objects/pack/pack-[0-9a-f]{40,64}\.idx$`), http.MethodGet, (*Backend).handleDumbRequest, ""},
	{regexp.MustCompile("(.*?)/git-upload-pack$"), http.MethodPost, (*Backend).handleServiceRPC, transport.UploadPackService},
	{regexp.MustCompile("(.*?)/git-receive-pack$"), http.MethodPost, (*Backend).handleServiceRPC, transport.ReceivePackService},
	{regexp.MustCompile("(.*?)/git-upload-archive$"), http.MethodPost, (*Backend).handleServiceRPC, transport.UploadArchiveService},
}

// ServeHTTP implements [http.Handler]. It supports the smart HTTP protocol
// only. The dumb protocol's requests are recognised so they can be denied,
// rather than answered as if the endpoint did not exist.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := strings.TrimPrefix(r.URL.Path, b.Prefix)
	for _, s := range httpServices {
		if m := s.pattern.FindStringSubmatch(urlPath); m != nil {
			if r.Method != s.method {
				renderStatusError(w, http.StatusMethodNotAllowed)
				return
			}

			repo := strings.TrimPrefix(m[1], "/")
			s.handler(b, w, r, repo, s.svc)
			return
		}
	}

	renderStatusError(w, http.StatusNotFound)
}

func (b *Backend) handleServiceRPC(w http.ResponseWriter, r *http.Request, repo, svc string) {
	version := r.Header.Get("Git-Protocol")
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))

	expectedContentType := strings.ToLower(fmt.Sprintf("application/x-git-%s-request", transport.ServiceName(svc)))
	if contentType != expectedContentType {
		renderStatusError(w, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", transport.ServiceName(svc)))
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var reader io.ReadCloser
	var err error
	switch r.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(r.Body)
		if err != nil {
			b.logf("error creating gzip reader: %v", err)
			renderStatusError(w, http.StatusInternalServerError)
			return
		}
		defer func() { _ = reader.Close() }()
	default:
		reader = r.Body
	}

	ep, err := transport.ParseURL(repo)
	if err != nil {
		b.logf("error parsing URL: %v", err)
		renderStatusError(w, http.StatusBadRequest)
		return
	}

	if !b.requireReceivePackAuth(w, r, svc) {
		return
	}

	frw := &flushResponseWriter{ResponseWriter: w, log: b.ErrorLog, chunkSize: defaultChunkSize}
	if err := b.Serve(r.Context(), reader, frw, &Request{
		URL:          ep,
		Service:      svc,
		GitProtocol:  version,
		StatelessRPC: true,
	}); err != nil {
		b.logf("error processing request: %v", err)
		if !frw.started.Load() {
			// Failure before any byte was written — the status line is still
			// ours, so surface a real error instead of an implicit 200.
			renderStatusError(w, http.StatusInternalServerError)
		}
		// Otherwise the body is already streaming: renderStatusError would race
		// the writer and cannot change the committed status.
		return
	}
}

func (b *Backend) handleInfoRefs(w http.ResponseWriter, r *http.Request, repo, _ string) {
	service := r.URL.Query().Get("service")
	if service == "" {
		// A request without a service is a dumb client's, and is denied the
		// same way as the static files it would go on to ask for.
		b.handleDumbRequest(w, r, repo, "")
		return
	}

	if service != transport.UploadPackService && service != transport.ReceivePackService {
		b.logf("unsupported service requested: %q", service)
		renderStatusError(w, http.StatusNotFound)
		return
	}

	if !b.requireReceivePackAuth(w, r, service) {
		return
	}

	ep, err := transport.ParseURL(repo)
	if err != nil {
		b.logf("error parsing URL: %v", err)
		renderStatusError(w, http.StatusBadRequest)
		return
	}

	version := r.Header.Get("Git-Protocol")

	hdrNocache(w)
	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", transport.ServiceName(service)))

	frw := &flushResponseWriter{ResponseWriter: w, log: b.ErrorLog, chunkSize: defaultChunkSize}
	if err := b.Serve(r.Context(), nil, frw, &Request{
		URL:           ep,
		Service:       service,
		GitProtocol:   version,
		AdvertiseRefs: true,
		StatelessRPC:  true,
	}); err != nil {
		b.logf("error processing request: %v", err)
		if !frw.started.Load() {
			// Advertisement failed before any byte was written — the headers set
			// above are not yet committed, so a real error status can still be
			// sent instead of an implicit 200.
			renderStatusError(w, http.StatusInternalServerError)
		}
		return
	}
}

// handleDumbRequest denies a request belonging to the dumb HTTP protocol. git
// http-backend serves those static files only while http.getanyfile is set,
// and answers 403 otherwise. Not serving them is the only mode this backend
// offers, so the denial is unconditional.
func (b *Backend) handleDumbRequest(w http.ResponseWriter, _ *http.Request, _, _ string) {
	b.logf("dumb HTTP protocol is not supported")
	renderStatusError(w, http.StatusForbidden)
}

func (b *Backend) requireReceivePackAuth(w http.ResponseWriter, r *http.Request, service string) bool {
	// For receive-pack, require authentication as a basic sanity check.
	if service == transport.ReceivePackService && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		b.logf("missing Authorization header for receive-pack service")
		renderStatusError(w, http.StatusUnauthorized)
		return false
	}
	return true
}

func renderStatusError(w http.ResponseWriter, code int) {
	http.Error(w, fmt.Sprintf("%d %s", code, http.StatusText(code)), code)
}

func hdrNocache(w http.ResponseWriter) {
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
}
