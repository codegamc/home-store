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
		trimmed := strings.TrimPrefix(copyHeader, "/")
		slashIdx := strings.Index(trimmed, "/")
		if slashIdx <= 0 {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid copy source")
			return
		}
		bucketPart := trimmed[:slashIdx]
		keyPart := trimmed[slashIdx+1:]
		bkt, err := url.PathUnescape(bucketPart)
		if err != nil {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid copy source")
			return
		}
		k, err := url.PathUnescape(keyPart)
		if err != nil {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid copy source")
			return
		}
		reader, meta, err := h.backend.GetObject(r.Context(), bkt, k)
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
			start, length, _, err := parseRange(value, meta.ContentLength)
			if err != nil {
				h.errorResponse(w, http.StatusRequestedRangeNotSatisfiable, s3.InvalidRange, "invalid copy source range")
				return
			}
			seeker, ok := reader.(io.Seeker)
			if !ok {
				h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "copy source does not support ranges")
				return
			}
			if _, err := seeker.Seek(start, io.SeekStart); err != nil {
				h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to seek copy source")
				return
			}
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
	q := r.URL.Query()
	parts, err := backend.ListParts(r.Context(), bucket, key, q.Get("uploadId"))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to list multipart parts")
		return
	}

	maxParts, err := parsePositivePageSize(q.Get("max-parts"))
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidArgument, "max-parts must be an integer between 1 and 1000")
		return
	}
	marker, err := parseNonNegativeMarker(q.Get("part-number-marker"))
	if err != nil || marker > 10000 {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidArgument, "part-number-marker must be an integer between 0 and 10000")
		return
	}

	filtered := make([]storage.PartInfo, 0)
	for _, p := range parts {
		if p.PartNumber > marker {
			filtered = append(filtered, p)
		}
	}

	isTruncated := false
	nextMarker := 0
	if len(filtered) > maxParts {
		isTruncated = true
		nextMarker = filtered[maxParts-1].PartNumber
		filtered = filtered[:maxParts]
	} else if len(filtered) == maxParts {
		// Check if there are more beyond
		if len(parts) > 0 {
			lastPartNum := filtered[len(filtered)-1].PartNumber
			for _, p := range parts {
				if p.PartNumber > lastPartNum {
					isTruncated = true
					nextMarker = lastPartNum
					break
				}
			}
		}
	}

	response := s3.ListPartsResult{
		Bucket: bucket, Key: encodeMultipartValue(q, key), UploadID: q.Get("uploadId"),
		IsTruncated: isTruncated, MaxParts: maxParts, PartNumberMarker: marker,
		NextPartNumberMarker: nextMarker, EncodingType: q.Get("encoding-type"),
	}
	for _, part := range filtered {
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
	q := r.URL.Query()
	uploads, err := backend.ListMultipartUploads(r.Context(), bucket, q.Get("prefix"))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to list multipart uploads")
		return
	}

	maxUploads, err := parsePositivePageSize(q.Get("max-uploads"))
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidArgument, "max-uploads must be an integer between 1 and 1000")
		return
	}
	keyMarker := q.Get("key-marker")
	uploadIDMarker := q.Get("upload-id-marker")
	entries := multipartListingEntries(uploads, q.Get("prefix"), q.Get("delimiter"))
	filtered := make([]multipartListingEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.markerKey < keyMarker || (entry.markerKey == keyMarker && entry.markerUploadID <= uploadIDMarker) {
			continue
		}
		filtered = append(filtered, entry)
	}

	isTruncated := len(filtered) > maxUploads
	nextKeyMarker := ""
	nextUploadIDMarker := ""
	if isTruncated {
		last := filtered[maxUploads-1]
		nextKeyMarker, nextUploadIDMarker = last.markerKey, last.markerUploadID
		filtered = filtered[:maxUploads]
	}

	owner := s3.Owner{ID: "homestore", DisplayName: "HomeStore"}
	response := s3.ListMultipartUploadsResult{
		Bucket: bucket, Prefix: encodeMultipartValue(q, q.Get("prefix")), Delimiter: encodeMultipartValue(q, q.Get("delimiter")),
		KeyMarker: encodeMultipartValue(q, keyMarker), UploadIDMarker: encodeMultipartValue(q, uploadIDMarker),
		NextKeyMarker: encodeMultipartValue(q, nextKeyMarker), NextUploadIDMarker: encodeMultipartValue(q, nextUploadIDMarker),
		MaxUploads: maxUploads, IsTruncated: isTruncated, EncodingType: q.Get("encoding-type"),
	}
	for _, entry := range filtered {
		if entry.commonPrefix != "" {
			response.CommonPrefixes = append(response.CommonPrefixes, s3.CommonPrefix{Prefix: encodeMultipartValue(q, entry.commonPrefix)})
			continue
		}
		upload := entry.upload
		response.Uploads = append(response.Uploads, s3.MultipartUpload{Key: encodeMultipartValue(q, upload.Key), UploadID: upload.UploadID, Initiator: owner, Owner: owner, StorageClass: "STANDARD", Initiated: upload.CreatedAt.UTC().Format(time.RFC3339)})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}

type multipartListingEntry struct {
	upload         storage.MultipartUpload
	commonPrefix   string
	markerKey      string
	markerUploadID string
}

func multipartListingEntries(uploads []storage.MultipartUpload, prefix, delimiter string) []multipartListingEntry {
	entries := make([]multipartListingEntry, 0, len(uploads))
	for index := 0; index < len(uploads); {
		upload := uploads[index]
		if delimiter == "" {
			entries = append(entries, multipartListingEntry{upload: upload, markerKey: upload.Key, markerUploadID: upload.UploadID})
			index++
			continue
		}
		remainder := strings.TrimPrefix(upload.Key, prefix)
		delimiterIndex := strings.Index(remainder, delimiter)
		if delimiterIndex < 0 {
			entries = append(entries, multipartListingEntry{upload: upload, markerKey: upload.Key, markerUploadID: upload.UploadID})
			index++
			continue
		}
		commonPrefix := prefix + remainder[:delimiterIndex+len(delimiter)]
		last := upload
		index++
		for index < len(uploads) && strings.HasPrefix(uploads[index].Key, commonPrefix) {
			last = uploads[index]
			index++
		}
		entries = append(entries, multipartListingEntry{commonPrefix: commonPrefix, markerKey: last.Key, markerUploadID: last.UploadID})
	}
	return entries
}

func encodeMultipartValue(query url.Values, value string) string {
	if query.Get("encoding-type") == "url" {
		return s3URLEncode(value)
	}
	return value
}

func parsePositivePageSize(value string) (int, error) {
	if value == "" {
		return 1000, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 1000 {
		return 0, storage.ErrInvalidKey
	}
	return parsed, nil
}

func parseNonNegativeMarker(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, storage.ErrInvalidKey
	}
	return parsed, nil
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
