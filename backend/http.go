package backend

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/transport"
)

var (
	// ErrUnauthorized is returned by [Backend.Authorize] when the request
	// carries no valid credentials. ServeHTTP answers it with 401 and a
	// WWW-Authenticate challenge, so the client asks for credentials.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned by [Backend.Authorize] when the request is
	// authenticated but may not use the service. ServeHTTP answers it
	// with 403.
	ErrForbidden = errors.New("forbidden")
)

// authChallenge is the WWW-Authenticate value sent with a 401. git asks its
// credential helpers for any challenge it receives.
const authChallenge = `Basic realm="git"`

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
//
// ServeHTTP does no authentication on its own. It follows git
// http-backend's service policy: upload-pack is served unless disabled,
// and receive-pack and upload-archive are served when enabled, or when
// [Backend.Authorize] accepts the request. Without Authorize, a push is
// refused with 403 unless [Backend.ReceivePack] is [ServiceEnabled].
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath, ok := b.trimPrefix(r.URL.Path)
	if !ok {
		renderStatusError(w, http.StatusNotFound)
		return
	}
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

// trimPrefix strips b.Prefix from urlPath. The prefix matches whole path
// segments only, so "/repo" matches "/repo/x.git" but not "/repository".
// It reports false when urlPath is outside the prefix.
func (b *Backend) trimPrefix(urlPath string) (string, bool) {
	prefix := strings.TrimSuffix(b.Prefix, "/")
	if prefix == "" {
		return urlPath, true
	}
	rest, ok := strings.CutPrefix(urlPath, prefix)
	if !ok || (rest != "" && rest[0] != '/') {
		return "", false
	}
	return rest, true
}

func (b *Backend) handleServiceRPC(w http.ResponseWriter, r *http.Request, repo, svc string) {
	if !b.authorizeHTTP(w, r, svc) {
		return
	}

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

	frw := &flushResponseWriter{ResponseWriter: w, log: b.ErrorLog, chunkSize: defaultChunkSize}
	if err := b.serve(r.Context(), reader, frw, &Request{
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

	if !b.authorizeHTTP(w, r, service) {
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
	if err := b.serve(r.Context(), nil, frw, &Request{
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

// authorizeHTTP applies git http-backend's service policy to r and renders
// the refusal when service may not be used. A service that is not disabled
// is authorized by Authorize. upload-pack is then served, and receive-pack
// and upload-archive are served only when enabled or authorized, the way
// http-backend serves them only when enabled or REMOTE_USER is set.
func (b *Backend) authorizeHTTP(w http.ResponseWriter, r *http.Request, service string) bool {
	var state ServiceState
	switch service {
	case transport.UploadPackService:
		state = b.UploadPack
	case transport.ReceivePackService:
		state = b.ReceivePack
	case transport.UploadArchiveService:
		state = b.UploadArchive
	}

	if state == ServiceDisabled {
		b.logf("service not enabled: %q", service)
		renderStatusError(w, http.StatusForbidden)
		return false
	}

	authorized := false
	if b.Authorize != nil {
		err := b.Authorize(r, service)
		switch {
		case err == nil:
			authorized = true
		case errors.Is(err, ErrUnauthorized):
			b.logf("unauthorized request for %q: %v", service, err)
			w.Header().Set("WWW-Authenticate", authChallenge)
			renderStatusError(w, http.StatusUnauthorized)
			return false
		case errors.Is(err, ErrForbidden):
			b.logf("forbidden request for %q: %v", service, err)
			renderStatusError(w, http.StatusForbidden)
			return false
		default:
			b.logf("error authorizing request for %q: %v", service, err)
			renderStatusError(w, http.StatusInternalServerError)
			return false
		}
	}

	if state == ServiceDefault && service != transport.UploadPackService && !authorized {
		b.logf("service not enabled: %q", service)
		renderStatusError(w, http.StatusForbidden)
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
