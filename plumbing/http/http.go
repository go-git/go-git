package http

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
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
	method  string
	handler http.HandlerFunc
	svc     transport.Service
}

var services = map[string]service{
	"(.*?)/HEAD$":                                  {http.MethodGet, getTextFile, ""},
	"(.*?)/info/refs$":                             {http.MethodGet, getInfoRefs, ""},
	"(.*?)/objects/info/alternates$":               {http.MethodGet, getTextFile, ""},
	"(.*?)/objects/info/http-alternates$":          {http.MethodGet, getTextFile, ""},
	"(.*?)/objects/info/packs$":                    {http.MethodGet, getInfoPacks, ""},
	"(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{38}$":      {http.MethodGet, getLooseObject, ""},
	"(.*?)/objects/[0-9a-f]{2}/[0-9a-f]{62}$":      {http.MethodGet, getLooseObject, ""},
	"(.*?)/objects/pack/pack-[0-9a-f]{40}\\.pack$": {http.MethodGet, getPackFile, ""},
	"(.*?)/objects/pack/pack-[0-9a-f]{64}\\.pack$": {http.MethodGet, getPackFile, ""},
	"(.*?)/objects/pack/pack-[0-9a-f]{40}\\.idx$":  {http.MethodGet, getIdxFile, ""},
	"(.*?)/objects/pack/pack-[0-9a-f]{64}\\.idx$":  {http.MethodGet, getIdxFile, ""},

	"(.*?)/git-upload-pack$":    {http.MethodPost, serviceRpc, transport.UploadPackService},
	"(.*?)/git-upload-archive$": {http.MethodPost, serviceRpc, transport.UploadArchiveService},
	"(.*?)/git-receive-pack$":   {http.MethodPost, serviceRpc, transport.ReceivePackService},
}

// DefaultLoader is the default loader used to load repositories from storage.
// It will use the current working directory as the base path for the
// repositories.
var DefaultLoader = transport.NewFilesystemLoader(osfs.New("."), false)

// Handler is a Git HTTP handler that serves git repositories over HTTP.
//
// It supports serving repositories using both the Smart-HTTP and the Dumb-HTTP
// protocols. When the Dumb-HTTP protocol is used, the repository store must
// implement the [storer.FilesystemStorer] interface. Keep in mind that
// repositories that wish to be server using the Dumb-HTTP protocol must update
// the server info files. This can be done by using
// [transport.UpdateServerInfo] before serving the repository.
type Handler struct {
	// Loader is the loader used to load repositories from storage. If nil, the
	// default loader is used.
	Loader transport.Loader
	// ErrorLog is the logger used to log errors. If nil, no errors are logged.
	ErrorLog *log.Logger
}

// init initializes the handler by setting the default loader.
func (h *Handler) init() {
	if h.Loader == nil {
		h.Loader = DefaultLoader
	}
}

// logf logs the given message to the error log if it is set.
func logf(logger *log.Logger, format string, v ...interface{}) {
	if logger != nil {
		logger.Printf(format, v...)
	}
}

// Handler returns a new HTTP handler that serves git repositories over HTTP.
// It uses the given loader to load the repositories from storage.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	for pattern, service := range services {
		pat := regexp.MustCompile(pattern)
		if m := pat.FindStringSubmatch(r.URL.Path); m != nil {
			if r.Method != service.method {
				renderStatusError(w, http.StatusMethodNotAllowed)
				return
			}

			repo := strings.TrimPrefix(m[1], "/")
			file := strings.Replace(r.URL.Path, repo+"/", "", 1)
			ep, err := transport.NewEndpoint(repo)
			if err != nil {
				logf(h.ErrorLog, "error creating endpoint: %v", err)
				renderStatusError(w, http.StatusBadRequest)
				return
			}

			st, err := h.Loader.Load(ep)
			if err != nil {
				logf(h.ErrorLog, "error loading repository: %v", err)
				renderStatusError(w, http.StatusNotFound)
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, contextKey("errorLog"), h.ErrorLog)
			ctx = context.WithValue(ctx, contextKey("repo"), m[1])
			ctx = context.WithValue(ctx, contextKey("file"), file)
			ctx = context.WithValue(ctx, contextKey("service"), service.svc)
			ctx = context.WithValue(ctx, contextKey("storer"), st)
			ctx = context.WithValue(ctx, contextKey("endpoint"), ep)

			service.handler(w, r.WithContext(ctx))
			return
		}
	}
}

