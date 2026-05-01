// Package git provides an in-process git:// protocol server.
//
// It listens on a TCP port and handles the git:// wire protocol
// (git-upload-pack, git-receive-pack, git-upload-archive) using the
// [backend.Backend] handler. The server supports configurable timeouts
// and maximum connections, similar to git-daemon.
package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const defaultAddr = "127.0.0.1:0"

var errAlreadyStarted = errors.New("git: server already started")

// Server is a git:// protocol server.
type Server struct {
	// Loader resolves repository URLs to storage. If nil,
	// [transport.DefaultLoader] is used.
	Loader transport.Loader

	// ErrorLog is used to log errors. If nil, errors are not logged.
	ErrorLog *log.Logger

	// Timeout is the idle timeout for each connection. If a connection
	// has no read or write activity for this duration, it is closed.
	// If zero, there is no idle timeout. Corresponds to git-daemon's
	// --timeout.
	Timeout time.Duration

	// InitTimeout is the timeout for the initial protocol handshake
	// (reading the GitProtoRequest). If zero, there is no handshake
	// timeout. Corresponds to git-daemon's --init-timeout.
	InitTimeout time.Duration

	// MaxTimeout is the absolute maximum duration a connection is
	// allowed to live, regardless of activity. If zero, there is no
	// maximum. This is useful to prevent long-lived connections from
	// consuming server resources indefinitely.
	MaxTimeout time.Duration

	// MaxConnections is the maximum number of simultaneous connections.
	// If zero, there is no limit. Corresponds to git-daemon's
	// --max-connections. Connections beyond the limit are immediately
	// closed.
	MaxConnections int

	ln  net.Listener
	srv *backend.Backend

	mu    sync.RWMutex
	conns map[net.Conn]context.CancelFunc
	wg    sync.WaitGroup
	done  chan struct{}

	started bool
}

// FromLoader creates a git:// server backed by the given loader.
func FromLoader(loader transport.Loader) *Server {
	return &Server{
		Loader: loader,
	}
}

// Start starts the git:// server on a random port.
// It returns the endpoint URL (e.g. "git://127.0.0.1:XXXXX") that
// clients can use to connect.
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return "", errAlreadyStarted
	}

	if s.Loader == nil {
		s.Loader = transport.DefaultLoader
	}

	ln, err := net.Listen("tcp", defaultAddr)
	if err != nil {
		return "", fmt.Errorf("git: listen: %w", err)
	}

	s.srv = backend.New(s.Loader)
	s.conns = make(map[net.Conn]context.CancelFunc)
	s.done = make(chan struct{})
	s.ln = ln
	s.started = true

	go s.serve()

	return endpoint(ln)
}

// Endpoint returns the git:// URL clients should connect to.
func (s *Server) Endpoint() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return endpoint(s.ln)
}

func endpoint(ln net.Listener) (string, error) {
	if ln == nil {
		return "", errors.New("git: server not started")
	}
	return "git://" + ln.Addr().String(), nil
}

// Close immediately closes the listener and all active connections.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.done != nil {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
	for conn, cancel := range s.conns {
		_ = conn.Close()
		cancel()
	}
	s.conns = make(map[net.Conn]context.CancelFunc)
	ln := s.ln
	s.mu.Unlock()

	var err error
	if ln != nil {
		err = ln.Close()
	}
	s.wg.Wait()
	return err
}

// serve accepts connections and handles each in a goroutine.
func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logf("git: accept: %v", err)
				return
			}
		}

		if !s.trackConn(conn, true) {
			_ = conn.Close()
			continue
		}

		s.wg.Go(func() {
			s.handleConn(conn)
			s.trackConn(conn, false)
		})
	}
}

// trackConn registers or unregisters a connection. Returns false if
// the connection exceeds MaxConnections and should be rejected.
func (s *Server) trackConn(conn net.Conn, add bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if add {
		if s.MaxConnections > 0 && len(s.conns) >= s.MaxConnections {
			return false
		}
		_, cancel := context.WithCancel(context.Background())
		s.conns[conn] = cancel
		return true
	}

	if cancel, ok := s.conns[conn]; ok {
		cancel()
		delete(s.conns, conn)
	}
	return true
}

