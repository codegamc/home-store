package api

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"hash/crc32"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func (h *Handler) handleListObjects(w http.ResponseWriter, r *http.Request, v2 bool) {
	bucket, _ := parseBucketKey(r.URL.Path)
	query := r.URL.Query()
	maxKeys := 1000
	if value := query.Get("max-keys"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidArgument, "max-keys must be a non-negative integer")
			return
		}
		maxKeys = parsed
	}
	result, err := h.backend.ListObjects(r.Context(), bucket, storage.ListOptions{
		Prefix: query.Get("prefix"), Delimiter: query.Get("delimiter"), Marker: query.Get("marker"),
		StartAfter: query.Get("start-after"), ContinuationToken: query.Get("continuation-token"), MaxKeys: maxKeys,
	})
	if err != nil {
		h.handleStorageError(w, r, err, "failed to list objects")
		return
	}
	encode := func(value string) string {
		if query.Get("encoding-type") == "url" {
			return s3URLEncode(value)
		}
		return value
	}
	objects := make([]s3.Object, 0, len(result.Objects))
	var owner *s3.Owner
	if query.Get("fetch-owner") == "true" {
		owner = &s3.Owner{ID: "homestore", DisplayName: "HomeStore"}
	}
	for _, object := range result.Objects {
		objects = append(objects, s3.Object{Key: encode(object.Key), LastModified: object.LastModified.UTC().Format(time.RFC3339), ETag: object.ETag, Size: object.Size, StorageClass: "STANDARD", Owner: owner})
	}
	prefixes := make([]s3.CommonPrefix, 0, len(result.CommonPrefixes))
	for _, prefix := range result.CommonPrefixes {
		prefixes = append(prefixes, s3.CommonPrefix{Prefix: encode(prefix)})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	if v2 {
		response := s3.ListObjectsV2Result{
			Name: bucket, Prefix: encode(query.Get("prefix")), Delimiter: encode(query.Get("delimiter")), StartAfter: encode(query.Get("start-after")), MaxKeys: maxKeys, IsTruncated: result.IsTruncated,
			Contents: objects, CommonPrefixes: prefixes, ContinuationToken: query.Get("continuation-token"),
			NextToken: result.NextContinuationToken, KeyCount: len(objects) + len(prefixes), EncodingType: query.Get("encoding-type"),
		}
		_ = xml.NewEncoder(w).Encode(response)
		return
	}
	response := s3.ListObjectsResult{
		Name: bucket, Prefix: encode(query.Get("prefix")), Marker: encode(query.Get("marker")), NextMarker: encode(result.NextMarker),
		Delimiter: encode(query.Get("delimiter")), MaxKeys: maxKeys, IsTruncated: result.IsTruncated, EncodingType: query.Get("encoding-type"), Contents: objects, CommonPrefixes: prefixes,
	}
	_ = xml.NewEncoder(w).Encode(response)
}

// s3URLEncode applies S3's URL encoding rules for XML listing results. It is
// byte-oriented so UTF-8 keys are encoded exactly as AWS clients expect,
// rather than using form or path escaping semantics.
func s3URLEncode(value string) string {
	const hexChars = "0123456789ABCDEF"
	var encoded strings.Builder
	encoded.Grow(len(value))
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~' {
			encoded.WriteByte(b)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexChars[b>>4])
		encoded.WriteByte(hexChars[b&0x0f])
	}
	return encoded.String()
}

func (h *Handler) handleDeleteObjects(w http.ResponseWriter, r *http.Request) {
	const maxDeleteBody = 2 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDeleteBody+1))
	if err != nil || len(body) > maxDeleteBody {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "multi-object delete request is too large")
		return
	}
	if err := verifyRequestChecksum(r, body); err != nil {
		h.handleStorageError(w, r, err, "invalid multi-object delete checksum")
		return
	}
	var request s3.DeleteRequest
	if err := xml.Unmarshal(body, &request); err != nil || len(request.Objects) > 1000 {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid multi-object delete request")
		return
	}
	bucket, _ := parseBucketKey(r.URL.Path)
	result := s3.DeleteResult{}
	for _, object := range request.Objects {
		if !storage.IsValidObjectKey(object.Key) {
			result.Errors = append(result.Errors, s3.DeleteError{Key: object.Key, Code: s3.InvalidArgument, Message: "invalid object key"})
			continue
		}
		err := h.backend.DeleteObject(r.Context(), bucket, object.Key)
		if err == nil {
			if !request.Quiet {
				result.Deleted = append(result.Deleted, s3.DeletedObject(object))
			}
			continue
		}
		if err == storage.ErrBucketNotFound {
			h.handleStorageError(w, r, err, "failed to delete objects")
			return
		}
		result.Errors = append(result.Errors, s3.DeleteError{Key: object.Key, Code: s3.InternalError, Message: "failed to delete object"})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(result)
}

func verifyRequestChecksum(r *http.Request, body []byte) error {
	if value := r.Header.Get("Content-MD5"); value != "" {
		digest := md5.Sum(body)
		if base64.StdEncoding.EncodeToString(digest[:]) != value {
			return storage.ErrInvalidDigest
		}
	}
	checksums := []struct {
		header string
		value  string
	}{
		{"x-amz-checksum-crc32", base64.StdEncoding.EncodeToString(crc32Checksum(body, crc32.IEEETable))},
		{"x-amz-checksum-crc32c", base64.StdEncoding.EncodeToString(crc32Checksum(body, crc32.MakeTable(crc32.Castagnoli)))},
		{"x-amz-checksum-sha1", sha1Checksum(body)},
		{"x-amz-checksum-sha256", sha256Checksum(body)},
	}
	for _, checksum := range checksums {
		if value := r.Header.Get(checksum.header); value != "" && value != checksum.value {
			return storage.ErrInvalidDigest
		}
	}
	return nil
}

func crc32Checksum(body []byte, table *crc32.Table) []byte {
	value := crc32.Checksum(body, table)
	return []byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
}

func sha1Checksum(body []byte) string {
	digest := sha1.Sum(body)
	return base64.StdEncoding.EncodeToString(digest[:])
}

func sha256Checksum(body []byte) string {
	digest := sha256.Sum256(body)
	return base64.StdEncoding.EncodeToString(digest[:])
}