func serviceRpc(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := ctx.Value(contextKey("storer")).(storage.Storer)
	svc := ctx.Value(contextKey("service")).(transport.Service)
	errorLog := ctx.Value(contextKey("errorLog")).(*log.Logger)
	version := r.Header.Get("Git-Protocol")
	contentType := r.Header.Get("Content-Type")

	if contentType != fmt.Sprintf("application/x-git-%s-request", svc.Name()) {
		renderStatusError(w, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-result", svc.Name()))
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var in bytes.Buffer
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

	io.Copy(&in, reader) //nolint:errcheck

	var out bytes.Buffer
	switch svc {
	case transport.UploadPackService:
		err = transport.UploadPack(ctx, st, io.NopCloser(&in), ioutil.WriteNopCloser(&out),
			&transport.UploadPackOptions{
				GitProtocol:   version,
				AdvertiseRefs: false,
				StatelessRPC:  true,
			})
	case transport.ReceivePackService:
		err = transport.ReceivePack(ctx, st, io.NopCloser(&in), ioutil.WriteNopCloser(&out),
			&transport.ReceivePackOptions{
				GitProtocol:   version,
				AdvertiseRefs: false,
				StatelessRPC:  true,
			})
	default:
		// TODO: Support git-upload-archive
		logf(errorLog, "unknown service: %s", svc.Name())
		renderStatusError(w, http.StatusBadRequest)
		return
	}
	if err != nil {
		logf(errorLog, "error processing request: %v", err)
		renderStatusError(w, http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		logf(errorLog, "http writer must implement http.Flusher interface")
		// The writer must implement the Flusher interface which the default
		// http.ResponseWriter does.
		renderStatusError(w, http.StatusBadRequest)
		return
	}

	// Write the response to the client in chunks.
	p := make([]byte, 1024)
	for {
		nr, err := out.Read(p)
		if errors.Is(err, io.EOF) {
			break
		}
		nw, err := w.Write(p[:nr])
		if err != nil {
			logf(errorLog, "error writing response: %v", err)
			renderStatusError(w, http.StatusInternalServerError)
			return
		}
		if nr != nw {
			logf(errorLog, "mismatched bytes written: expected %d, wrote %d", nr, nw)
			renderStatusError(w, http.StatusInternalServerError)
			return
		}
		flusher.Flush()
	}
}

func sendFile(w http.ResponseWriter, r *http.Request, contentType string) {
	ctx := r.Context()
	st := ctx.Value(contextKey("storer")).(storage.Storer)
	fss, ok := st.(storer.FilesystemStorer)
	if !ok {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	file := ctx.Value(contextKey("file")).(string)
	fs := fss.Filesystem()
	f, err := fs.Open(file)
	if err != nil {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	defer f.Close() //nolint:errcheck
	stat, err := fs.Stat(file)
	if err != nil {
		renderStatusError(w, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Last-Modified", stat.ModTime().Format(http.TimeFormat))
	io.Copy(w, f) //nolint:errcheck
}

func getTextFile(w http.ResponseWriter, r *http.Request) {
	hdrNocache(w)
	sendFile(w, r, "text/plain; charset=utf-8")
}

func getInfoRefs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := ctx.Value(contextKey("storer")).(storage.Storer)
	errorLog := ctx.Value(contextKey("errorLog")).(*log.Logger)
	service := transport.Service(r.FormValue("service"))
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
	now := time.Now().Unix()
	expires := now + 31536000
	w.Header().Set("Date", fmt.Sprintf("%d", now))
	w.Header().Set("Expires", fmt.Sprintf("%d", expires))
	w.Header().Set("Cache-Control", "public, max-age=31536000")
}
