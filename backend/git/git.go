package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ErrServerClosed indicates that the server has been closed.
var ErrServerClosed = errors.New("server closed")

// DefaultBackend is the default global Git transport server handler.
var DefaultBackend = NewBackend(nil)

// ServerContextKey is the context key used to store the server in the context.
var ServerContextKey = &contextKey{"git-server"}

// Backend represents a Git transport server handler that can handle
// git-upload-pack, git-receive-pack, and git-upload-archive requests over TCP.
type Backend struct {
	// Loader is used to load repositories. It uses [transport.DefaultLoader]
	// when nil.
	Loader transport.Loader
	// UploadPack indicates whether the handler should handle
	// git-upload-pack requests.
	UploadPack bool
	// ReceivePack indicates whether the handler should handle
	// git-receive-pack requests.
	ReceivePack bool
	// ArchivePack indicates whether the handler should handle
	// git-upload-archive requests.
	// ArchivePack bool // TODO: Implement git-upload-archive support
}

// NewBackend creates a new [Backend] for the given loader. It defaults to
// enabling both git-upload-pack and git-upload-archive but not
// git-receive-pack.
func NewBackend(loader transport.Loader) *Backend {
	return &Backend{
		Loader:      loader,
		UploadPack:  true,
		ReceivePack: false,
		// ArchivePack: true, // TODO: Implement git-upload-archive support
	}
}

// ServeTCP implements the [Handler] interface for the [Backend].
// TODO: Support idle timeout based on the context. Something like
// context.WithIdleTimeout where it resets the timer on each read/write
// operation.
func (b *Backend) ServeTCP(ctx context.Context, c net.Conn, req *packp.GitProtoRequest) {
	loader := b.Loader
	if loader == nil {
		loader = transport.DefaultLoader
	}

	r := ioutil.NewContextReader(ctx, c)
	wc := ioutil.NewContextWriteCloser(ctx, c)

	// Ensure we close the connection when we're done.
	defer c.Close() //nolint:errcheck

	svc := transport.Service(req.RequestCommand)
	if (svc != transport.UploadPackService &&
		// svc != transport.UploadArchiveService &&
		svc != transport.ReceivePackService) ||
		(svc == transport.UploadPackService && !b.UploadPack) ||
		// (svc == transport.UploadArchiveService && !b.UploadArchive) ||
		(svc == transport.ReceivePackService && !b.ReceivePack) {
		renderError(wc, transport.ErrUnsupportedService)
		return
	}

	host := req.Host
	if host == "" {
		host = "localhost"
	}

	url, err := url.JoinPath(fmt.Sprintf("git://%s", host), req.Pathname)
	if err != nil {
		renderError(wc, transport.ErrRepositoryNotFound)
		return
	}

	ep, err := transport.NewEndpoint(url)
	if err != nil {
		// XXX: Should we use a more descriptive error?
		renderError(wc, transport.ErrRepositoryNotFound)
		return
	}

	st, err := loader.Load(ep)
	if err != nil {
		renderError(wc, err)
		return
	}

	version := strings.Join(req.ExtraParams, ":")
	switch svc {
	case transport.UploadPackService:
		err = transport.UploadPack(ctx, st,
			io.NopCloser(r), ioutil.WriteNopCloser(wc),
			&transport.UploadPackOptions{
				GitProtocol: version,
			})
	case transport.ReceivePackService:
		err = transport.ReceivePack(ctx, st,
			io.NopCloser(r), ioutil.WriteNopCloser(wc),
			&transport.ReceivePackOptions{
				GitProtocol: version,
			})
	}

	if err != nil {
		renderError(wc, transport.ErrRepositoryNotFound)
		return
	}
}

// Handler is the interface that handles TCP requests for the Git protocol.
type Handler interface {
	// ServeTCP handles a TCP connection for the Git protocol.
	ServeTCP(ctx context.Context, c net.Conn, req *packp.GitProtoRequest)
}

// HandlerFunc is a function that implements the Handler interface.
type HandlerFunc func(ctx context.Context, c net.Conn, req *packp.GitProtoRequest)

