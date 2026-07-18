package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server wraps http.Server with graceful shutdown logic.
type Server struct {
	srv *http.Server
	ln  net.Listener
	mu  sync.RWMutex
}

// Options controls HTTP server resource limits. Zero ReadTimeout and
// WriteTimeout deliberately allow long S3 transfers; deployments should set
// them when a bounded transfer duration is required.
type Options struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// New creates a new Server.
func New(addr string, handler http.Handler, options Options) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: options.ReadHeaderTimeout,
			ReadTimeout:       options.ReadTimeout,
			WriteTimeout:      options.WriteTimeout,
			IdleTimeout:       options.IdleTimeout,
		},
	}
}

// Start starts the server and blocks until it fails.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	return s.srv.Serve(ln)
}

// StartTLS starts the server with TLS and blocks until it stops.
func (s *Server) StartTLS(certFile, keyFile string) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	return s.srv.ServeTLS(ln, certFile, keyFile)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Addr returns the server's listening address.
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ln != nil {
		return s.ln.Addr()
	}
	return nil
}
