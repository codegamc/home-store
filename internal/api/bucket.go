package api

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

// handleListBuckets handles GET /
func (h *Handler) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.backend.ListBuckets(r.Context())
	if err != nil {
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to list buckets")
		return
	}

	// Convert storage.BucketInfo to s3.Bucket for XML response
	var s3Buckets []s3.Bucket
	for _, b := range buckets {
		s3Buckets = append(s3Buckets, s3.Bucket{
			Name:         b.Name,
			CreationDate: b.CreatedAt.Format(time.RFC3339),
		})
	}

	resp := s3.ListAllMyBucketsResult{
		Owner: s3.Owner{
			ID:          "homestore",
			DisplayName: "HomeStore",
		},
		Buckets: s3Buckets,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(resp)
}

// handleCreateBucket handles PUT /{bucket}
func (h *Handler) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	if bucket == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "bucket name is required")
		return
	}

	if !storage.IsValidBucketName(bucket) {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidBucketName, "invalid bucket name")
		return
	}

	if err := h.backend.CreateBucket(r.Context(), bucket); err != nil {
		if err == storage.ErrBucketExists {
			h.errorResponse(w, http.StatusConflict, s3.BucketAlreadyExists, "bucket already exists")
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to create bucket")
		}
		return
	}

	w.Header().Set("Location", "/"+bucket)
	w.WriteHeader(http.StatusOK)
}

// handleDeleteBucket handles DELETE /{bucket}
func (h *Handler) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	if bucket == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "bucket name is required")
		return
	}

	if !storage.IsValidBucketName(bucket) {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidBucketName, "invalid bucket name")
		return
	}

	if err := h.backend.DeleteBucket(r.Context(), bucket); err != nil {
		if err == storage.ErrBucketNotFound {
			h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to delete bucket")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleHeadBucket handles HEAD /{bucket}
func (h *Handler) handleHeadBucket(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	if bucket == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "bucket name is required")
		return
	}

	if !storage.IsValidBucketName(bucket) {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidBucketName, "invalid bucket name")
		return
	}

	exists, err := h.backend.BucketExists(r.Context(), bucket)
	if err != nil {
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to check bucket")
		return
	}

	if !exists {
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		return
	}

	w.WriteHeader(http.StatusOK)
}

// errorResponse writes an S3-compatible error response.
func (h *Handler) errorResponse(w http.ResponseWriter, statusCode int, code string, message string) {
	h.errorResponseWithResource(w, statusCode, code, message, "")
}

// errorResponseWithResource writes an S3-compatible error response with resource path.
func (h *Handler) errorResponseWithResource(w http.ResponseWriter, statusCode int, code string, message string, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)

	resp := s3.ErrorResponse{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestID: generateRequestID(),
	}

	w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(resp)
}