// handleConn processes a single git:// connection.
//
// Wire protocol:
//
//	Client sends: <command> <path>\0host=<host>\0\n  (pkt-line)
//	Server reads the GitProtoRequest, resolves the repository, and
//	dispatches to the appropriate backend handler.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Retrieve the per-connection context created by trackConn.
	s.mu.RLock()
	cancel, ok := s.conns[conn]
	s.mu.RUnlock()
	if !ok {
		return
	}
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	// When the connection times out, cancel the per-connection context
	// so backend.Serve can abort gracefully.
	closeCanceler := func() {
		cancel()
		ctxCancel()
	}

	maxDeadline := time.Time{}
	if s.MaxTimeout > 0 {
		maxDeadline = time.Now().Add(s.MaxTimeout)
	}

	sc := &serverConn{
		Conn:          conn,
		idleTimeout:   s.Timeout,
		initTimeout:   s.InitTimeout,
		maxDeadline:   maxDeadline,
		closeCanceler: closeCanceler,
	}
	sc.updateDeadline()

	// Read the git protocol request line.
	br := bufio.NewReader(sc)
	var proto packp.GitProtoRequest
	if err := proto.Decode(br); err != nil {
		s.logf("git: decode request: %v", err)
		return
	}

	// Handshake complete — clear init deadline so only idle + max
	// remain in effect.
	sc.clearInitDeadline()

	req := backend.RequestFromProto(&proto)
	if !s.validService(req.Service) {
		s.logf("git: unsupported service: %s", req.Service)
		return
	}

	if err := s.srv.Serve(ctx, io.NopCloser(br), ioutil.WriteNopCloser(sc), req); err != nil {
		s.logf("git: serve %s %s: %v", req.Service, req.URL.Path, err)
	}
}

func (s *Server) logf(format string, v ...any) {
	if s.ErrorLog != nil {
		s.ErrorLog.Printf(format, v...)
	}
}

// validService returns true if the service is one we handle.
func (*Server) validService(svc string) bool {
	switch svc {
	case transport.UploadPackService, transport.ReceivePackService, transport.UploadArchiveService:
		return true
	default:
		return false
	}
}

// serverConn wraps a net.Conn with deadline management, inspired by
// gliderlabs/ssh. Every Read and Write resets the idle deadline.
// Three deadlines are computed:
//
//   - initTimeout: applied only during the initial handshake phase
//     (reading the GitProtoRequest). Cleared once the handshake
//     completes.
//   - idleTimeout: resets on each Read/Write. If no I/O occurs
//     within this duration, the connection times out.
//   - maxDeadline: absolute deadline for the connection lifetime,
//     set once when the connection is accepted.
//
// When a timeout net.Error is returned from Read or Write, the
// connection's context is cancelled via closeCanceler so the
// backend handler can abort gracefully.
type serverConn struct {
	net.Conn

	idleTimeout   time.Duration
	initTimeout   time.Duration
	maxDeadline   time.Time
	initCleared   atomic.Bool
	closeCanceler func()
}

func (c *serverConn) Read(b []byte) (n int, err error) {
	if c.idleTimeout > 0 {
		c.updateDeadline()
	}
	n, err = c.Conn.Read(b)
	if ne, ok := err.(net.Error); ok && ne.Timeout() && c.closeCanceler != nil {
		c.closeCanceler()
	}
	return n, err
}

func (c *serverConn) Write(p []byte) (n int, err error) {
	if c.idleTimeout > 0 {
		c.updateDeadline()
	}
	n, err = c.Conn.Write(p)
	if ne, ok := err.(net.Error); ok && ne.Timeout() && c.closeCanceler != nil {
		c.closeCanceler()
	}
	return n, err
}

func (c *serverConn) Close() (err error) {
	err = c.Conn.Close()
	if c.closeCanceler != nil {
		c.closeCanceler()
	}
	return err
}

// clearInitDeadline clears the handshake deadline so only idle +
// max remain in effect.
func (c *serverConn) clearInitDeadline() {
	c.initCleared.Store(true)
	if c.idleTimeout > 0 || !c.maxDeadline.IsZero() {
		c.updateDeadline()
	}
}

// updateDeadline recomputes and sets the connection deadline from the
// three configured timeouts. The earliest non-zero deadline wins.
func (c *serverConn) updateDeadline() {
	var deadline time.Time

	if !c.maxDeadline.IsZero() {
		deadline = c.maxDeadline
	}

	if !c.initCleared.Load() && c.initTimeout > 0 {
		initDeadline := time.Now().Add(c.initTimeout)
		if deadline.IsZero() || initDeadline.Before(deadline) {
			deadline = initDeadline
		}
	}

	if c.idleTimeout > 0 {
		idleDeadline := time.Now().Add(c.idleTimeout)
		if deadline.IsZero() || idleDeadline.Before(deadline) {
			deadline = idleDeadline
		}
	}

	_ = c.SetDeadline(deadline)
}
