package backend

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

type httpService struct {
	pattern *regexp.Regexp
	method  string
	handler func(b *Backend, w http.ResponseWriter, r *http.Request, repo, file, svc string)
	svc     string
}

var httpServices = []httpService{
	{regexp.MustCompile("(.*?)/HEAD$"), http.MethodGet, (*Backend).handleDumbTextFile, ""},
	{regexp.MustCompile("(.*?)/info/refs$"), http.MethodGet, (*Backend).handleInfoRefs, ""},
	{regexp.MustCompile("(.*?)/objects/info/alternates$"), http.MethodGet, (*Backend).handleDumbTextFile, ""},
	{regexp.MustCompile("(.*?)/objects/info/http-alternates$"), http.MethodGet, (*Backend).handleDumbTextFile, ""},
	{regexp.MustCompile("(.*?)/objects/info/packs$"), http.MethodGet, (*Backend).handleDumbInfoPacks, ""},
	{regexp.MustCompile("(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{38,62}$"), http.MethodGet, (*Backend).handleDumbLooseObject, ""},
	{regexp.MustCompile(`(.*?)/objects/pack/pack-[0-9a-f]{40,64}\.pack$`), http.MethodGet, (*Backend).handleDumbPackFile, ""},
	{regexp.MustCompile(`(.*?)/objects/pack/pack-[0-9a-f]{40,64}\.idx$`), http.MethodGet, (*Backend).handleDumbIdxFile, ""},
	{regexp.MustCompile("(.*?)/git-upload-pack$"), http.MethodPost, (*Backend).handleServiceRPC, transport.UploadPackService},
	{regexp.MustCompile("(.*?)/git-receive-pack$"), http.MethodPost, (*Backend).handleServiceRPC, transport.ReceivePackService},
}

// ServeHTTP implements [http.Handler]. It supports both smart and dumb
// HTTP protocols.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := strings.TrimPrefix(r.URL.Path, b.Prefix)
	for _, s := range httpServices {
		if m := s.pattern.FindStringSubmatch(urlPath); m != nil {
			if r.Method != s.method {
				renderStatusError(w, http.StatusMethodNotAllowed)
				return
			}

			repo := strings.TrimPrefix(m[1], "/")
			file := strings.Replace(urlPath, repo+"/", "", 1)
			s.handler(b, w, r, repo, file, s.svc)
			return
		}
	}

	renderStatusError(w, http.StatusNotFound)
}

func (b *Backend) handleServiceRPC(w http.ResponseWriter, r *http.Request, repo, _, svc string) {
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
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
}

func (b *Backend) handleInfoRefs(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	service := r.URL.Query().Get("service")
	if service == "" {
		hdrNocache(w)
		b.handleDumbSendFile(w, r, repo, file, "text/plain; charset=utf-8")
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

	if err := b.Serve(r.Context(), nil, ioutil.WriteNopCloser(w), &Request{
		URL:           ep,
		Service:       service,
		GitProtocol:   version,
		AdvertiseRefs: true,
		StatelessRPC:  true,
	}); err != nil {
		b.logf("error processing request: %v", err)
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
}

// Dumb HTTP handlers — serve static files from the repository filesystem.

func (b *Backend) handleDumbTextFile(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	hdrNocache(w)
	b.handleDumbSendFile(w, r, repo, file, "text/plain; charset=utf-8")
}

func (b *Backend) handleDumbInfoPacks(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	hdrCacheForever(w)
	b.handleDumbSendFile(w, r, repo, file, "text/plain; charset=utf-8")
}

func (b *Backend) handleDumbLooseObject(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	hdrCacheForever(w)
	b.handleDumbSendFile(w, r, repo, file, "application/x-git-loose-object")
}

func (b *Backend) handleDumbPackFile(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	hdrCacheForever(w)
	b.handleDumbSendFile(w, r, repo, file, "application/x-git-packed-objects")
}

func (b *Backend) handleDumbIdxFile(w http.ResponseWriter, r *http.Request, repo, file, _ string) {
	hdrCacheForever(w)
	b.handleDumbSendFile(w, r, repo, file, "application/x-git-packed-objects-toc")
}

func (b *Backend) handleDumbSendFile(w http.ResponseWriter, _ *http.Request, repo, file, contentType string) {
	loader := b.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}

	ep, err := transport.ParseURL(repo)
	if err != nil {
		b.logf("error parsing URL: %v", err)
		renderStatusError(w, http.StatusBadRequest)
		return
	}

	st, err := loader.Load(ep)
	if err != nil {
		b.logf("error loading repository: %v", err)
		renderStatusError(w, http.StatusNotFound)
		return
	}

	fss, ok := st.(storer.FilesystemStorer)
	if !ok {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	fs := fss.Filesystem()
	f, err := fs.Open(file)
	if err != nil {
		renderStatusError(w, http.StatusNotFound)
		return
	}
	defer func() { _ = f.Close() }()

	stat, err := fs.Lstat(file)
	if err != nil || !stat.Mode().IsRegular() {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Last-Modified", stat.ModTime().Format(http.TimeFormat))

	frw := &flushResponseWriter{ResponseWriter: w, log: b.ErrorLog, chunkSize: defaultChunkSize}
	if _, err := ioutil.CopyBufferPool(frw, f); err != nil {
		b.logf("error writing response: %v", err)
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
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

func hdrCacheForever(w http.ResponseWriter) {
	now := time.Now()
	expires := now.Add(365 * 24 * time.Hour)
	w.Header().Set("Date", now.Format(http.TimeFormat))
	w.Header().Set("Expires", expires.Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "public, max-age=31536000")
}
