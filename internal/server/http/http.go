package http

import (
	"errors"
	"fmt"
	"net"
	gohttp "net/http"
	"time"

	"github.com/go-git/go-git/v6/backend/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

const (
	addr = "127.0.0.1:0"
)

type server struct {
	l transport.Loader

	ln  net.Listener
	srv *gohttp.Server
}

func FromLoader(l transport.Loader) (*server, error) {
	s := &server{
		l: l,
	}
	return s, nil
}

func (s *server) Start() (string, error) {
	h := http.NewBackend(s.l)

	var err error
	s.ln, err = net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("cannot listen to TCP: %w", err)
	}

	s.srv = &gohttp.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		_ = s.srv.Serve(s.ln)
	}()

	return s.Endpoint()
}

func (s *server) Endpoint() (string, error) {
	if s.ln != nil && s.srv != nil {
		return "http://" + s.ln.Addr().String(), nil
	}

	return "", errors.New("failed to get endpoint: server not started")
}

func (s *server) Close() error {
	err1 := s.ln.Close()
	err2 := s.srv.Close()

	return errors.Join(err1, err2)
}
