package server

import (
	"context"
	"log/slog"
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

// New creates a new Server.
func New(addr string, handler http.Handler) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           requestLogger(handler),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    1 << 20,
		},
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(started), "remote_addr", r.RemoteAddr, "request_id", w.Header().Get("x-amz-request-id"))
	})
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
