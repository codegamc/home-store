package api

import (
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func (h *Handler) handleGetBucketLocation(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	region, err := h.backend.GetBucketLocation(r.Context(), bucket)
	if err == storage.ErrBucketNotFound {
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		return
	}
	if err != nil {
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to get bucket location")
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ LocationConstraint"`
		Value   string   `xml:",chardata"`
	}{Value: region})
}

func (h *Handler) handleListObjects(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	query := r.URL.Query()
	result, err := h.backend.ListObjects(r.Context(), bucket, listOptions(query.Get("prefix"), query.Get("delimiter"), query.Get("marker"), query.Get("max-keys")))
	if err != nil {
		h.listError(w, err)
		return
	}
	resp := s3.ListObjectsResult{
		Name: bucket, Prefix: query.Get("prefix"), Marker: query.Get("marker"), Delimiter: query.Get("delimiter"),
		MaxKeys: parsedMaxKeys(query.Get("max-keys")), IsTruncated: result.IsTruncated, NextMarker: result.NextMarker,
		Contents: objectList(result.Objects), CommonPrefixes: commonPrefixes(result.CommonPrefixes),
	}
	h.xmlResponse(w, http.StatusOK, resp)
}

func (h *Handler) handleListObjectsV2(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	query := r.URL.Query()
	marker := query.Get("start-after")
	if token := query.Get("continuation-token"); token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid continuation token")
			return
		}
		marker = string(decoded)
	}
	result, err := h.backend.ListObjects(r.Context(), bucket, listOptions(query.Get("prefix"), query.Get("delimiter"), marker, query.Get("max-keys")))
	if err != nil {
		h.listError(w, err)
		return
	}
	nextToken := ""
	if result.IsTruncated {
		nextToken = base64.RawURLEncoding.EncodeToString([]byte(result.NextMarker))
	}
	resp := s3.ListObjectsV2Result{
		Name: bucket, Prefix: query.Get("prefix"), MaxKeys: parsedMaxKeys(query.Get("max-keys")),
		IsTruncated: result.IsTruncated, Contents: objectList(result.Objects), CommonPrefixes: commonPrefixes(result.CommonPrefixes),
		ContinuationToken: query.Get("continuation-token"), NextToken: nextToken,
		KeyCount: len(result.Objects) + len(result.CommonPrefixes),
	}
	h.xmlResponse(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteObjects(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	var request s3.DeleteObjectsRequest
	if err := xml.NewDecoder(r.Body).Decode(&request); err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid delete request")
		return
	}
	if len(request.Objects) > 1000 {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "a delete request may contain at most 1000 objects")
		return
	}
	if exists, err := h.backend.BucketExists(r.Context(), bucket); err != nil {
		h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to check bucket")
		return
	} else if !exists {
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		return
	}

	response := s3.DeleteObjectsResult{}
	for _, object := range request.Objects {
		if err := h.backend.DeleteObject(r.Context(), bucket, object.Key); err != nil {
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to delete object")
			return
		}
		if !request.Quiet {
			response.Deleted = append(response.Deleted, s3.DeletedObject(object))
		}
	}
	h.xmlResponse(w, http.StatusOK, response)
}

func (h *Handler) listError(w http.ResponseWriter, err error) {
	if err == storage.ErrBucketNotFound {
		h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		return
	}
	h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to list objects")
}

func listOptions(prefix, delimiter, marker, maxKeys string) storage.ListOptions {
	return storage.ListOptions{Prefix: prefix, Delimiter: delimiter, Marker: marker, MaxKeys: parsedMaxKeys(maxKeys)}
}

func parsedMaxKeys(value string) int {
	if value == "" {
		return 1000
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 1000
	}
	if parsed > 1000 {
		return 1000
	}
	return parsed
}

func objectList(objects []storage.ObjectInfo) []s3.Object {
	result := make([]s3.Object, 0, len(objects))
	for _, object := range objects {
		result = append(result, s3.Object{Key: object.Key, Size: object.Size, ETag: object.ETag, LastModified: object.LastModified.UTC().Format("2006-01-02T15:04:05.000Z"), StorageClass: "STANDARD"})
	}
	return result
}

func commonPrefixes(prefixes []string) []s3.CommonPrefix {
	result := make([]s3.CommonPrefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		result = append(result, s3.CommonPrefix{Prefix: prefix})
	}
	return result
}

func (h *Handler) xmlResponse(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(response)
}
