package http

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

type contextKey string

type service struct {
	pattern  *regexp.Regexp
	method  string
	handler http.HandlerFunc
	svc     transport.Service
}

var services = []service{
	{regexp.MustCompile("(.*?)/HEAD$"), http.MethodGet, getTextFile, ""},
	{regexp.MustCompile("(.*?)/info/refs$"), http.MethodGet, getInfoRefs, ""},
	{regexp.MustCompile("(.*?)/objects/info/alternates$"), http.MethodGet, getTextFile, ""},
	{regexp.MustCompile("(.*?)/objects/info/http-alternates$"), http.MethodGet, getTextFile, ""},
	{regexp.MustCompile("(.*?)/objects/info/packs$"), http.MethodGet, getInfoPacks, ""},
	{regexp.MustCompile("(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{38}$"), http.MethodGet, getLooseObject, ""},
	{regexp.MustCompile("(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{62}$"), http.MethodGet, getLooseObject, ""},
	{regexp.MustCompile("(.*?)/objects/pack/pack-[0-9a-f]{40}\\.pack$"), http.MethodGet, getPackFile, ""},
	{regexp.MustCompile("(.*?)/objects/pack/pack-[0-9a-f]{64}\\.pack$"), http.MethodGet, getPackFile, ""},
	{regexp.MustCompile("(.*?)/objects/pack/pack-[0-9a-f]{40}\\.idx$"), http.MethodGet, getIdxFile, ""},
	{regexp.MustCompile("(.*?)/objects/pack/pack-[0-9a-f]{64}\\.idx$"), http.MethodGet, getIdxFile, ""},

	// TODO: Support git-upload-archive
	// {regexp.MustCompile("(.*?)/git-upload-archive$"), http.MethodPost, serviceRpc, transport.UploadArchiveService},
	{regexp.MustCompile("(.*?)/git-upload-pack$"), http.MethodPost, serviceRpc, transport.UploadPackService},
	{regexp.MustCompile("(.*?)/git-receive-pack$"), http.MethodPost, serviceRpc, transport.ReceivePackService},
}

// DefaultLoader is the default loader used to load repositories from storage.
// It will use the current working directory as the base path for the
// repositories.
var DefaultLoader = transport.NewFilesystemLoader(osfs.New("."), false)

// HandlerOptions represents a set of options for the Git HTTP handler.
type HandlerOptions struct {
	// ErrorLog is the logger used to log errors. If nil, no errors are logged.
	ErrorLog *log.Logger
	// Prefix is a path prefix that will be stripped from the URL path before
	// matching the service patterns.
	Prefix string
}

