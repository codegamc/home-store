package api

import (
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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

// parseBucketKey splits a request path into bucket and object key.
// Path must have the form /{bucket}/{key...}.
func parseBucketKey(path string) (bucket, key string) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	return parts[0], parts[1]
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

	// Determine if this is a bucket-only path (/{bucket}) or an object path (/{bucket}/{key}).
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	if len(parts) == 1 {
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
		return
	}

	// Object operations
	switch r.Method {
	case http.MethodGet:
		h.handleGetObject(w, r)
	case http.MethodPut:
		if r.Header.Get("x-amz-copy-source") != "" {
			h.handleCopyObject(w, r)
		} else {
			h.handlePutObject(w, r)
		}
	case http.MethodDelete:
		h.handleDeleteObject(w, r)
	case http.MethodHead:
		h.handleHeadObject(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
