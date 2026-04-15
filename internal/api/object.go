package api

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

// handlePutObject handles PUT /{bucket}/{key}.
func (h *Handler) handlePutObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	meta := storage.ObjectMeta{
		ContentType: contentType,
	}

	if err := h.backend.PutObject(r.Context(), bucket, key, r.Body, meta); err != nil {
		if err == storage.ErrBucketNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", fmt.Sprintf("/%s", bucket))
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to store object")
		}
		return
	}

	// Re-fetch metadata to get the computed ETag.
	stored, err := h.backend.HeadObject(r.Context(), bucket, key)
	if err == nil {
		w.Header().Set("ETag", stored.ETag)
	}
	w.WriteHeader(http.StatusOK)
}

// handleGetObject handles GET /{bucket}/{key}.
func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)

	reader, meta, err := h.backend.GetObject(r.Context(), bucket, key)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", r.URL.Path)
		} else if err == storage.ErrBucketNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", fmt.Sprintf("/%s", bucket))
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to retrieve object")
		}
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.LastModified.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

// handleHeadObject handles HEAD /{bucket}/{key}.
func (h *Handler) handleHeadObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)

	meta, err := h.backend.HeadObject(r.Context(), bucket, key)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", r.URL.Path)
		} else if err == storage.ErrBucketNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", fmt.Sprintf("/%s", bucket))
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to retrieve object metadata")
		}
		return
	}

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.LastModified.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
}

// handleDeleteObject handles DELETE /{bucket}/{key}.
func (h *Handler) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)

	if err := h.backend.DeleteObject(r.Context(), bucket, key); err != nil {
		if err == storage.ErrObjectNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", r.URL.Path)
		} else if err == storage.ErrBucketNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", fmt.Sprintf("/%s", bucket))
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to delete object")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCopyObject handles PUT /{bucket}/{key} with x-amz-copy-source header.
func (h *Handler) handleCopyObject(w http.ResponseWriter, r *http.Request) {
	dstBucket, dstKey := parseBucketKey(r.URL.Path)

	// x-amz-copy-source is /{srcBucket}/{srcKey} or {srcBucket}/{srcKey}
	copySource := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
	parts := strings.SplitN(copySource, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
		return
	}
	srcBucket, srcKey := parts[0], parts[1]

	meta, err := h.backend.CopyObject(r.Context(), srcBucket, srcKey, dstBucket, dstKey)
	if err != nil {
		if err == storage.ErrObjectNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", fmt.Sprintf("/%s/%s", srcBucket, srcKey))
		} else if err == storage.ErrBucketNotFound {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", fmt.Sprintf("/%s", srcBucket))
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to copy object")
		}
		return
	}

	resp := s3.CopyObjectResult{
		ETag:         meta.ETag,
		LastModified: meta.LastModified.UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(resp)
}
