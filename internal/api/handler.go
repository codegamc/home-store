package api

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func generateRequestID() string {
	value := make([]byte, 16)
	if _, err := cryptorand.Read(value); err != nil {
		return "homestore-request"
	}
	return hex.EncodeToString(value)
}

type Handler struct {
	backend storage.Backend
}

func NewHandler(backend storage.Backend) *Handler {
	return &Handler{backend: backend}
}

func parseBucketKey(path string) (bucket, key string) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
	bucket = parts[0]
	if len(parts) == 2 {
		key = parts[1]
	}
	return bucket, key
}

func (h *Handler) routeRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}
	if r.URL.Path == "/" {
		if r.Method == http.MethodGet {
			h.handleListBuckets(w, r)
			return
		}
		h.methodNotAllowed(w, r)
		return
	}

	bucket, key := parseBucketKey(r.URL.Path)
	if bucket == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "bucket name is required")
		return
	}
	query := r.URL.Query()
	if key == "" && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), "/") {
		switch r.Method {
		case http.MethodPut:
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handleCreateBucket(w, r)
		case http.MethodDelete:
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handleDeleteBucket(w, r)
		case http.MethodHead:
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handleHeadBucket(w, r)
		case http.MethodGet:
			if query.Has("location") {
				if hasUnsupportedQuery(query, "location") {
					h.notImplemented(w, r)
					return
				}
				h.handleGetBucketLocation(w, r)
			} else if query.Has("uploads") {
				if hasUnsupportedQuery(query, "uploads", "prefix", "delimiter", "key-marker", "upload-id-marker", "max-uploads", "encoding-type") {
					h.notImplemented(w, r)
					return
				}
				h.handleListMultipartUploads(w, r)
			} else if hasUnsupportedQuery(query, "list-type", "prefix", "delimiter", "marker", "max-keys", "encoding-type", "continuation-token", "start-after", "fetch-owner") {
				h.notImplemented(w, r)
			} else {
				h.handleListObjects(w, r, query.Get("list-type") == "2")
			}
		case http.MethodPost:
			if query.Has("delete") {
				if hasUnsupportedQuery(query, "delete") {
					h.notImplemented(w, r)
					return
				}
				h.handleDeleteObjects(w, r)
			} else {
				h.notImplemented(w, r)
			}
		default:
			h.methodNotAllowed(w, r)
		}
		return
	}

	if !storage.IsValidObjectKey(key) {
		h.errorResponseWithResource(w, http.StatusBadRequest, s3.InvalidRequest, "object key must contain between 1 and 1024 UTF-8 bytes", r.URL.Path)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if query.Get("uploadId") != "" {
			if hasUnsupportedQuery(query, "uploadId", "part-number-marker", "max-parts", "encoding-type") {
				h.notImplemented(w, r)
				return
			}
			h.handleListParts(w, r)
		} else if hasUnsupportedQuery(query) {
			h.notImplemented(w, r)
		} else {
			h.handleGetObject(w, r)
		}
	case http.MethodPut:
		if query.Get("uploadId") != "" && query.Get("partNumber") != "" {
			if hasUnsupportedQuery(query, "uploadId", "partNumber") {
				h.notImplemented(w, r)
				return
			}
			h.handleUploadPart(w, r)
		} else if r.Header.Get("x-amz-copy-source") != "" {
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handleCopyObject(w, r)
		} else {
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handlePutObject(w, r)
		}
	case http.MethodPost:
		if query.Has("uploads") {
			if hasUnsupportedQuery(query, "uploads") {
				h.notImplemented(w, r)
				return
			}
			h.handleCreateMultipartUpload(w, r)
		} else if query.Get("uploadId") != "" {
			if hasUnsupportedQuery(query, "uploadId") {
				h.notImplemented(w, r)
				return
			}
			h.handleCompleteMultipartUpload(w, r)
		} else {
			h.notImplemented(w, r)
		}
	case http.MethodDelete:
		if query.Get("uploadId") != "" {
			if hasUnsupportedQuery(query, "uploadId") {
				h.notImplemented(w, r)
				return
			}
			h.handleAbortMultipartUpload(w, r)
		} else {
			if hasUnsupportedQuery(query) {
				h.notImplemented(w, r)
				return
			}
			h.handleDeleteObject(w, r)
		}
	case http.MethodHead:
		if hasUnsupportedQuery(query) {
			h.notImplemented(w, r)
			return
		}
		h.handleHeadObject(w, r)
	default:
		h.methodNotAllowed(w, r)
	}
}

func hasUnsupportedQuery(query url.Values, allowed ...string) bool {
	allowedSet := make(map[string]bool, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = true
	}
	for name := range query {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-") || lower == "x-id" {
			continue
		}
		if !allowedSet[name] {
			return true
		}
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("x-amz-request-id", generateRequestID())
	h.routeRequest(w, r)
}

func (h *Handler) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	h.errorResponseWithResource(w, http.StatusMethodNotAllowed, s3.MethodNotAllowed, "the specified method is not allowed against this resource", r.URL.Path)
}

func (h *Handler) notImplemented(w http.ResponseWriter, r *http.Request) {
	h.errorResponseWithResource(w, http.StatusNotImplemented, s3.NotImplemented, "the requested S3 operation is not implemented", r.URL.Path)
}
