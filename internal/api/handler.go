package api

import (
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

// requestIDRand is a dedicated random source for request IDs.
// Not cryptographically secure, but sufficient for request ID generation.
var requestIDRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// generateRequestID generates a unique request ID for S3 error responses.
func generateRequestID() string {
	return strconv.FormatUint(requestIDRand.Uint64(), 16)
}

// Handler is the HTTP request handler for the S3 API.
type Handler struct {
	backend storage.Backend
	mux     *http.ServeMux
}

// NewHandler creates a new API handler.
func NewHandler(backend storage.Backend) *Handler {
	h := &Handler{
		backend: backend,
		mux:     http.NewServeMux(),
	}
	h.setupRoutes()
	return h
}

// setupRoutes sets up all HTTP routes.
func (h *Handler) setupRoutes() {
	h.mux.HandleFunc("/", h.routeRequest)
}

// routeRequest dispatches requests to appropriate handlers.
func (h *Handler) routeRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Root path - list buckets
	if path == "/" {
		if r.Method == http.MethodGet {
			h.handleListBuckets(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Bucket operations
	switch r.Method {
	case http.MethodPut:
		h.handleCreateBucket(w, r)
	case http.MethodDelete:
		h.handleDeleteBucket(w, r)
	case http.MethodHead:
		h.handleHeadBucket(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
