package api

import (
	"net/http"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

func (h *Handler) handleGetObjectAttributes(w http.ResponseWriter, r *http.Request) {
	bucket, key := parseBucketKey(r.URL.Path)
	meta, err := h.backend.HeadObject(r.Context(), bucket, key)
	if err != nil {
		switch err {
		case storage.ErrBucketNotFound:
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchBucket, "the specified bucket does not exist", "/"+bucket)
		case storage.ErrObjectNotFound:
			h.errorResponseWithResource(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist", r.URL.Path)
		default:
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to retrieve object attributes")
		}
		return
	}
	h.xmlResponse(w, http.StatusOK, s3.GetObjectAttributesResult{ETag: meta.ETag, ObjectSize: meta.ContentLength, StorageClass: "STANDARD"})
}