// ServeTCP implements the Handler interface.
func (f HandlerFunc) ServeTCP(ctx context.Context, c net.Conn, req *packp.GitProtoRequest) {
	f(ctx, c, req)
}

// Server is a TCP server that handles Git protocol requests.
type Server struct {
	// Addr is the address to listen on. If empty, it defaults to ":9418".
	Addr string

	// Handler is the handler for Git protocol requests. It uses
	// [DefaultHandler] when nil.
	Handler Handler

	// ErrorLog is the logger used to log errors. When nil, it won't log
	// errors.
	ErrorLog *log.Logger

	// BaseContext optionally specifies a function to create a base context for
	// the server listeners. If nil, [context.Background] will be used.
	// The provided listener is the specific listener that is about to start
	// accepting connections.
	BaseContext func(net.Listener) context.Context

	// ConnContext optionally specifies a function to create a context for each
	// connection. If nil, the context will be derived from the server's base
	// context.
	ConnContext func(context.Context, net.Conn) context.Context

	inShutdown    atomic.Bool // true when server is in shutdown
	mu            sync.Mutex
	listeners     map[*net.Listener]struct{}
	listenerGroup sync.WaitGroup
	activeConn    map[*conn]struct{} // active connections being served
}

// shutdownPollIntervalMax is the maximum interval for polling
// idle connections during shutdown.
const shutdownPollIntervalMax = 500 * time.Millisecond

// Shutdown gracefully shuts down the server, waiting for all active
// connections to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	s.inShutdown.Store(true)

	s.mu.Lock()
	lnerr := s.closeListenersLocked()
	s.mu.Unlock()
	s.listenerGroup.Wait()

	pollIntervalBase := time.Millisecond
	nextPollInterval := func() time.Duration {
		// Add 10% jitter.
		interval := pollIntervalBase + time.Duration(rand.Intn(int(pollIntervalBase/10)))
		// Double and clamp for next time.
		pollIntervalBase *= 2
		if pollIntervalBase > shutdownPollIntervalMax {
			pollIntervalBase = shutdownPollIntervalMax
		}
		return interval
	}

	timer := time.NewTimer(nextPollInterval())
	for {
		if s.closeIdleConns() {
			return lnerr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			timer.Reset(nextPollInterval())
		}
	}
}

// Close immediately closes the server and all active connections. It returns
// any error returned from closing the underlying listeners.
func (s *Server) Close() error {
	s.inShutdown.Store(true)

	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.closeListenersLocked()

	// We need to unlock the mutex while waiting for listenersGroup.
	s.mu.Unlock()
	s.listenerGroup.Wait()
	s.mu.Lock()

	for c := range s.activeConn {
		c.Close() //nolint:errcheck
		delete(s.activeConn, c)
	}
	return err
}

