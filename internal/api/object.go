package api

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func metadataFromRequest(r *http.Request) (storage.ObjectMeta, error) {
	meta := storage.ObjectMeta{
		ContentType:        r.Header.Get("Content-Type"),
		CacheControl:       r.Header.Get("Cache-Control"),
		ContentDisposition: r.Header.Get("Content-Disposition"),
		ContentEncoding:    r.Header.Get("Content-Encoding"),
		ContentLanguage:    r.Header.Get("Content-Language"),
		UserMetadata:       map[string]string{},
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if value := r.Header.Get("Expires"); value != "" {
		parsed, err := http.ParseTime(value)
		if err != nil {
			return storage.ObjectMeta{}, fmt.Errorf("invalid Expires header")
		}
		meta.Expires = parsed
	}
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-meta-") && len(values) != 0 {
			// If multiple values for same meta header, keep last per S3 spec (not comma-join which breaks values containing commas)
			meta.UserMetadata[lower] = values[len(values)-1]
		}
	}
	if contentMD5 := r.Header.Get("Content-MD5"); contentMD5 != "" {
		digest, err := base64.StdEncoding.DecodeString(contentMD5)
		if err != nil || len(digest) != 16 {
			return storage.ObjectMeta{}, storage.ErrInvalidDigest
		}
		meta.ExpectedMD5 = `"` + hex.EncodeToString(digest) + `"`
	}
	if payloadHash := r.Header.Get("x-amz-content-sha256"); len(payloadHash) == 64 {
		if _, err := hex.DecodeString(payloadHash); err != nil {
			return storage.ObjectMeta{}, storage.ErrInvalidDigest
		}
		meta.ChecksumSHA256 = strings.ToLower(payloadHash)
	}
	for _, checksum := range []struct {
		algorithm string
		header    string
		length    int
	}{
		{"CRC32", "x-amz-checksum-crc32", 4},
		{"CRC32C", "x-amz-checksum-crc32c", 4},
		{"SHA1", "x-amz-checksum-sha1", 20},
		{"SHA256", "x-amz-checksum-sha256", 32},
	} {
		if value := r.Header.Get(checksum.header); value != "" {
			digest, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(digest) != checksum.length {
				return storage.ObjectMeta{}, storage.ErrInvalidDigest
			}
			meta.ExpectedChecksumAlgorithm = checksum.algorithm
			meta.ExpectedChecksum = value
			break
		}
	}
	return meta, nil
}

func requestChecksum(r *http.Request) (string, string) {
	for _, value := range []struct{ algorithm, header string }{
		{"CRC32", "x-amz-checksum-crc32"}, {"CRC32C", "x-amz-checksum-crc32c"},
		{"SHA1", "x-amz-checksum-sha1"}, {"SHA256", "x-amz-checksum-sha256"},
	} {
		if checksum := r.Header.Get(value.header); checksum != "" {
			return value.algorithm, checksum
		}
	}
	return "", ""
}

func writeObjectHeaders(w http.ResponseWriter, meta storage.ObjectMeta) {
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ContentLength, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")
	optional := map[string]string{
		"Cache-Control": meta.CacheControl, "Content-Disposition": meta.ContentDisposition,
		"Content-Encoding": meta.ContentEncoding, "Content-Language": meta.ContentLanguage,
	}
	for name, value := range optional {
		if value != "" {
			w.Header().Set(name, value)
		}
	}
	if !meta.Expires.IsZero() {
		w.Header().Set("Expires", meta.Expires.UTC().Format(http.TimeFormat))
	}
	for name, value := range meta.UserMetadata {
		w.Header().Set(name, value)
	}
}

func requestConditions(r *http.Request) storage.Conditions {
	return storage.Conditions{IfMatch: r.Header.Get("If-Match"), IfNoneMatch: r.Header.Get("If-None-Match")}
}

func (h *Handler) handlePutObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	meta, err := metadataFromRequest(r)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to validate object metadata")
		return
	}
	stored, err := h.backend.PutObjectConditional(r.Context(), bucket, key, r.Body, meta, requestConditions(r))
	if err != nil {
		h.handleStorageError(w, r, err, "failed to store object")
		return
	}
	w.Header().Set("ETag", stored.ETag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	reader, meta, err := h.backend.GetObject(r.Context(), bucket, key)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to retrieve object")
		return
	}
	defer func() { _ = reader.Close() }()
	if status := evaluateReadConditions(meta, r.Header, ""); status != 0 {
		writeObjectHeaders(w, meta)
		w.WriteHeader(status)
		return
	}
	start, length, partial, err := parseRange(r.Header.Get("Range"), meta.ContentLength)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", meta.ContentLength))
		h.errorResponseWithResource(w, http.StatusRequestedRangeNotSatisfiable, s3.InvalidRange, "the requested range is not satisfiable", r.URL.Path)
		return
	}
	writeObjectHeaders(w, meta)
	if partial {
		seeker, ok := reader.(io.Seeker)
		if !ok {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "backend does not support range reads")
			return
		}
		if _, err := seeker.Seek(start, io.SeekStart); err != nil {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to seek object")
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, meta.ContentLength))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.CopyN(w, reader, length)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (h *Handler) handleHeadObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	meta, err := h.backend.HeadObject(r.Context(), bucket, key)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to retrieve object metadata")
		return
	}
	writeObjectHeaders(w, meta)
	if status := evaluateReadConditions(meta, r.Header, ""); status != 0 {
		w.WriteHeader(status)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	if err := h.backend.DeleteObject(r.Context(), bucket, key); err != nil {
		h.handleStorageError(w, r, err, "failed to delete object")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCopyObject(w http.ResponseWriter, r *http.Request) {
	dstBucket, dstKey := parseBucketKey(r.URL.Path)
	rawSource := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
	slashIdx := strings.Index(rawSource, "/")
	if slashIdx <= 0 {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
		return
	}
	bkt, err := url.PathUnescape(rawSource[:slashIdx])
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
		return
	}
	k, err := url.PathUnescape(rawSource[slashIdx+1:])
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
		return
	}
	if bkt == "" || !storage.IsValidObjectKey(k) {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-copy-source header")
		return
	}
	reader, sourceMeta, err := h.backend.GetObject(r.Context(), bkt, k)
	if err != nil {
		h.handleStorageError(w, r, err, "failed to retrieve copy source")
		return
	}
	defer func() { _ = reader.Close() }()
	if status := evaluateReadConditions(sourceMeta, r.Header, "x-amz-copy-source-"); status != 0 {
		h.errorResponseWithResource(w, http.StatusPreconditionFailed, s3.PreconditionFailed, "copy source condition failed", "/"+rawSource)
		return
	}
	meta := sourceMeta
	if strings.EqualFold(r.Header.Get("x-amz-metadata-directive"), "REPLACE") {
		meta, err = metadataFromRequest(r)
		if err != nil {
			h.handleStorageError(w, r, err, "invalid replacement metadata")
			return
		}
	}
	meta.Key, meta.BlobID, meta.ETag, meta.ExpectedMD5, meta.ChecksumSHA256 = dstKey, "", "", "", ""
	stored, err := h.backend.PutObjectConditional(r.Context(), dstBucket, dstKey, reader, meta, storage.Conditions{})
	if err != nil {
		h.handleStorageError(w, r, err, "failed to copy object")
		return
	}
	resp := s3.CopyObjectResult{ETag: stored.ETag, LastModified: stored.LastModified.UTC().Format(time.RFC3339)}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(resp)
}