// NewHandler returns a Git HTTP handler that serves git repositories over
// HTTP.
//
// It supports serving repositories using both the Smart-HTTP and the Dumb-HTTP
// protocols. When the Dumb-HTTP protocol is used, the repository store must
// implement the [storer.FilesystemStorer] interface. Keep in mind that
// repositories that wish to be server using the Dumb-HTTP protocol must update
// the server info files. This can be done by using
// [transport.UpdateServerInfo] before serving the repository.
func NewHandler(loader transport.Loader, opts *HandlerOptions) http.HandlerFunc {
	if loader == nil {
		loader = DefaultLoader
	}
	if opts == nil {
		opts = &HandlerOptions{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		urlPath := r.URL.Path
		urlPath = strings.TrimPrefix(urlPath, opts.Prefix)
		for _, s := range services {
			if m := s.pattern.FindStringSubmatch(urlPath); m != nil {
				if r.Method != s.method {
					renderStatusError(w, http.StatusMethodNotAllowed)
					return
				}

				repo := strings.TrimPrefix(m[1], "/")
				file := strings.Replace(urlPath, repo+"/", "", 1)
				ep, err := transport.NewEndpoint(repo)
				if err != nil {
					logf(opts.ErrorLog, "error creating endpoint: %v", err)
					renderStatusError(w, http.StatusBadRequest)
					return
				}

				st, err := loader.Load(ep)
				if err != nil {
					logf(opts.ErrorLog, "error loading repository: %v", err)
					renderStatusError(w, http.StatusNotFound)
					return
				}

				ctx := r.Context()
				ctx = context.WithValue(ctx, contextKey("errorLog"), opts.ErrorLog)
				ctx = context.WithValue(ctx, contextKey("repo"), m[1])
				ctx = context.WithValue(ctx, contextKey("file"), file)
				ctx = context.WithValue(ctx, contextKey("service"), s.svc)
				ctx = context.WithValue(ctx, contextKey("storer"), st)
				ctx = context.WithValue(ctx, contextKey("endpoint"), ep)

				s.handler(w, r.WithContext(ctx))
				return
			}
		}

		// If no service matched, return 404.
		renderStatusError(w, http.StatusNotFound)
	}
}

// logf logs the given message to the error log if it is set.
func logf(logger *log.Logger, format string, v ...interface{}) {
	if logger != nil {
		logger.Printf(format, v...)
	}
}

func serviceRpc(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, ok := ctx.Value(contextKey("storer")).(storage.Storer)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	svc, ok := ctx.Value(contextKey("service")).(transport.Service)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	errorLog, ok := ctx.Value(contextKey("errorLog")).(*log.Logger)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	version := r.Header.Get("Git-Protocol")
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))

	expectedContentType := strings.ToLower(fmt.Sprintf("application/x-git-%s-request", svc.Name()))
	if contentType != expectedContentType {
		renderStatusError(w, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", svc.Name()))
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var reader io.ReadCloser
	var err error
	switch r.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(r.Body)
		if err != nil {
			logf(errorLog, "error creating gzip reader: %v", err)
			renderStatusError(w, http.StatusInternalServerError)
			return
		}
		defer reader.Close() //nolint:errcheck
	default:
		reader = r.Body
	}

	frw := &flushResponseWriter{ResponseWriter: w, log: errorLog, chunkSize: defaultChunkSize}

	switch svc {
	case transport.UploadPackService:
		err = transport.UploadPack(ctx, st, reader, frw,
			&transport.UploadPackOptions{
				GitProtocol:   version,
				AdvertiseRefs: false,
				StatelessRPC:  true,
			})
	case transport.ReceivePackService:
		err = transport.ReceivePack(ctx, st, reader, frw,
			&transport.ReceivePackOptions{
				GitProtocol:   version,
				AdvertiseRefs: false,
				StatelessRPC:  true,
			})
	default:
		// TODO: Support git-upload-archive
		logf(errorLog, "unknown service: %s", svc.Name())
		renderStatusError(w, http.StatusNotFound)
		return
	}
	if err != nil {
		logf(errorLog, "error processing request: %v", err)
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
}

func sendFile(w http.ResponseWriter, r *http.Request, contentType string) {
	ctx := r.Context()
	st, ok := ctx.Value(contextKey("storer")).(storage.Storer)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	fss, ok := st.(storer.FilesystemStorer)
	if !ok {
		renderStatusError(w, http.StatusNotFound)
		return
	}
	errorLog, ok := ctx.Value(contextKey("errorLog")).(*log.Logger)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}

	file, ok := ctx.Value(contextKey("file")).(string)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	fs := fss.Filesystem()
	f, err := fs.Open(file)
	if err != nil {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	defer f.Close() //nolint:errcheck

	stat, err := fs.Lstat(file)
	if err != nil || !stat.Mode().IsRegular() {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Last-Modified", stat.ModTime().Format(http.TimeFormat))

	frw := &flushResponseWriter{ResponseWriter: w, log: errorLog, chunkSize: defaultChunkSize}
	if _, err := io.Copy(frw, f); err != nil {
		logf(errorLog, "error writing response: %v", err)
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
}

func getTextFile(w http.ResponseWriter, r *http.Request) {
	hdrNocache(w)
	sendFile(w, r, "text/plain; charset=utf-8")
}

func getInfoRefs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, ok := ctx.Value(contextKey("storer")).(storage.Storer)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}
	errorLog, ok := ctx.Value(contextKey("errorLog")).(*log.Logger)
	if !ok {
		renderStatusError(w, http.StatusInternalServerError)
		return
	}

	service := transport.Service(r.URL.Query().Get("service"))
	version := r.Header.Get("Git-Protocol")

	if service != "" {
		hdrNocache(w)
		w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", service.Name()))

		var err error
		switch service {
		case transport.UploadPackService:
			err = transport.UploadPack(ctx, st, nil, ioutil.WriteNopCloser(w),
				&transport.UploadPackOptions{
					GitProtocol:   version,
					AdvertiseRefs: true,
					StatelessRPC:  true,
				},
			)
		case transport.ReceivePackService:
			err = transport.ReceivePack(ctx, st, nil, ioutil.WriteNopCloser(w),
				&transport.ReceivePackOptions{
					GitProtocol:   version,
					AdvertiseRefs: true,
					StatelessRPC:  true,
				},
			)
		}
		if err != nil {
			logf(errorLog, "error processing request: %v", err)
			renderStatusError(w, http.StatusInternalServerError)
			return
		}
	} else {
		hdrNocache(w)
		sendFile(w, r, "text/plain; charset=utf-8")
	}
}

func getInfoPacks(w http.ResponseWriter, r *http.Request) {
	hdrCacheForever(w)
	sendFile(w, r, "text/plain; charset=utf-8")
}

func getLooseObject(w http.ResponseWriter, r *http.Request) {
	hdrCacheForever(w)
	sendFile(w, r, "application/x-git-loose-object")
}

func getPackFile(w http.ResponseWriter, r *http.Request) {
	hdrCacheForever(w)
	sendFile(w, r, "application/x-git-packed-objects")
}

func getIdxFile(w http.ResponseWriter, r *http.Request) {
	hdrCacheForever(w)
	sendFile(w, r, "application/x-git-packed-objects-toc")
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
