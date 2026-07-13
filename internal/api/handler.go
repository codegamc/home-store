package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

// generateRequestID generates a unique request ID for S3 error responses.
func generateRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(id[:])
}

// Handler is the HTTP request handler for the S3 API.
type Handler struct {
	backend storage.Backend
}

// NewHandler creates a new API handler.
func NewHandler(backend storage.Backend) *Handler {
	return &Handler{backend: backend}
}

// parseBucketKey splits a request path into bucket and object key.
// Path must have the form /{bucket}/{key...}.
func parseBucketKey(path string) (bucket, key string) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
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
		if r.Method == http.MethodGet {
			switch {
			case r.URL.Query().Has("location"):
				h.handleGetBucketLocation(w, r)
			case r.URL.Query().Has("uploads"):
				h.handleListMultipartUploads(w, r)
			case r.URL.Query().Get("list-type") == "2":
				h.handleListObjectsV2(w, r)
			default:
				h.handleListObjects(w, r)
			}
			return
		}
		if r.Method == http.MethodPost && r.URL.Query().Has("delete") {
			h.handleDeleteObjects(w, r)
			return
		}
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
	if r.URL.Query().Has("uploadId") {
		switch r.Method {
		case http.MethodGet:
			h.handleListParts(w, r)
		case http.MethodPut:
			h.handleUploadPart(w, r)
		case http.MethodPost:
			h.handleCompleteMultipartUpload(w, r)
		case http.MethodDelete:
			h.handleAbortMultipartUpload(w, r)
		default:
			h.errorResponse(w, http.StatusMethodNotAllowed, s3.InvalidRequest, "method not allowed")
		}
		return
	}
	if r.Method == http.MethodPost && r.URL.Query().Has("uploads") {
		h.handleCreateMultipartUpload(w, r)
		return
	}
	if r.Method == http.MethodPut && r.URL.Query().Has("renameObject") {
		h.handleRenameObject(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Query().Has("attributes") {
		h.handleGetObjectAttributes(w, r)
		return
	}
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
	// Do not use http.ServeMux here: it normalizes paths containing dot
	// segments, while S3 object keys are opaque and may legally contain them.
	h.routeRequest(w, r)
}
