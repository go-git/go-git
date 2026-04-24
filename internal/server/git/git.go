// Package git provides an in-process git:// protocol server for testing.
//
// It listens on a random TCP port and handles the git:// wire protocol
// (git-upload-pack, git-receive-pack, git-upload-archive) using the
// [backend.Backend] handler. This allows integration tests to exercise
// the full git:// transport stack without requiring an external git
// daemon.
package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const defaultAddr = "127.0.0.1:0"

// Server is a git:// protocol server.
type Server struct {
	// Loader resolves repository URLs to storage. If nil,
	// [transport.DefaultLoader] is used.
	Loader transport.Loader

	// ErrorLog is used to log errors. If nil, errors are not logged.
	ErrorLog *log.Logger

	ln  net.Listener
	srv *backend.Backend
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
	if s.Loader == nil {
		s.Loader = transport.DefaultLoader
	}

	s.srv = backend.New(s.Loader)

	var err error
	s.ln, err = net.Listen("tcp", defaultAddr)
	if err != nil {
		return "", fmt.Errorf("git: listen: %w", err)
	}

	go s.serve()

	return s.Endpoint()
}

// Endpoint returns the git:// URL clients should connect to.
func (s *Server) Endpoint() (string, error) {
	if s.ln == nil {
		return "", errors.New("git: server not started")
	}
	return "git://" + s.ln.Addr().String(), nil
}

// Close shuts down the server and closes the listener.
func (s *Server) Close() error {
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// serve accepts connections and handles each in a goroutine.
func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// Listener closed — normal shutdown.
			return
		}
		go s.handleConn(conn)
	}
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Read the git protocol request line.
	br := bufio.NewReader(conn)
	var proto packp.GitProtoRequest
	if err := proto.Decode(br); err != nil {
		s.logf("git: decode request: %v", err)
		return
	}

	req := backend.RequestFromProto(&proto)
	if !s.validService(req.Service) {
		s.logf("git: unsupported service: %s", req.Service)
		return
	}

	if err := s.srv.Serve(ctx, io.NopCloser(conn), ioutil.WriteNopCloser(conn), req); err != nil {
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
