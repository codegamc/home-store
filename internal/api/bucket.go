package api

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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
	_, _ = w.Write([]byte(xml.Header))
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

	location, err := parseCreateBucketLocation(r)
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, err.Error())
		return
	}

	if err := h.backend.CreateBucket(r.Context(), bucket, location); err != nil {
		if errors.Is(err, storage.ErrBucketExists) {
			h.errorResponse(w, http.StatusConflict, s3.BucketAlreadyExists, "bucket already exists")
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to create bucket")
		}
		return
	}

	w.Header().Set("Location", "/"+bucket)
	w.WriteHeader(http.StatusOK)
}

func parseCreateBucketLocation(r *http.Request) (string, error) {
	if r.Body == nil {
		return "", nil
	}

	const maxCreateBucketBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCreateBucketBody+1))
	if err != nil {
		return "", fmt.Errorf("failed to read create bucket request body")
	}
	if len(body) > maxCreateBucketBody {
		return "", fmt.Errorf("create bucket configuration is too large")
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(body))
	var configuration s3.CreateBucketConfiguration
	if err := decoder.Decode(&configuration); err != nil || configuration.XMLName.Local != "CreateBucketConfiguration" {
		return "", fmt.Errorf("invalid create bucket configuration")
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("invalid create bucket configuration")
	}
	return strings.TrimSpace(configuration.LocationConstraint), nil
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
		if errors.Is(err, storage.ErrBucketNotFound) {
			h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		} else if errors.Is(err, storage.ErrBucketNotEmpty) {
			h.errorResponse(w, http.StatusConflict, s3.BucketNotEmpty, "the bucket you tried to delete is not empty")
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
	info, err := h.backend.GetBucket(r.Context(), bucket)
	if err == nil {
		w.Header().Set("x-amz-bucket-region", info.Region)
	}
	w.WriteHeader(http.StatusOK)
}

// handleGetBucketLocation handles GET /{bucket}?location.
func (h *Handler) handleGetBucketLocation(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	info, err := h.backend.GetBucket(r.Context(), bucket)
	if err != nil {
		if errors.Is(err, storage.ErrBucketNotFound) {
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", "/"+bucket)
		} else {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to retrieve bucket location")
		}
		return
	}
	type locationConstraint struct {
		XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ LocationConstraint"`
		Value   string   `xml:",chardata"`
	}
	value := info.Region
	if value == "us-east-1" {
		value = ""
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-bucket-region", info.Region)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(locationConstraint{Value: value})
}

// errorResponse writes an S3-compatible error response.
func (h *Handler) errorResponse(w http.ResponseWriter, statusCode int, code string, message string) {
	h.errorResponseWithResource(w, statusCode, code, message, "")
}

// errorResponseWithResource writes an S3-compatible error response with resource path.
func (h *Handler) errorResponseWithResource(w http.ResponseWriter, statusCode int, code string, message string, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	requestID := w.Header().Get("x-amz-request-id")
	if requestID == "" {
		requestID = generateRequestID()
		w.Header().Set("x-amz-request-id", requestID)
	}
	w.WriteHeader(statusCode)

	resp := s3.ErrorResponse{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestID: requestID,
	}

	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(resp)
}
