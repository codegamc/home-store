package server

import (
	"context"
	"net"
	"net/http"
	"sync"
)

// Server wraps http.Server with graceful shutdown logic.
type Server struct {
	srv *http.Server
	ln  net.Listener
	mu  sync.RWMutex
}

// New creates a new Server.
func New(addr string, handler http.Handler) *Server {
	return &Server{
		srv: &http.Server{
			Addr:    addr,
			Handler: handler,
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