// ListenAndServe listens on the TCP network address and serves Git
// protocol requests using the provided handler.
func (s *Server) ListenAndServe() error {
	if s.shuttingDown() {
		return ErrServerClosed
	}
	addr := s.Addr
	if addr == "" {
		addr = ":9418" // Default Git protocol port
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve starts the server and listens for incoming connections on the given
// listener.
func (s *Server) Serve(ln net.Listener) error {
	origLn := ln
	l := &onceCloseListener{Listener: ln}
	defer l.Close() //nolint:errcheck

	if !s.trackListener(&l.Listener, true) {
		return ErrServerClosed
	}
	defer s.trackListener(&l.Listener, false)

	baseCtx := context.Background()
	if s.BaseContext != nil {
		baseCtx = s.BaseContext(origLn)
		if baseCtx == nil {
			panic("git: BaseContext returned nil context")
		}
	}

	var tempDelay time.Duration // how long to sleep on accept failure
	ctx := context.WithValue(baseCtx, ServerContextKey, s)
	for {
		rw, err := l.Accept()
		if err != nil {
			if s.shuttingDown() {
				return ErrServerClosed
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				s.logf("git: Accept error: %v; retrying in %v", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return err
		}
		connCtx := ctx
		if cc := s.ConnContext; cc != nil {
			connCtx = cc(ctx, rw)
			if connCtx == nil {
				panic("git: ConnContext returned nil context")
			}
		}
		tempDelay = 0
		c := s.newConn(rw)
		s.trackConn(c, true)
		go c.serve(connCtx) //nolint:errcheck
	}
}

func (s *Server) shuttingDown() bool {
	return s.inShutdown.Load()
}

func (s *Server) closeListenersLocked() error {
	var err error
	for ln := range s.listeners {
		if cerr := (*ln).Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

// handler delegates to either the server's Handler or the DefaultBackend.
func (s *Server) handler(ctx context.Context, c net.Conn, req *packp.GitProtoRequest) {
	if s.Handler != nil {
		s.Handler.ServeTCP(ctx, c, req)
	} else {
		DefaultBackend.ServeTCP(ctx, c, req)
	}
}

// trackListener adds or removes a net.Listener to the set of tracked
// listeners.
//
// We store a pointer to interface in the map set, in case the
// net.Listener is not comparable. This is safe because we only call
// trackListener via Serve and can track+defer untrack the same
// pointer to local variable there. We never need to compare a
// Listener from another caller.
//
// It reports whether the server is still up (not Shutdown or Closed).
func (s *Server) trackListener(ln *net.Listener, add bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = make(map[*net.Listener]struct{})
	}
	if add {
		if s.shuttingDown() {
			return false
		}
		s.listeners[ln] = struct{}{}
		s.listenerGroup.Add(1)
	} else {
		delete(s.listeners, ln)
		s.listenerGroup.Done()
	}
	return true
}

// closeIdleConns closes all idle connections. It returns whether the server is
// quiescent.
func (s *Server) closeIdleConns() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	quiescent := true
	for c := range s.activeConn {
		unixSec := c.unixSec.Load()
		if unixSec == 0 {
			// New connection, skip it.
			quiescent = false
			continue
		}
		c.Close() //nolint:errcheck
		delete(s.activeConn, c)
	}
	return quiescent
}

func (s *Server) logf(format string, args ...interface{}) {
	if s.ErrorLog != nil {
		s.ErrorLog.Printf(format, args...)
	}
}

func (s *Server) trackConn(c *conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.unixSec.Store(uint64(time.Now().Unix()))
	if s.activeConn == nil {
		s.activeConn = make(map[*conn]struct{})
	}
	if add {
		s.activeConn[c] = struct{}{}
	} else {
		delete(s.activeConn, c)
	}
}

// conn represents a server connection that is being handled.
type conn struct {
	// Conn is the underlying net.Conn that is being used to read and write Git
	// protocol messages.
	net.Conn
	// unix timestamp in seconds when the connection was established
	unixSec atomic.Uint64
	// s the server that is handling this connection.
	s *Server
}

// newConn creates a new conn instance with the given net.Conn.
func (s *Server) newConn(rwc net.Conn) *conn {
	return &conn{
		s:    s,
		Conn: rwc,
	}
}

// serve serves a new connection.
func (c *conn) serve(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			if c.s.ErrorLog != nil {
				c.s.ErrorLog.Printf("git: panic serving connection: %v", err)
			}
			if cerr := c.Conn.Close(); cerr != nil && c.s.ErrorLog != nil {
				c.s.ErrorLog.Printf("git: error closing connection: %v", cerr)
			}
		}
	}()

	r := ioutil.NewContextReadCloser(ctx, c)

	var req packp.GitProtoRequest
	if err := req.Decode(r); err != nil {
		renderError(c, fmt.Errorf("error decoding request: %s", transport.ErrInvalidRequest))
		return
	}

	c.s.handler(ctx, c.Conn, &req)
}

// onceCloseListener wraps a net.Listener, protecting it from
// multiple Close calls.
type onceCloseListener struct {
	net.Listener
	once     sync.Once
	closeErr error
}

func (oc *onceCloseListener) Close() error {
	oc.once.Do(oc.close)
	return oc.closeErr
}

func (oc *onceCloseListener) close() { oc.closeErr = oc.Listener.Close() }

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

func renderError(rw io.WriteCloser, err error) {
	if _, err := pktline.WriteError(rw, err); err != nil {
		rw.Close() //nolint:errcheck
		return
	}
	if err := rw.Close(); err != nil {
	}
}