func evaluateReadConditions(meta storage.ObjectMeta, headers http.Header, prefix string) int {
	get := func(suffix string) string { return headers.Get(prefix + suffix) }
	ifMatch := get("If-Match")
	if ifMatch != "" && !etagMatches(ifMatch, meta.ETag) {
		return http.StatusPreconditionFailed
	}
	if ifMatch == "" {
		if value := get("If-Unmodified-Since"); value != "" {
			if parsed, err := http.ParseTime(value); err == nil && meta.LastModified.After(parsed.Add(time.Second)) {
				return http.StatusPreconditionFailed
			}
		}
	}
	ifNoneMatch := get("If-None-Match")
	if ifNoneMatch != "" && etagMatches(ifNoneMatch, meta.ETag) {
		return http.StatusNotModified
	}
	if ifNoneMatch == "" {
		if value := get("If-Modified-Since"); value != "" {
			if parsed, err := http.ParseTime(value); err == nil && !meta.LastModified.After(parsed.Add(time.Second)) {
				return http.StatusNotModified
			}
		}
	}
	return 0
}

func etagMatches(value, etag string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "*" {
		return true
	}
	cleanETag := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(etag), "W/"))
	for _, candidate := range strings.Split(value, ",") {
		c := strings.TrimSpace(candidate)
		c = strings.TrimPrefix(c, "W/")
		c = strings.TrimSpace(c)
		if c == etag || c == cleanETag {
			return true
		}
	}
	return false
}

func parseRange(value string, size int64) (start, length int64, partial bool, err error) {
	if value == "" {
		return 0, size, false, nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size == 0 {
		return 0, 0, false, storage.ErrInvalidRange
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, storage.ErrInvalidRange
	}
	if parts[0] == "" {
		suffix, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffix <= 0 {
			return 0, 0, false, storage.ErrInvalidRange
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, true, nil
	}
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false, storage.ErrInvalidRange
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false, storage.ErrInvalidRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, true, nil
}

func (h *Handler) handleStorageError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	switch {
	case errors.Is(err, storage.ErrBucketNotFound):
		bucket, _ := parseBucketKey(r.URL.Path)
		h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", "/"+bucket)
	case errors.Is(err, storage.ErrObjectNotFound):
		h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", r.URL.Path)
	case errors.Is(err, storage.ErrPreconditionFailed):
		h.errorResponseWithResource(w, http.StatusPreconditionFailed, s3.PreconditionFailed, "at least one of the preconditions you specified did not hold", r.URL.Path)
	case errors.Is(err, storage.ErrInvalidDigest):
		h.errorResponseWithResource(w, http.StatusBadRequest, s3.InvalidDigest, "the content checksum you specified was invalid", r.URL.Path)
	case errors.Is(err, storage.ErrInvalidRange):
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", 0))
		h.errorResponseWithResource(w, http.StatusRequestedRangeNotSatisfiable, s3.InvalidRange, "the requested range is not satisfiable", r.URL.Path)
	case errors.Is(err, storage.ErrInvalidKey):
		h.errorResponseWithResource(w, http.StatusBadRequest, s3.InvalidArgument, "invalid object key or request argument", r.URL.Path)
	case errors.Is(err, storage.ErrNoSuchUpload):
		h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchUpload, "the specified multipart upload does not exist", r.URL.Path)
	case errors.Is(err, storage.ErrInvalidPart):
		h.errorResponseWithResource(w, http.StatusBadRequest, s3.InvalidPart, "one or more specified parts could not be found", r.URL.Path)
	case errors.Is(err, storage.ErrEntityTooSmall):
		h.errorResponseWithResource(w, http.StatusBadRequest, s3.EntityTooSmall, "your proposed upload is smaller than the minimum allowed object size", r.URL.Path)
	default:
		if strings.Contains(err.Error(), "aws-chunked") {
			h.errorResponseWithResource(w, http.StatusBadRequest, s3.InvalidDigest, "invalid chunked upload", r.URL.Path)
			return
		}
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, fallback)
	}
}
