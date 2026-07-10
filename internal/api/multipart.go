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

func (h *Handler) multipartBackend(w http.ResponseWriter, r *http.Request) (storage.MultipartBackend, bool) {
	backend, ok := h.backend.(storage.MultipartBackend)
	if !ok {
		h.notImplemented(w, r)
	}
	return backend, ok
}

func (h *Handler) handleCreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	bucket, key := parseBucketKey(r.URL.Path)
	meta, err := metadataFromRequest(r)
	if err != nil {
		h.handleStorageError(w, r, err, "invalid multipart metadata")
		return
	}
	upload, err := backend.CreateMultipartUpload(r.Context(), bucket, key, meta)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to create multipart upload")
		return
	}
	response := s3.InitiateMultipartUploadResult{Bucket: bucket, Key: key, UploadID: upload.UploadID}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

func (h *Handler) handleUploadPart(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	bucket, key := parseBucketKey(r.URL.Path)
	partNumber, err := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidArgument, "invalid part number")
		return
	}
	body := io.Reader(r.Body)
	expectedPayloadHash := r.Header.Get("x-amz-content-sha256")
	var sourceReader io.ReadCloser
	if copyHeader := r.Header.Get("x-amz-copy-source"); copyHeader != "" {
		expectedPayloadHash = ""
		copySource, err := url.PathUnescape(strings.TrimPrefix(copyHeader, "/"))
		if err != nil {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid copy source")
			return
		}
		parts := strings.SplitN(copySource, "/", 2)
		if len(parts) != 2 {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid copy source")
			return
		}
		reader, meta, err := h.backend.GetObject(r.Context(), parts[0], parts[1])
		if err != nil {
			h.handleStorageError(w, r, err, "failed to read upload-part copy source")
			return
		}
		sourceReader = reader
		defer func() { _ = sourceReader.Close() }()
		if evaluateReadConditions(meta, r.Header, "x-amz-copy-source-") != 0 {
			h.errorResponse(w, http.StatusPreconditionFailed, s3.PreconditionFailed, "copy source condition failed")
			return
		}
		if value := r.Header.Get("x-amz-copy-source-range"); value != "" {
			start, length, _, err := parseRange(strings.Replace(value, "bytes=", "bytes=", 1), meta.ContentLength)
			if err != nil {
				h.errorResponse(w, http.StatusRequestedRangeNotSatisfiable, s3.InvalidRange, "invalid copy source range")
				return
			}
			seeker, ok := reader.(io.Seeker)
			if !ok {
				h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "copy source does not support ranges")
				return
			}
			_, _ = seeker.Seek(start, io.SeekStart)
			body = io.LimitReader(reader, length)
		} else {
			body = reader
		}
	}
	checksumAlgorithm, checksumValue := requestChecksum(r)
	part, err := backend.UploadPart(r.Context(), bucket, key, r.URL.Query().Get("uploadId"), partNumber, body, expectedPayloadHash, checksumAlgorithm, checksumValue)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to upload multipart part")
		return
	}
	w.Header().Set("ETag", part.ETag)
	if r.Header.Get("x-amz-copy-source") != "" {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(xml.Header))
		_ = xml.NewEncoder(w).Encode(s3.CopyPartResult{ETag: part.ETag, LastModified: part.LastModified.UTC().Format(time.RFC3339)})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleListParts(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	bucket, key := parseBucketKey(r.URL.Path)
	parts, err := backend.ListParts(r.Context(), bucket, key, r.URL.Query().Get("uploadId"))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to list multipart parts")
		return
	}
	response := s3.ListPartsResult{Bucket: bucket, Key: key, UploadID: r.URL.Query().Get("uploadId")}
	for _, part := range parts {
		response.Parts = append(response.Parts, s3.Part{PartNumber: part.PartNumber, LastModified: part.LastModified.UTC().Format(time.RFC3339), ETag: part.ETag, Size: part.Size})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

func (h *Handler) handleListMultipartUploads(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	bucket, _ := parseBucketKey(r.URL.Path)
	uploads, err := backend.ListMultipartUploads(r.Context(), bucket, r.URL.Query().Get("prefix"))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to list multipart uploads")
		return
	}
	owner := s3.Owner{ID: "homestore", DisplayName: "HomeStore"}
	response := s3.ListMultipartUploadsResult{Bucket: bucket, Prefix: r.URL.Query().Get("prefix")}
	for _, upload := range uploads {
		response.Uploads = append(response.Uploads, s3.MultipartUpload{Key: upload.Key, UploadID: upload.UploadID, Initiator: owner, Owner: owner, StorageClass: "STANDARD", Initiated: upload.CreatedAt.UTC().Format(time.RFC3339)})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

func (h *Handler) handleCompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	const maxCompleteBody = 2 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCompleteBody+1))
	if err != nil || len(body) > maxCompleteBody {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid complete multipart request")
		return
	}
	var request s3.CompleteMultipartUploadRequest
	if err := xml.Unmarshal(body, &request); err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid complete multipart request")
		return
	}
	completed := make([]storage.CompletedPart, 0, len(request.Parts))
	for _, part := range request.Parts {
		completed = append(completed, storage.CompletedPart{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	bucket, key := parseBucketKey(r.URL.Path)
	meta, err := backend.CompleteMultipartUpload(r.Context(), bucket, key, r.URL.Query().Get("uploadId"), completed, requestConditions(r))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to complete multipart upload")
		return
	}
	response := s3.CompleteMultipartUploadResult{Location: "/" + bucket + "/" + key, Bucket: bucket, Key: key, ETag: meta.ETag}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

func (h *Handler) handleAbortMultipartUpload(w http.ResponseWriter, r *http.Request) {
	backend, ok := h.multipartBackend(w, r)
	if !ok {
		return
	}
	bucket, key := parseBucketKey(r.URL.Path)
	if err := backend.AbortMultipartUpload(r.Context(), bucket, key, r.URL.Query().Get("uploadId")); err != nil {
		h.handleStorageError(w, r, err, "failed to abort multipart upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
