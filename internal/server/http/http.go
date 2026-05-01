// Package http provides an in-process HTTP git server for testing.
package http

import (
	"errors"
	"fmt"
	"net"
	gohttp "net/http"
	"sync"
	"time"

	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

const (
	addr = "127.0.0.1:0"
)

var errAlreadyStarted = errors.New("http: server already started")

type server struct {
	l transport.Loader

	ln  net.Listener
	srv *gohttp.Server

	mu      sync.RWMutex
	started bool
}

// FromLoader creates an HTTP git server backed by the given loader.
func FromLoader(l transport.Loader) (*server, error) { //nolint:revive // unexported-return is intentional
	s := &server{
		l: l,
	}
	return s, nil
}

func (s *server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return "", errAlreadyStarted
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("cannot listen to TCP: %w", err)
	}

	h := backend.New(s.l)
	srv := &gohttp.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.ln = ln
	s.srv = srv
	s.started = true

	go func() {
		_ = srv.Serve(ln)
	}()

	return endpoint(ln, srv)
}

func (s *server) Endpoint() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return endpoint(s.ln, s.srv)
}

func endpoint(ln net.Listener, srv *gohttp.Server) (string, error) {
	if ln != nil && srv != nil {
		return "http://" + ln.Addr().String(), nil
	}

	return "", errors.New("failed to get endpoint: server not started")
}

func (s *server) Close() error {
	s.mu.RLock()
	srv := s.srv
	s.mu.RUnlock()

	if srv == nil {
		return nil
	}

	// s.srv.Close() closes the listener internally; calling s.ln.Close()
	// separately would close it twice and return "use of closed network
	// connection" from the second close.
	return srv.Close()
}
