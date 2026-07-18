package api

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func (h *Handler) handleCreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	meta := storage.ObjectMeta{ContentType: r.Header.Get("Content-Type"), UserMetadata: userMetadata(r.Header)}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	upload, err := h.backend.CreateMultipartUpload(r.Context(), bucket, key, meta)
	if err != nil {
		h.multipartError(w, err)
		return
	}
	h.xmlResponse(w, http.StatusOK, s3.InitiateMultipartUploadResult{Bucket: bucket, Key: key, UploadID: upload.UploadID})
}

func (h *Handler) handleUploadPart(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	partNumber, err := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidPart, "invalid part number")
		return
	}
	var body io.Reader = r.Body
	isCopy := false
	if copySource := r.Header.Get("x-amz-copy-source"); copySource != "" {
		isCopy = true
		decoded, err := url.PathUnescape(strings.TrimPrefix(copySource, "/"))
		if err != nil {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
			return
		}
		parts := strings.SplitN(decoded, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
			return
		}
		reader, _, err := h.backend.GetObject(r.Context(), parts[0], parts[1])
		if err != nil {
			h.multipartError(w, err)
			return
		}
		defer func() { _ = reader.Close() }()
		body = reader
	}
	part, err := h.backend.UploadPart(r.Context(), bucket, key, r.URL.Query().Get("uploadId"), partNumber, body)
	if err != nil {
		h.multipartError(w, err)
		return
	}
	if isCopy {
		h.xmlResponse(w, http.StatusOK, s3.CopyPartResult{ETag: part.ETag, LastModified: part.LastModified.UTC().Format(time.RFC3339)})
		return
	}
	w.Header().Set("ETag", part.ETag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleCompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	var request s3.CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid multipart completion request")
		return
	}
	parts := make([]storage.CompletedPart, 0, len(request.Parts))
	for _, part := range request.Parts {
		parts = append(parts, storage.CompletedPart{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	meta, err := h.backend.CompleteMultipartUpload(r.Context(), bucket, key, r.URL.Query().Get("uploadId"), parts)
	if err != nil {
		h.multipartError(w, err)
		return
	}
	h.xmlResponse(w, http.StatusOK, s3.CompleteMultipartUploadResult{
		Location: r.URL.Path, Bucket: bucket, Key: key, ETag: meta.ETag,
	})
}

func (h *Handler) handleAbortMultipartUpload(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	if err := h.backend.AbortMultipartUpload(r.Context(), bucket, key, r.URL.Query().Get("uploadId")); err != nil {
		h.multipartError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListMultipartUploads(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	uploads, err := h.backend.ListMultipartUploads(r.Context(), bucket)
	if err != nil {
		h.multipartError(w, err)
		return
	}
	response := s3.ListMultipartUploadsResult{Bucket: bucket}
	for _, upload := range uploads {
		response.Uploads = append(response.Uploads, s3.MultipartUpload{
			Key: upload.Key, UploadID: upload.UploadID, Initiated: upload.Initiated.UTC().Format(time.RFC3339),
		})
	}
	h.xmlResponse(w, http.StatusOK, response)
}

func (h *Handler) handleListParts(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	uploadID := r.URL.Query().Get("uploadId")
	parts, err := h.backend.ListParts(r.Context(), bucket, key, uploadID)
	if err != nil {
		h.multipartError(w, err)
		return
	}
	response := s3.ListPartsResult{Bucket: bucket, Key: key, UploadID: uploadID}
	for _, part := range parts {
		response.Parts = append(response.Parts, s3.MultipartPart{
			PartNumber: part.PartNumber, ETag: part.ETag, Size: part.Size, LastModified: part.LastModified.UTC().Format(time.RFC3339),
		})
	}
	h.xmlResponse(w, http.StatusOK, response)
}

func (h *Handler) multipartError(w http.ResponseWriter, err error) {
	switch err {
	case storage.ErrBucketNotFound:
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
	case storage.ErrUploadNotFound:
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchUpload, "the specified multipart upload does not exist")
	case storage.ErrInvalidPart:
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidPart, "one or more specified parts could not be found")
	case storage.ErrEntityTooLarge:
		h.errorResponse(w, http.StatusRequestEntityTooLarge, s3.EntityTooLarge, "object exceeds the configured storage limit")
	default:
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "multipart upload operation failed")
	}
}
